package assistant

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pb "procureflow-ai/gen/assistant/v1"
	"procureflow-ai/internal/domain"
	aimigrations "procureflow-ai/migrations/assistant"
)

func testVectorDatabase(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	configured := os.Getenv("AI_TEST_DATABASE_URL")
	if configured == "" {
		t.Skip("AI pgvector integration NOT RUN: AI_TEST_DATABASE_URL is unset; provide a pgvector test server with CREATEDB")
	}
	parsed, err := url.Parse(configured)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("AI_TEST_DATABASE_URL must be a PostgreSQL URL (details redacted)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := OpenIndex(ctx, configured, false)
	if err != nil {
		t.Skip("AI pgvector integration NOT RUN: configured test server is unavailable (details redacted)")
	}
	t.Cleanup(admin.Close)
	name := "procureflow_ai_test_" + strings.ReplaceAll(domain.NewUUID(), "-", "")
	if _, err = admin.Exec(ctx, `CREATE DATABASE `+pgx.Identifier{name}.Sanitize()); err != nil {
		var e *pgconn.PgError
		if errors.As(err, &e) && e.Code == "42501" {
			t.Skip("AI pgvector integration NOT RUN: test role requires CREATEDB")
		}
		t.Fatal("cannot create isolated AI test database")
	}
	var pool *pgxpool.Pool
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, `DROP DATABASE `+pgx.Identifier{name}.Sanitize()+` WITH (FORCE)`); err != nil {
			t.Error("isolated AI database cleanup failed")
		}
	})
	parsed.Path = "/" + name
	pool, err = OpenIndex(ctx, parsed.String(), false)
	if err != nil {
		t.Fatal(err)
	}
	return pool, parsed.String()
}

func TestPGVectorRetrievalImportAndCitations(t *testing.T) {
	pool, databaseURL := testVectorDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg := testConfig()
	for i := 0; i < 2; i++ {
		if err := aimigrations.Apply(ctx, pool, cfg.EmbeddingModel, cfg.Dimensions); err != nil {
			t.Fatal(err)
		}
	}
	if err := aimigrations.Apply(ctx, pool, cfg.EmbeddingModel, 4); err == nil {
		t.Fatal("existing index accepted different dimensions")
	}
	if err := aimigrations.Apply(ctx, pool, "text-embedding-3-large", 3); err == nil {
		t.Fatal("existing index accepted a different model")
	}
	chunks, err := LoadDocuments(filepath.Join("..", "..", "policies"))
	if err != nil {
		t.Fatal(err)
	}
	store := &PostgresStore{Pool: pool, Model: cfg.EmbeddingModel, Dimensions: cfg.Dimensions}
	provider := fakeProvider{
		embed: func(_ context.Context, text string) ([]float32, error) {
			// Explicit deterministic TEST vectors; never a production fallback.
			lower := strings.ToLower(text)
			if strings.Contains(lower, "meal") {
				return []float32{1, 0, 0}, nil
			}
			if strings.Contains(lower, "supplier") {
				return []float32{0, 1, 0}, nil
			}
			return []float32{0, 0, 1}, nil
		},
		generate: func(_ context.Context, _ string, retrieved []Chunk) (Draft, error) {
			if len(retrieved) != 1 || retrieved[0].Document != "reimbursements.md" || !strings.HasPrefix(retrieved[0].Section, "Meal allowance@L") {
				t.Fatalf("wrong SQL retrieval=%+v", retrieved)
			}
			return Draft{true, "The fictional meal reimbursement limit is EUR 25 per person per business day.", []Citation{{1, "The FICTIONAL meal reimbursement limit is EUR 25 per person per business day."}}}, nil
		},
	}
	embedded, err := Import(ctx, store, provider, chunks, 3)
	if err != nil || embedded != len(chunks) {
		t.Fatalf("first import=%d error=%v", embedded, err)
	}
	var before, after time.Time
	if err = pool.QueryRow(ctx, `SELECT max(updated_at) FROM ai.policy_chunks`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	embedded, err = Import(ctx, store, provider, chunks, 3)
	if err != nil || embedded != 0 {
		t.Fatalf("repeat import=%d error=%v", embedded, err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*),max(updated_at) FROM ai.policy_chunks`).Scan(&count, &after); err != nil || count != len(chunks) || !before.Equal(after) {
		t.Fatalf("upsert changed count/time: count=%d error=%v", count, err)
	}
	if empty, err := store.Search(ctx, []float32{1, 1, 1}, 3, .1); err != nil || len(empty) != 0 {
		t.Fatalf("irrelevant evidence accepted=%+v error=%v", empty, err)
	}
	readOnly, err := OpenIndex(ctx, databaseURL, true)
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	reader := &PostgresStore{Pool: readOnly, Model: cfg.EmbeddingModel, Dimensions: 3}
	if err = reader.Verify(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = readOnly.Exec(ctx, `DELETE FROM ai.policy_chunks WHERE false`)
	var dbError *pgconn.PgError
	if !errors.As(err, &dbError) || dbError.Code != "25006" {
		t.Fatalf("runtime pool did not enforce read-only SQL: %v", err)
	}

	// Real gRPC + real pgvector retrieval, with fake embeddings/LLM only.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := grpc.NewServer()
	defer server.Stop()
	pb.RegisterAssistantServiceServer(server, NewService(cfg, provider, reader))
	go func() { _ = server.Serve(listener) }()
	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	answer, err := pb.NewAssistantServiceClient(connection).AnswerPolicy(ctx, &pb.AnswerPolicyRequest{Question: "What is the fictional meal reimbursement limit?"})
	if err != nil || !answer.GetConfigured() || len(answer.GetSources()) != 1 {
		t.Fatalf("gRPC answer=%v error=%v", answer, err)
	}
	for _, source := range answer.Sources {
		found := false
		for _, c := range chunks {
			if source.Document == c.Document && source.Section == c.Section && int(source.ChunkIndex) == c.Index && strings.Contains(c.Content, source.Quote) {
				found = true
			}
		}
		if !found {
			t.Fatalf("citation is not an exact source-file match: %v", source)
		}
	}
	// Importing a changed corpus removes obsolete chunks only in the AI index.
	if err = store.Replace(ctx, chunksWithVectors(t, store, ctx, chunks[:1])); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM ai.policy_chunks`).Scan(&count); err != nil || count != 1 {
		t.Fatal("stale chunks survived corpus replacement")
	}
}

func chunksWithVectors(t *testing.T, s *PostgresStore, ctx context.Context, chunks []Chunk) []Chunk {
	t.Helper()
	existing, err := s.Existing(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result := make([]Chunk, len(chunks))
	for i, c := range chunks {
		c.Embedding = existing[chunkKey(c)].Embedding
		result[i] = c
	}
	return result
}

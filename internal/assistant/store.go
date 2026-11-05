package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"procureflow-ai/internal/postgres"
)

func OpenIndex(ctx context.Context, url string, readOnly bool) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("invalid AI database configuration (details redacted)")
	}
	cfg.MaxConns = 4
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	if readOnly {
		cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open AI database (details redacted)")
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("AI database unavailable (details redacted)")
	}
	return pool, nil
}

type PostgresStore struct {
	Pool       *pgxpool.Pool
	Model      string
	Dimensions int
}

func (s *PostgresStore) Verify(ctx context.Context) error {
	var model string
	var dimensions int
	if err := s.Pool.QueryRow(ctx, `SELECT model,dimensions FROM ai.embedding_config WHERE singleton`).Scan(&model, &dimensions); err != nil {
		return fmt.Errorf("policy index is missing; run policy-import first")
	}
	if model != s.Model || dimensions != s.Dimensions {
		return fmt.Errorf("policy index model/dimensions differ from configuration")
	}
	return nil
}
func (s *PostgresStore) Search(ctx context.Context, vector []float32, k int, maxDistance float64) ([]Chunk, error) {
	if err := ValidateVector(vector, s.Dimensions); err != nil {
		return nil, err
	}
	if k < 1 || k > 5 || !(maxDistance >= 0 && maxDistance <= 1) {
		return nil, fmt.Errorf("invalid retrieval bounds")
	}
	encoded, _ := json.Marshal(vector)
	rows, err := s.Pool.Query(ctx, `SELECT document,section,chunk_index,content FROM ai.policy_chunks
        WHERE embedding_model=$2 AND embedding <=> $1::vector <= $3
        ORDER BY embedding <=> $1::vector,document,section,chunk_index LIMIT $4`, string(encoded), s.Model, maxDistance, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var chunks []Chunk
	for rows.Next() {
		var c Chunk
		if err = rows.Scan(&c.Document, &c.Section, &c.Index, &c.Content); err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}
func chunkKey(c Chunk) string {
	return c.Document + "\x00" + c.Section + "\x00" + strconv.Itoa(c.Index)
}
func (s *PostgresStore) Existing(ctx context.Context) (map[string]Chunk, error) {
	rows, err := s.Pool.Query(ctx, `SELECT document,section,chunk_index,content_hash,embedding::text FROM ai.policy_chunks WHERE embedding_model=$1`, s.Model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]Chunk{}
	for rows.Next() {
		var c Chunk
		var vector string
		if err = rows.Scan(&c.Document, &c.Section, &c.Index, &c.Hash, &vector); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(vector), &c.Embedding); err != nil {
			return nil, err
		}
		result[chunkKey(c)] = c
	}
	return result, rows.Err()
}

// Replace publishes one corpus atomically after every embedding succeeded.
// Upserts preserve unchanged timestamps; stale chunks from removed files vanish.
func (s *PostgresStore) Replace(ctx context.Context, chunks []Chunk) error {
	if len(chunks) == 0 || len(chunks) > MaxChunks {
		return fmt.Errorf("invalid policy corpus size")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer postgres.Rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(73008002)`); err != nil {
		return err
	}
	for _, c := range chunks {
		if err = ValidateVector(c.Embedding, s.Dimensions); err != nil {
			return err
		}
		encoded, _ := json.Marshal(c.Embedding)
		_, err = tx.Exec(ctx, `INSERT INTO ai.policy_chunks(document,section,chunk_index,content,content_hash,embedding_model,embedding)
            VALUES($1,$2,$3,$4,$5,$6,$7::vector) ON CONFLICT(document,section,chunk_index) DO UPDATE
            SET content=EXCLUDED.content,content_hash=EXCLUDED.content_hash,embedding_model=EXCLUDED.embedding_model,
                embedding=EXCLUDED.embedding,updated_at=CURRENT_TIMESTAMP
            WHERE ai.policy_chunks.content_hash IS DISTINCT FROM EXCLUDED.content_hash OR ai.policy_chunks.embedding_model IS DISTINCT FROM EXCLUDED.embedding_model`, c.Document, c.Section, c.Index, c.Content, c.Hash, s.Model, string(encoded))
		if err != nil {
			return err
		}
	}
	keep, _ := json.Marshal(chunks)
	_, err = tx.Exec(ctx, `DELETE FROM ai.policy_chunks AS p WHERE NOT EXISTS (
        SELECT 1 FROM jsonb_to_recordset($1::jsonb) AS k(document text,section text,chunk_index integer)
        WHERE p.document=k.document AND p.section=k.section AND p.chunk_index=k.chunk_index)`, string(keep))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

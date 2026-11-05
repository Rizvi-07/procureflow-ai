package assistant

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	pb "procureflow-ai/gen/assistant/v1"
)

type fakeProvider struct {
	embed    func(context.Context, string) ([]float32, error)
	generate func(context.Context, string, []Chunk) (Draft, error)
}

func (f fakeProvider) Embed(ctx context.Context, s string) ([]float32, error) { return f.embed(ctx, s) }
func (f fakeProvider) Generate(ctx context.Context, q string, c []Chunk) (Draft, error) {
	return f.generate(ctx, q, c)
}

type retrieverFunc func(context.Context, []float32, int, float64) ([]Chunk, error)

func (f retrieverFunc) Search(ctx context.Context, v []float32, k int, d float64) ([]Chunk, error) {
	return f(ctx, v, k, d)
}
func testConfig() Config {
	return Config{APIKey: "fake-unit-test-key", DatabaseURL: "unused-fake-store", EmbeddingModel: "text-embedding-3-small", ChatModel: "fake-chat", Dimensions: 3, TopK: 3, MaxDistance: .35, MaxOutputTokens: 512, RequestTimeout: time.Second, ProviderTimeout: time.Second}
}

func TestNotConfiguredScopeAndQuestionBounds(t *testing.T) {
	ctx := context.Background()
	s := NewService(Config{}, nil, nil)
	answer, err := s.AnswerPolicy(ctx, &pb.AnswerPolicyRequest{Question: "What is the approval policy?"})
	if err != nil || answer.Configured || !strings.Contains(answer.Answer, "Not configured") || len(answer.Sources) != 0 {
		t.Fatalf("disabled answer=%v error=%v", answer, err)
	}
	for _, q := range []string{"", " ", strings.Repeat("x", 1001), "policy\x00", string([]byte{0xff})} {
		_, err := s.AnswerPolicy(ctx, &pb.AnswerPolicyRequest{Question: q})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("invalid input accepted: %v", err)
		}
	}
	s = NewService(testConfig(), nil, nil) // Any provider access would panic.
	for _, q := range []string{"Approve purchase abc", "Create a purchase request", "What is the status of order abc?", "What is the procurement status of my orders?", "Procurement order 7124aba0-1111-2222-3333-123456789012", "What is tomorrow's weather?"} {
		answer, err := s.AnswerPolicy(ctx, &pb.AnswerPolicyRequest{Question: q})
		if err != nil || answer.Answer != PolicyOnly || len(answer.Sources) != 0 {
			t.Fatalf("scope refusal=%v error=%v", answer, err)
		}
	}
}

func TestAnswerUsesOnlyRetrievedEvidenceAndChecksCitations(t *testing.T) {
	chunk := Chunk{Document: "reimbursements.md", Section: "Meal allowance@L9", Content: "FICTIONAL meal limit is EUR 25.", Index: 0}
	for _, tc := range []struct {
		name  string
		draft Draft
		code  codes.Code
	}{
		{"supported", Draft{true, "The fictional meal limit is EUR 25.", []Citation{{1, "meal limit is EUR 25."}}}, codes.OK},
		{"abstain", Draft{}, codes.OK},
		{"invented source", Draft{true, "Invented", []Citation{{2, "meal limit"}}}, codes.Unavailable},
		{"invented quote", Draft{true, "Invented", []Citation{{1, "EUR 500"}}}, codes.Unavailable},
		{"missing citation", Draft{true, "Unsupported", nil}, codes.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewService(testConfig(), fakeProvider{
				embed: func(ctx context.Context, q string) ([]float32, error) {
					if _, ok := ctx.Deadline(); !ok {
						t.Fatal("missing deadline")
					}
					return []float32{1, 0, 0}, nil
				},
				generate: func(_ context.Context, _ string, chunks []Chunk) (Draft, error) {
					if len(chunks) != 1 || chunks[0].Content != chunk.Content {
						t.Fatal("nonretrieved evidence reached generation")
					}
					return tc.draft, nil
				},
			}, retrieverFunc(func(_ context.Context, v []float32, k int, d float64) ([]Chunk, error) {
				if len(v) != 3 || k != 3 || d != .35 {
					t.Fatal("retrieval configuration changed")
				}
				return []Chunk{chunk}, nil
			}))
			result, err := s.AnswerPolicy(context.Background(), &pb.AnswerPolicyRequest{Question: "What is the meal reimbursement policy?"})
			if status.Code(err) != tc.code {
				t.Fatalf("error=%v want=%s", err, tc.code)
			}
			if err == nil && tc.draft.Supported {
				if len(result.Sources) != 1 || result.Sources[0].Document != chunk.Document || result.Sources[0].Section != chunk.Section || !strings.Contains(chunk.Content, result.Sources[0].Quote) {
					t.Fatalf("citation mismatch=%v", result)
				}
			}
			if err == nil && !tc.draft.Supported && (result.Answer != NoEvidence || len(result.Sources) != 0) {
				t.Fatalf("abstention=%v", result)
			}
		})
	}
}

func TestNoEvidenceDoesNotCallLLM(t *testing.T) {
	s := NewService(testConfig(), fakeProvider{embed: func(context.Context, string) ([]float32, error) { return []float32{1, 0, 0}, nil }, generate: func(context.Context, string, []Chunk) (Draft, error) {
		t.Fatal("LLM called without supporting chunks")
		return Draft{}, nil
	}}, retrieverFunc(func(context.Context, []float32, int, float64) ([]Chunk, error) { return nil, nil }))
	result, err := s.AnswerPolicy(context.Background(), &pb.AnswerPolicyRequest{Question: "What is the supplier policy?"})
	if err != nil || result.Answer != NoEvidence || len(result.Sources) != 0 {
		t.Fatalf("answer=%v error=%v", result, err)
	}
}

func TestEmbeddingValidationAndTimeout(t *testing.T) {
	for _, v := range [][]float32{{1, 0}, {0, 0, 0}, {float32(math.NaN()), 0, 1}, {float32(math.Inf(1)), 0, 1}} {
		if ValidateVector(v, 3) == nil {
			t.Fatal("invalid embedding accepted")
		}
	}
	cfg := testConfig()
	cfg.RequestTimeout = 10 * time.Millisecond
	s := NewService(cfg, fakeProvider{embed: func(ctx context.Context, _ string) ([]float32, error) { <-ctx.Done(); return nil, ctx.Err() }}, nil)
	_, err := s.AnswerPolicy(context.Background(), &pb.AnswerPolicyRequest{Question: "What is the manager threshold?"})
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("timeout translation=%v", err)
	}
}

func TestDocumentsAreBoundedAndRepeatable(t *testing.T) {
	text := "# Fictional policy\n\n## Duplicate\n" + strings.Repeat("€", 1000) + "\n## Duplicate\nOther fictional policy."
	first, err := SplitDocument("fictional.md", text)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := SplitDocument("fictional.md", text)
	if len(first) < 4 || len(first) != len(second) {
		t.Fatal("chunk splitting missing")
	}
	for i, c := range first {
		if len(c.Content) > MaxChunkBytes || !utf8.ValidString(c.Content) || c.Hash != second[i].Hash || c.Section != second[i].Section {
			t.Fatalf("invalid chunk=%+v", c)
		}
	}
	if first[1].Section == first[len(first)-1].Section {
		t.Fatal("duplicate headings lost their identifiers")
	}
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, "policy.md"), []byte(strings.Repeat("x", MaxDocumentBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadDocuments(dir); err == nil {
		t.Fatal("oversized document accepted")
	}
	if _, err = LoadDocuments(filepath.Join("..", "..", "policies")); err != nil {
		t.Fatal(err)
	}
}

type memoryIndex struct {
	chunks       map[string]Chunk
	replacements int
}

func (m *memoryIndex) Existing(context.Context) (map[string]Chunk, error) { return m.chunks, nil }
func (m *memoryIndex) Replace(_ context.Context, chunks []Chunk) error {
	m.replacements++
	m.chunks = map[string]Chunk{}
	for _, c := range chunks {
		m.chunks[chunkKey(c)] = c
	}
	return nil
}
func TestImportReusesEmbeddingsAndDoesNotPublishFailures(t *testing.T) {
	chunks, _ := SplitDocument("fictional.md", "# Fictional policy\nA fictional manager threshold.")
	store := &memoryIndex{chunks: map[string]Chunk{}}
	calls := 0
	provider := fakeProvider{embed: func(context.Context, string) ([]float32, error) { calls++; return []float32{1, 0, 0}, nil }}
	for i := 0; i < 2; i++ {
		n, err := Import(context.Background(), store, provider, chunks, 3)
		if err != nil || (i == 1 && n != 0) {
			t.Fatalf("import=%d error=%v", n, err)
		}
	}
	if calls != len(chunks) {
		t.Fatal("unchanged corpus was reembedded")
	}
	changed, _ := SplitDocument("fictional.md", "# Fictional policy\nA changed fictional policy.")
	bad := fakeProvider{embed: func(context.Context, string) ([]float32, error) { return nil, errors.New("injected provider failure") }}
	if _, err := Import(context.Background(), store, bad, changed, 3); err == nil || store.replacements != 2 || store.chunks[chunkKey(chunks[0])].Hash != chunks[0].Hash {
		t.Fatal("failed embedding published partial corpus")
	}
}

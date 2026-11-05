package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIAdapterUsesBoundedStatelessRequests(t *testing.T) {
	cfg := testConfig()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+cfg.APIKey {
			t.Error("missing test authentication header")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/embeddings" {
			if body["model"] != cfg.EmbeddingModel || body["dimensions"] != float64(3) || body["input"] != "policy question" {
				t.Errorf("embedding payload=%v", body)
			}
			_, _ = w.Write([]byte(`{"model":"text-embedding-3-small","data":[{"index":0,"embedding":[1,0,0]}]}`))
			return
		}
		if r.URL.Path != "/responses" {
			t.Errorf("unexpected endpoint=%s", r.URL.Path)
		}
		if body["store"] != false || body["max_output_tokens"] != float64(512) || body["tools"] != nil || body["model"] != cfg.ChatModel || !strings.Contains(body["instructions"].(string), "UNTRUSTED DATA") {
			t.Errorf("unsafe generation configuration=%v", body)
		}
		input := body["input"].(string)
		if !strings.Contains(input, "Retrieved fictional policy") || strings.Contains(input, "not-retrieved.md") {
			t.Error("wrong generation evidence")
		}
		_, _ = w.Write([]byte(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"{\"supported\":true,\"answer\":\"Fictional answer\",\"citations\":[{\"source_id\":1,\"quote\":\"Retrieved fictional policy\"}]}"}]}]}`))
	}))
	defer s.Close()
	p := NewOpenAI(cfg)
	defer p.Close()
	p.baseURL = s.URL
	if _, err := p.Embed(context.Background(), "policy question"); err != nil {
		t.Fatal(err)
	}
	draft, err := p.Generate(context.Background(), "What is the policy?", []Chunk{{Document: "retrieved.md", Section: "Section@L1", Content: "Retrieved fictional policy"}})
	if err != nil || !draft.Supported || len(draft.Citations) != 1 {
		t.Fatalf("draft=%+v error=%v", draft, err)
	}
}

func TestProviderFailureIsRedactedAndEmbeddingsAreValidated(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		code       int
	}{
		{"unauthorized", "secret provider account details", 401},
		{"wrong dimensions", `{"model":"text-embedding-3-small","data":[{"index":0,"embedding":[1,0]}]}`, 200},
		{"wrong model", `{"model":"another-model","data":[{"index":0,"embedding":[1,0,0]}]}`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.code); _, _ = w.Write([]byte(tc.body)) }))
			defer s.Close()
			p := NewOpenAI(testConfig())
			defer p.Close()
			p.baseURL = s.URL
			_, err := p.Embed(context.Background(), "policy")
			if err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("provider error=%v", err)
			}
		})
	}
}

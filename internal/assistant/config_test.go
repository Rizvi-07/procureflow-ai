package assistant

import "testing"

func configEnv(t *testing.T) {
	t.Helper()
	for k, v := range map[string]string{"ASSISTANT_GRPC_ADDR": "127.0.0.1:50053", "AI_DATABASE_URL": "", "OPENAI_API_KEY": "", "EMBEDDING_MODEL": "", "CHAT_MODEL": "", "EMBEDDING_DIMENSIONS": "1536", "AI_TOP_K": "3", "AI_MAX_OUTPUT_TOKENS": "512", "AI_MAX_COSINE_DISTANCE": "0.35", "ASSISTANT_REQUEST_TIMEOUT": "8s", "AI_PROVIDER_TIMEOUT": "6s"} {
		t.Setenv(k, v)
	}
}
func TestOptionalConfigAndDimensions(t *testing.T) {
	configEnv(t)
	c, err := LoadConfig()
	if err != nil || c.Configured() {
		t.Fatalf("default configuration=%+v error=%v", c, err)
	}
	if ValidateEmbedding("text-embedding-3-small", 1536) != nil || ValidateEmbedding("text-embedding-3-large", 3072) != nil {
		t.Fatal("supported model dimensions rejected")
	}
	for _, tc := range []struct{ key, value string }{
		{"EMBEDDING_MODEL", "unknown"}, {"AI_TOP_K", "6"}, {"EMBEDDING_DIMENSIONS", "0"}, {"AI_MAX_COSINE_DISTANCE", "NaN"}, {"AI_MAX_OUTPUT_TOKENS", "99999"}, {"AI_PROVIDER_TIMEOUT", "0s"}, {"ASSISTANT_REQUEST_TIMEOUT", "2m"}, {"ASSISTANT_GRPC_ADDR", "bad"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			configEnv(t)
			t.Setenv(tc.key, tc.value)
			if _, err := LoadConfig(); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	if ValidateEmbedding("text-embedding-3-small", 1537) == nil {
		t.Fatal("model dimension mismatch accepted")
	}
}

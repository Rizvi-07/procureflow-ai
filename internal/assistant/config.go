// Package assistant implements an optional policy-only RAG boundary.
package assistant

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	MaxQuestionBytes = 1000
	MaxDocumentBytes = 32 * 1024
	MaxChunkBytes    = 1200
	MaxChunks        = 64
	NoEvidence       = "No supporting policy was found in the imported fictional documents."
	NotConfigured    = "Not configured. The optional policy assistant requires an API key, models, and an imported policy index."
	PolicyOnly       = "I can only explain fictional procurement policy documents. I cannot create or decide purchases, look up live orders, or report their status."
)

type Config struct {
	Addr, DatabaseURL, APIKey, EmbeddingModel, ChatModel, PolicyDir string
	Dimensions, TopK, MaxOutputTokens                               int
	MaxDistance                                                     float64
	RequestTimeout, ProviderTimeout                                 time.Duration
}

func (c Config) Configured() bool {
	return c.APIKey != "" && c.EmbeddingModel != "" && c.ChatModel != "" && c.DatabaseURL != ""
}

func ValidateEmbedding(model string, dimensions int) error {
	max := 1536
	if model == "text-embedding-3-large" {
		max = 3072
	} else if model != "text-embedding-3-small" {
		return fmt.Errorf("EMBEDDING_MODEL must be text-embedding-3-small or text-embedding-3-large")
	}
	if dimensions < 1 || dimensions > max {
		return fmt.Errorf("EMBEDDING_DIMENSIONS must be 1-%d for the selected model", max)
	}
	return nil
}

func LoadConfig() (Config, error) {
	get := func(key, fallback string) string {
		if s := os.Getenv(key); s != "" {
			return s
		}
		return fallback
	}
	c := Config{Addr: get("ASSISTANT_GRPC_ADDR", "127.0.0.1:50053"), DatabaseURL: os.Getenv("AI_DATABASE_URL"), APIKey: strings.TrimSpace(os.Getenv("OPENAI_API_KEY")), EmbeddingModel: strings.TrimSpace(os.Getenv("EMBEDDING_MODEL")), ChatModel: strings.TrimSpace(os.Getenv("CHAT_MODEL")), PolicyDir: get("POLICY_DIR", "policies")}
	_, port, err := net.SplitHostPort(c.Addr)
	n, e := strconv.Atoi(port)
	if err != nil || e != nil || n < 1 || n > 65535 {
		return Config{}, fmt.Errorf("ASSISTANT_GRPC_ADDR must be host:port with port 1-65535")
	}
	for _, p := range []struct {
		key, fallback string
		value         *int
		min, max      int
	}{
		{"EMBEDDING_DIMENSIONS", "1536", &c.Dimensions, 1, 3072},
		{"AI_TOP_K", "3", &c.TopK, 1, 5},
		{"AI_MAX_OUTPUT_TOKENS", "512", &c.MaxOutputTokens, 64, 1024},
	} {
		v, err := strconv.Atoi(get(p.key, p.fallback))
		if err != nil || v < p.min || v > p.max {
			return Config{}, fmt.Errorf("%s must be %d-%d", p.key, p.min, p.max)
		}
		*p.value = v
	}
	if c.EmbeddingModel != "" {
		if err := ValidateEmbedding(c.EmbeddingModel, c.Dimensions); err != nil {
			return Config{}, err
		}
	}
	c.MaxDistance, err = strconv.ParseFloat(get("AI_MAX_COSINE_DISTANCE", "0.35"), 64)
	if err != nil || !(c.MaxDistance >= 0 && c.MaxDistance <= 1) {
		return Config{}, fmt.Errorf("AI_MAX_COSINE_DISTANCE must be 0-1")
	}
	for _, p := range []struct {
		key, fallback string
		value         *time.Duration
	}{
		{"ASSISTANT_REQUEST_TIMEOUT", "8s", &c.RequestTimeout}, {"AI_PROVIDER_TIMEOUT", "6s", &c.ProviderTimeout},
	} {
		v, err := time.ParseDuration(get(p.key, p.fallback))
		if err != nil || v <= 0 || v > time.Minute {
			return Config{}, fmt.Errorf("%s must be positive and at most 1m", p.key)
		}
		*p.value = v
	}
	return c, nil
}

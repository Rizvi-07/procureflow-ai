// policy-import is an explicit administrative operation that can incur API cost.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"procureflow-ai/internal/assistant"
	aimigrations "procureflow-ai/migrations/assistant"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := run(ctx); err != nil {
		slog.Error("policy import failed", "error", err)
		os.Exit(1)
	}
}
func run(ctx context.Context) error {
	cfg, err := assistant.LoadConfig()
	if err != nil {
		return err
	}
	if cfg.APIKey == "" || cfg.EmbeddingModel == "" || cfg.DatabaseURL == "" {
		return fmt.Errorf("set OPENAI_API_KEY, EMBEDDING_MODEL, and AI_DATABASE_URL before explicitly importing")
	}
	chunks, err := assistant.LoadDocuments(cfg.PolicyDir)
	if err != nil {
		return err
	}
	pool, err := assistant.OpenIndex(ctx, cfg.DatabaseURL, false)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err = aimigrations.Apply(ctx, pool, cfg.EmbeddingModel, cfg.Dimensions); err != nil {
		return fmt.Errorf("optional AI schema initialization failed; check pgvector installation and model/dimensions")
	}
	index := &assistant.PostgresStore{Pool: pool, Model: cfg.EmbeddingModel, Dimensions: cfg.Dimensions}
	api := assistant.NewOpenAI(cfg)
	defer api.Close()
	embedded, err := assistant.Import(ctx, index, api, chunks, cfg.Dimensions)
	if err != nil {
		return fmt.Errorf("policy import did not publish a new corpus (details redacted)")
	}
	slog.Info("fictional policy import complete", "chunks", len(chunks), "new_embeddings", embedded)
	return nil
}

package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"procureflow-ai/internal/postgres"
	"procureflow-ai/migrations"
)

func main() {
	if err := run(); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
	slog.Info("migrations applied; already-applied files were skipped")
}

func run() error {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, err := postgres.Open(ctx, url, 5*time.Second)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := migrations.Apply(ctx, pool); err != nil {
		// Avoid exposing connection strings or server detail in startup logs.
		return errors.New("cannot apply migrations; check database permissions and migration checksums")
	}
	return nil
}

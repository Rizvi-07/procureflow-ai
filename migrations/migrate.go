// Package migrations embeds ordered, immutable schema migrations.
package migrations

import (
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"strings"

	"procureflow-ai/internal/postgres"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var files embed.FS

// Apply serializes concurrent runners and commits schema plus ledger atomically.
// Runtime services never call this; migrations use a separate administrative step.
func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer postgres.Rollback(tx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(73003001)`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS public.procureflow_migrations (
		name TEXT PRIMARY KEY, checksum TEXT NOT NULL, applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return err
	}
	entries, err := files.ReadDir(".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		body, err := files.ReadFile(entry.Name())
		if err != nil {
			return err
		}
		// Git may check SQL out as CRLF on Windows; checksums must be portable.
		body = []byte(strings.ReplaceAll(string(body), "\r\n", "\n"))
		checksum := fmt.Sprintf("%x", sha256.Sum256(body))
		var previous string
		err = tx.QueryRow(ctx, `SELECT checksum FROM public.procureflow_migrations WHERE name=$1`, entry.Name()).Scan(&previous)
		if err == nil {
			if previous != checksum {
				return fmt.Errorf("migration %s has changed after application", entry.Name())
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", entry.Name(), err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO public.procureflow_migrations(name,checksum) VALUES ($1,$2)`, entry.Name(), checksum); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

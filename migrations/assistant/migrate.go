// Package assistantmigrations contains optional, isolated AI schema changes.
package assistantmigrations

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"procureflow-ai/internal/postgres"
)

//go:embed 001_policy_chunks.sql
var schema string

func Apply(ctx context.Context, pool *pgxpool.Pool, model string, dimensions int) error {
	if model == "" || dimensions < 1 || dimensions > 3072 {
		return fmt.Errorf("invalid embedding schema configuration")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer postgres.Rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(73008001)`); err != nil {
		return err
	}
	ddl := strings.ReplaceAll(schema, "{{DIMENSIONS}}", fmt.Sprint(dimensions))
	if _, err = tx.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("optional AI migration failed; pgvector must be installed")
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS ai.schema_migrations (version INTEGER PRIMARY KEY, checksum TEXT NOT NULL)`); err != nil {
		return err
	}
	checksum := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ReplaceAll(schema, "\r\n", "\n"))))
	if _, err = tx.Exec(ctx, `INSERT INTO ai.schema_migrations(version,checksum) VALUES (1,$1) ON CONFLICT DO NOTHING`, checksum); err != nil {
		return err
	}
	var previous string
	if err = tx.QueryRow(ctx, `SELECT checksum FROM ai.schema_migrations WHERE version=1`).Scan(&previous); err != nil {
		return err
	}
	if previous != checksum {
		return fmt.Errorf("AI migration changed after application")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO ai.embedding_config(singleton,model,dimensions) VALUES(TRUE,$1,$2) ON CONFLICT DO NOTHING`, model, dimensions); err != nil {
		return err
	}
	var savedModel, columnType string
	var savedDimensions int
	if err = tx.QueryRow(ctx, `SELECT model,dimensions FROM ai.embedding_config WHERE singleton`).Scan(&savedModel, &savedDimensions); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `SELECT format_type(atttypid,atttypmod) FROM pg_attribute WHERE attrelid='ai.policy_chunks'::regclass AND attname='embedding'`).Scan(&columnType); err != nil {
		return err
	}
	if savedModel != model || savedDimensions != dimensions || columnType != fmt.Sprintf("vector(%d)", dimensions) {
		return fmt.Errorf("existing AI index uses another model or dimension; use a separate empty AI database and reimport")
	}
	return tx.Commit(ctx)
}

// Package postgres manages shared connection mechanics, never service tables.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"procureflow-ai/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Open(ctx context.Context, url string, timeout time.Duration) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errors.New("invalid DATABASE_URL (details redacted)")
	}
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	cfg.ConnConfig.ConnectTimeout = timeout
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.New("cannot initialize PostgreSQL pool (details redacted)")
	}
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("cannot connect to PostgreSQL; check DATABASE_URL and server availability: %w", Error(pingCtx, err))
	}
	return pool, nil
}

// Error preserves retry/cancellation semantics without exposing SQL or secrets.
func Error(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var dbError *pgconn.PgError
	if errors.As(err, &dbError) {
		switch dbError.Code {
		case "23505":
			return domain.ErrExists
		case "23514", "23502", "22P02", "22003":
			return domain.ErrInvalid
		case "40001", "40P01", "55P03", "57014", "57P01", "57P02", "57P03", "53300":
			return domain.ErrUnavailable
		default:
			return errors.New("database operation failed")
		}
	}
	return domain.ErrUnavailable
}

func IsUUID(id string) bool {
	var value pgtype.UUID
	return value.Scan(id) == nil && value.Valid
}

// Rollback must still release the transaction after its request is canceled.
func Rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

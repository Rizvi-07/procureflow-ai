package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"procureflow-ai/internal/domain"
)

func TestErrorMapping(t *testing.T) {
	for code, want := range map[string]error{"23505": domain.ErrExists, "23514": domain.ErrInvalid, "40001": domain.ErrUnavailable, "40P01": domain.ErrUnavailable} {
		got := Error(context.Background(), &pgconn.PgError{Code: code, Message: "sensitive details"})
		if !errors.Is(got, want) || strings.Contains(got.Error(), "sensitive") {
			t.Fatalf("code=%s mapped to %v", code, got)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(Error(ctx, errors.New("connection lost")), context.Canceled) {
		t.Fatal("context cancellation lost")
	}
	_, err := Open(context.Background(), "postgres://user:do-not-log@host:invalid/db", time.Second)
	if err == nil || strings.Contains(err.Error(), "do-not-log") {
		t.Fatal("invalid DSN not safely rejected")
	}
}

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"procureflow-ai/internal/approval"
	"procureflow-ai/internal/domain"
	"procureflow-ai/internal/postgres"
	"procureflow-ai/internal/purchase"
	"procureflow-ai/migrations"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type database struct {
	pool *pgxpool.Pool
	url  string
}

// Every test creates its own database. Never truncate an existing database.
func testDatabase(t *testing.T) *database {
	t.Helper()
	configured := os.Getenv("TEST_DATABASE_URL")
	if configured == "" {
		t.Skip("PostgreSQL integration NOT RUN: TEST_DATABASE_URL is unset; provide a test server URL with CREATEDB permission")
	}
	parsed, err := url.Parse(configured)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("TEST_DATABASE_URL must be a PostgreSQL URL (value redacted)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := postgres.Open(ctx, configured, 3*time.Second)
	if err != nil {
		t.Skip("PostgreSQL integration NOT RUN: configured test server is unavailable or authentication failed (connection details redacted)")
	}
	t.Cleanup(admin.Close)
	name := "procureflow_test_" + strings.ReplaceAll(domain.NewUUID(), "-", "")
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+pgx.Identifier{name}.Sanitize()); err != nil {
		var dbErr *pgconn.PgError
		if errors.As(err, &dbErr) && dbErr.Code == "42501" {
			t.Skip("PostgreSQL integration NOT RUN: test role needs CREATEDB permission for isolated test databases")
		}
		t.Fatal("cannot create isolated test database")
	}
	db := &database{}
	t.Cleanup(func() {
		if db.pool != nil {
			db.pool.Close()
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, `DROP DATABASE `+pgx.Identifier{name}.Sanitize()+` WITH (FORCE)`); err != nil {
			t.Error("cannot remove isolated test database", name)
		}
	})
	parsed.Path = "/" + name
	db.url = parsed.String()
	db.pool, err = postgres.Open(ctx, db.url, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(ctx, db.pool); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	return db
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func newPurchase(key string) purchase.Purchase {
	return purchase.Purchase{Input: purchase.Input{ClientRequestID: key, EmployeeID: "fictional-employee", ItemName: "Fictional lamp", Quantity: 2, UnitPriceCents: 2500}, ID: domain.NewUUID(), TotalCents: 5000, Status: domain.Pending}
}

func TestPostgresPersistenceAndIdempotency(t *testing.T) {
	db := testDatabase(t)
	ctx := testContext(t)
	pr := purchase.NewPostgresRepository(db.pool, 5*time.Second)
	ar := approval.NewPostgresRepository(db.pool, 5*time.Second)
	p, err := pr.CreateOrGet(ctx, newPurchase("persistent-key"))
	if err != nil {
		t.Fatal(err)
	}
	a, err := ar.CreateOrGet(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != domain.Pending {
		t.Fatal("approval must start pending")
	}
	retryInput := p
	retryInput.ID = domain.NewUUID()
	repeat, err := pr.CreateOrGet(ctx, retryInput)
	if err != nil || repeat != p {
		t.Fatalf("repeat=%+v err=%v", repeat, err)
	}
	retryInput.Quantity++
	retryInput.TotalCents += retryInput.UnitPriceCents
	if _, err := pr.CreateOrGet(ctx, retryInput); !errors.Is(err, domain.ErrExists) {
		t.Fatalf("key conflict=%v", err)
	}
	if _, err := ar.Decide(ctx, p.ID, domain.Approved); err != nil {
		t.Fatal(err)
	}
	if _, err := pr.SetStatus(ctx, p.ID, domain.Approved); err != nil {
		t.Fatal(err)
	}
	// Close all connections and construct fresh repositories, as on a restart.
	db.pool.Close()
	db.pool, err = postgres.Open(ctx, db.url, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	pr = purchase.NewPostgresRepository(db.pool, 5*time.Second)
	ar = approval.NewPostgresRepository(db.pool, 5*time.Second)
	persisted, err := pr.Get(ctx, p.ID)
	if err != nil || persisted.ID != p.ID || persisted.TotalCents != 5000 || persisted.Status != domain.Approved {
		t.Fatalf("persisted=%+v err=%v", persisted, err)
	}
	approvals, err := ar.List(ctx)
	if err != nil || len(approvals) != 1 || approvals[0].Status != domain.Approved {
		t.Fatalf("approvals=%+v err=%v", approvals, err)
	}
	purchases, err := pr.List(ctx)
	if err != nil || len(purchases) != 1 {
		t.Fatalf("purchases=%+v err=%v", purchases, err)
	}
	repeat, err = pr.CreateOrGet(ctx, p)
	if err != nil || repeat.Status != domain.Approved {
		t.Fatalf("create after restart=%+v %v", repeat, err)
	}
	a, err = ar.CreateOrGet(ctx, p.ID)
	if err != nil || a.Status != domain.Approved {
		t.Fatalf("approval after restart=%+v %v", a, err)
	}
	if _, err := pr.Get(ctx, domain.NewUUID()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing purchase=%v", err)
	}
	if _, err := ar.Get(ctx, domain.NewUUID()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing approval=%v", err)
	}
}

func TestPostgresMigrationsAndConstraints(t *testing.T) {
	db := testDatabase(t)
	ctx := testContext(t)
	p := newPurchase("constraint-key")
	pr := purchase.NewPostgresRepository(db.pool, 5*time.Second)
	if _, err := pr.CreateOrGet(ctx, p); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := migrations.Apply(ctx, db.pool); err != nil {
			t.Fatalf("rerun migration: %v", err)
		}
	}
	var count int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM public.procureflow_migrations`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("migration ledger=%d err=%v", count, err)
	}
	if _, err := pr.Get(ctx, p.ID); err != nil {
		t.Fatal("migration rerun lost data", err)
	}
	var zone string
	if err := db.pool.QueryRow(ctx, `SHOW TIME ZONE`).Scan(&zone); err != nil || zone != "UTC" {
		t.Fatalf("timezone=%s err=%v", zone, err)
	}
	for _, schema := range []string{"purchase", "approval"} {
		var typ string
		if err := db.pool.QueryRow(ctx, `SELECT data_type FROM information_schema.columns WHERE table_schema=$1 AND column_name='id'`, schema).Scan(&typ); err != nil || typ != "uuid" {
			t.Fatalf("%s primary key type=%s err=%v", schema, typ, err)
		}
		if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=$1 AND column_name IN ('created_at','updated_at') AND data_type='timestamp with time zone' AND is_nullable='NO'`, schema).Scan(&count); err != nil || count != 2 {
			t.Fatalf("%s timestamp columns=%d err=%v", schema, count, err)
		}
	}
	for _, tc := range []struct {
		name                   string
		quantity, price, total int64
		status                 string
	}{
		{"quantity", 0, 2500, 0, "PENDING"}, {"price", 1, -1, 0, "PENDING"},
		{"total", 1, 1, -1, "PENDING"}, {"mismatch", 2, 5, 9, "PENDING"},
		{"overflow", 2, math.MaxInt64, 0, "PENDING"}, {"status", 1, 1, 1, "UNKNOWN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.pool.Exec(ctx, `INSERT INTO purchase.requests(id,client_request_id,employee_id,item_name,quantity,unit_price_cents,total_cents,status) VALUES ($1::text::uuid,$2,'employee','item',$3,$4,$5,$6)`, domain.NewUUID(), tc.name, tc.quantity, tc.price, tc.total, tc.status)
			wantSQLState(t, err, "23514")
		})
	}
	_, err := db.pool.Exec(ctx, `INSERT INTO purchase.requests(id,client_request_id,employee_id,item_name,quantity,unit_price_cents,total_cents) VALUES ($1::text::uuid,$2,'employee','item',1,1,1)`, domain.NewUUID(), p.ClientRequestID)
	wantSQLState(t, err, "23505")
	_, err = db.pool.Exec(ctx, `INSERT INTO approval.decisions(id,purchase_id,status) VALUES ($1::text::uuid,$2::text::uuid,'UNKNOWN')`, domain.NewUUID(), p.ID)
	wantSQLState(t, err, "23514")
	ar := approval.NewPostgresRepository(db.pool, time.Second)
	if _, err := ar.CreateOrGet(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	_, err = db.pool.Exec(ctx, `INSERT INTO approval.decisions(id,purchase_id) VALUES ($1::text::uuid,$2::text::uuid)`, domain.NewUUID(), p.ID)
	wantSQLState(t, err, "23505")
	var created, updated time.Time
	if err := db.pool.QueryRow(ctx, `SELECT created_at,updated_at FROM purchase.requests WHERE id=$1::text::uuid`, p.ID).Scan(&created, &updated); err != nil || created.IsZero() || updated.Before(created) {
		t.Fatalf("timestamps invalid: %v", err)
	}
	if _, err := pr.SetStatus(ctx, p.ID, domain.Rejected); err != nil {
		t.Fatal(err)
	}
	var decidedAt time.Time
	if err := db.pool.QueryRow(ctx, `SELECT updated_at FROM purchase.requests WHERE id=$1::text::uuid`, p.ID).Scan(&decidedAt); err != nil || decidedAt.Before(updated) {
		t.Fatalf("update timestamp invalid: %v", err)
	}
	if _, err := pr.SetStatus(ctx, p.ID, domain.Rejected); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT updated_at FROM purchase.requests WHERE id=$1::text::uuid`, p.ID).Scan(&updated); err != nil || !updated.Equal(decidedAt) {
		t.Fatalf("identical retry changed timestamp: %v", err)
	}
}

func wantSQLState(t *testing.T, err error, code string) {
	t.Helper()
	var dbErr *pgconn.PgError
	if !errors.As(err, &dbErr) || dbErr.Code != code {
		t.Fatalf("expected SQLSTATE %s; got %v", code, err)
	}
}

func TestPostgresConcurrentIdempotencyAndTransitions(t *testing.T) {
	db := testDatabase(t)
	ctx := testContext(t)
	pr := purchase.NewPostgresRepository(db.pool, 5*time.Second)
	ar := approval.NewPostgresRepository(db.pool, 5*time.Second)
	ids := make(chan string, 16)
	var wg sync.WaitGroup
	for i := 0; i < cap(ids); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := pr.CreateOrGet(ctx, newPurchase("concurrent-key"))
			if err != nil {
				t.Error(err)
				return
			}
			if _, err := ar.CreateOrGet(ctx, p.ID); err != nil {
				t.Error(err)
				return
			}
			ids <- p.ID
		}()
	}
	wg.Wait()
	close(ids)
	id := ""
	for current := range ids {
		if id == "" {
			id = current
		}
		if id != current {
			t.Fatal("duplicate purchase IDs")
		}
	}
	if id == "" {
		t.Fatal("no successful creates")
	}
	pl, err := pr.List(ctx)
	if err != nil || len(pl) != 1 {
		t.Fatalf("purchases=%d err=%v", len(pl), err)
	}
	al, err := ar.List(ctx)
	if err != nil || len(al) != 1 {
		t.Fatalf("approvals=%d err=%v", len(al), err)
	}
	var approved, rejected atomic.Int32
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			decision := domain.Approved
			if i%2 == 0 {
				decision = domain.Rejected
			}
			_, err := ar.Decide(ctx, id, decision)
			if errors.Is(err, domain.ErrConflict) {
				return
			}
			if err != nil {
				t.Error(err)
				return
			}
			if decision == domain.Approved {
				approved.Add(1)
			} else {
				rejected.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if (approved.Load() == 0) == (rejected.Load() == 0) {
		t.Fatal("conflicting decisions committed")
	}
	a, err := ar.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := ar.Decide(ctx, id, a.Status); err != nil {
			t.Fatal(err)
		}
		if _, err := pr.SetStatus(ctx, id, a.Status); err != nil {
			t.Fatal(err)
		}
	}
	other := domain.Approved
	if a.Status == other {
		other = domain.Rejected
	}
	if _, err := pr.SetStatus(ctx, id, other); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("purchase conflict=%v", err)
	}
	if _, err := ar.Decide(ctx, id, domain.Pending); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("pending decision=%v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := pr.List(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	// Hold the row lock and verify that repository deadlines bound lock waits.
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer postgres.Rollback(tx)
	if _, err := tx.Exec(ctx, `SELECT 1 FROM approval.decisions WHERE purchase_id=$1::text::uuid FOR UPDATE`, id); err != nil {
		t.Fatal(err)
	}
	shortRepo := approval.NewPostgresRepository(db.pool, 30*time.Millisecond)
	if _, err := shortRepo.Decide(ctx, id, a.Status); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock deadline=%v", err)
	}
}

type updaterFunc func(context.Context, string, domain.Status) error

func (f updaterFunc) SetApprovalStatus(ctx context.Context, id string, d domain.Status) error {
	return f(ctx, id, d)
}

func TestPostgresDecisionRetryAfterRestart(t *testing.T) {
	db := testDatabase(t)
	ctx := testContext(t)
	pr := purchase.NewPostgresRepository(db.pool, 5*time.Second)
	ar := approval.NewPostgresRepository(db.pool, 5*time.Second)
	p, err := pr.CreateOrGet(ctx, newPurchase("retry-after-restart"))
	if err != nil {
		t.Fatal(err)
	}
	svc := approval.NewService(ar, updaterFunc(func(context.Context, string, domain.Status) error { return domain.ErrUnavailable }), time.Second)
	if _, err := svc.Create(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Decide(ctx, p.ID, domain.Approved); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("propagation failure=%v", err)
	}
	db.pool.Close()
	db.pool, err = postgres.Open(ctx, db.url, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	pr = purchase.NewPostgresRepository(db.pool, 5*time.Second)
	ar = approval.NewPostgresRepository(db.pool, 5*time.Second)
	a, err := ar.Get(ctx, p.ID)
	if err != nil || a.Status != domain.Approved {
		t.Fatalf("decision lost: %+v %v", a, err)
	}
	stillPending, err := pr.Get(ctx, p.ID)
	if err != nil || stillPending.Status != domain.Pending {
		t.Fatalf("purchase=%+v %v", stillPending, err)
	}
	svc = approval.NewService(ar, updaterFunc(func(ctx context.Context, id string, d domain.Status) error {
		_, err := pr.SetStatus(ctx, id, d)
		return err
	}), time.Second)
	if _, err := svc.Decide(ctx, p.ID, domain.Approved); err != nil {
		t.Fatal(err)
	}
	repaired, err := pr.Get(ctx, p.ID)
	if err != nil || repaired.Status != domain.Approved {
		t.Fatal(fmt.Sprintf("repair=%+v err=%v", repaired, err))
	}
}

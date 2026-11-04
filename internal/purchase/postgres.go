package purchase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"procureflow-ai/internal/domain"
	"procureflow-ai/internal/postgres"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

var _ Repository = (*PostgresRepository)(nil)

// The application owns and closes the pool after draining requests.
func NewPostgresRepository(pool *pgxpool.Pool, timeout time.Duration) *PostgresRepository {
	return &PostgresRepository{pool: pool, timeout: timeout}
}

const purchaseColumns = `id::text, client_request_id, employee_id, item_name, quantity, unit_price_cents, total_cents, status`

func scanPurchase(row pgx.Row) (Purchase, error) {
	var p Purchase
	err := row.Scan(&p.ID, &p.ClientRequestID, &p.EmployeeID, &p.ItemName, &p.Quantity, &p.UnitPriceCents, &p.TotalCents, &p.Status)
	return p, err
}

func (r *PostgresRepository) CreateOrGet(ctx context.Context, p Purchase) (Purchase, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	stored, err := scanPurchase(r.pool.QueryRow(ctx, `INSERT INTO purchase.requests
		(id, client_request_id, employee_id, item_name, quantity, unit_price_cents, total_cents, status)
		VALUES ($1::text::uuid,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (client_request_id) DO NOTHING RETURNING `+purchaseColumns,
		p.ID, p.ClientRequestID, p.EmployeeID, p.ItemName, p.Quantity, p.UnitPriceCents, p.TotalCents, p.Status))
	if errors.Is(err, pgx.ErrNoRows) {
		// A separate READ COMMITTED statement sees a concurrent winner's commit.
		stored, err = scanPurchase(r.pool.QueryRow(ctx, `SELECT `+purchaseColumns+` FROM purchase.requests WHERE client_request_id=$1`, p.ClientRequestID))
	}
	if err != nil {
		return Purchase{}, postgres.Error(ctx, err)
	}
	if stored.Input != p.Input {
		return Purchase{}, fmt.Errorf("%w: client_request_id already used with different details", domain.ErrExists)
	}
	return stored, nil
}

func (r *PostgresRepository) Get(ctx context.Context, id string) (Purchase, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	// Legacy syntactically valid lookup keys still mean 'not found', not a SQL error.
	if !postgres.IsUUID(id) {
		return Purchase{}, domain.ErrNotFound
	}
	p, err := scanPurchase(r.pool.QueryRow(ctx, `SELECT `+purchaseColumns+` FROM purchase.requests WHERE id=$1::text::uuid`, id))
	return p, postgres.Error(ctx, err)
}

func (r *PostgresRepository) List(ctx context.Context) ([]Purchase, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `SELECT `+purchaseColumns+` FROM purchase.requests ORDER BY id`)
	if err != nil {
		return nil, postgres.Error(ctx, err)
	}
	defer rows.Close()
	items := make([]Purchase, 0)
	for rows.Next() {
		p, err := scanPurchase(rows)
		if err != nil {
			return nil, postgres.Error(ctx, err)
		}
		items = append(items, p)
	}
	return items, postgres.Error(ctx, rows.Err())
}

func (r *PostgresRepository) SetStatus(ctx context.Context, id string, decision domain.Status) (Purchase, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if err := domain.ValidateDecision(decision); err != nil {
		return Purchase{}, err
	}
	if !postgres.IsUUID(id) {
		return Purchase{}, domain.ErrNotFound
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Purchase{}, postgres.Error(ctx, err)
	}
	defer postgres.Rollback(tx)
	p, err := scanPurchase(tx.QueryRow(ctx, `SELECT `+purchaseColumns+` FROM purchase.requests WHERE id=$1::text::uuid FOR UPDATE`, id))
	if err != nil {
		return Purchase{}, postgres.Error(ctx, err)
	}
	if p.Status != domain.Pending && p.Status != decision {
		return Purchase{}, fmt.Errorf("%w: purchase already %s", domain.ErrConflict, p.Status)
	}
	if p.Status != decision {
		p, err = scanPurchase(tx.QueryRow(ctx, `UPDATE purchase.requests SET status=$2, updated_at=clock_timestamp() WHERE id=$1::text::uuid RETURNING `+purchaseColumns, id, decision))
		if err != nil {
			return Purchase{}, postgres.Error(ctx, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Purchase{}, postgres.Error(ctx, err)
	}
	return p, nil
}

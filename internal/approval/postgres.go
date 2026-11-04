package approval

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

func NewPostgresRepository(pool *pgxpool.Pool, timeout time.Duration) *PostgresRepository {
	return &PostgresRepository{pool: pool, timeout: timeout}
}

const approvalColumns = `purchase_id::text, status`

func scanApproval(row pgx.Row) (Approval, error) {
	var a Approval
	err := row.Scan(&a.PurchaseID, &a.Status)
	return a, err
}

func (r *PostgresRepository) CreateOrGet(ctx context.Context, id string) (Approval, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if !postgres.IsUUID(id) {
		return Approval{}, fmt.Errorf("%w: purchase_id must be a purchase UUID", domain.ErrInvalid)
	}
	a, err := scanApproval(r.pool.QueryRow(ctx, `INSERT INTO approval.decisions(id, purchase_id)
		VALUES ($1::text::uuid,$2::text::uuid) ON CONFLICT (purchase_id) DO NOTHING RETURNING `+approvalColumns, domain.NewUUID(), id))
	if errors.Is(err, pgx.ErrNoRows) {
		a, err = scanApproval(r.pool.QueryRow(ctx, `SELECT `+approvalColumns+` FROM approval.decisions WHERE purchase_id=$1::text::uuid`, id))
	}
	return a, postgres.Error(ctx, err)
}

func (r *PostgresRepository) Get(ctx context.Context, id string) (Approval, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if !postgres.IsUUID(id) {
		return Approval{}, domain.ErrNotFound
	}
	a, err := scanApproval(r.pool.QueryRow(ctx, `SELECT `+approvalColumns+` FROM approval.decisions WHERE purchase_id=$1::text::uuid`, id))
	return a, postgres.Error(ctx, err)
}

func (r *PostgresRepository) List(ctx context.Context) ([]Approval, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `SELECT `+approvalColumns+` FROM approval.decisions ORDER BY purchase_id`)
	if err != nil {
		return nil, postgres.Error(ctx, err)
	}
	defer rows.Close()
	items := make([]Approval, 0)
	for rows.Next() {
		a, err := scanApproval(rows)
		if err != nil {
			return nil, postgres.Error(ctx, err)
		}
		items = append(items, a)
	}
	return items, postgres.Error(ctx, rows.Err())
}

func (r *PostgresRepository) Decide(ctx context.Context, id string, decision domain.Status) (Approval, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if err := domain.ValidateDecision(decision); err != nil {
		return Approval{}, err
	}
	if !postgres.IsUUID(id) {
		return Approval{}, domain.ErrNotFound
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Approval{}, postgres.Error(ctx, err)
	}
	defer postgres.Rollback(tx)
	a, err := scanApproval(tx.QueryRow(ctx, `SELECT `+approvalColumns+` FROM approval.decisions WHERE purchase_id=$1::text::uuid FOR UPDATE`, id))
	if err != nil {
		return Approval{}, postgres.Error(ctx, err)
	}
	if a.Status != domain.Pending && a.Status != decision {
		return Approval{}, fmt.Errorf("%w: approval already %s", domain.ErrConflict, a.Status)
	}
	if a.Status != decision {
		a, err = scanApproval(tx.QueryRow(ctx, `UPDATE approval.decisions SET status=$2, updated_at=clock_timestamp() WHERE purchase_id=$1::text::uuid RETURNING `+approvalColumns, id, decision))
		if err != nil {
			return Approval{}, postgres.Error(ctx, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Approval{}, postgres.Error(ctx, err)
	}
	return a, nil
}

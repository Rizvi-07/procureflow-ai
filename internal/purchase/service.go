package purchase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"procureflow-ai/internal/domain"
)

type ApprovalCreator interface {
	CreateApproval(context.Context, string) error
}

type Service struct {
	repo      *Repository
	approvals ApprovalCreator
	timeout   time.Duration
}

func NewService(repo *Repository, approvals ApprovalCreator, timeout time.Duration) *Service {
	return &Service{repo: repo, approvals: approvals, timeout: timeout}
}

func Validate(in Input) (int64, error) {
	if err := domain.ValidateID("client_request_id", in.ClientRequestID); err != nil {
		return 0, err
	}
	if err := domain.ValidateID("employee_id", in.EmployeeID); err != nil {
		return 0, err
	}
	if !utf8.ValidString(in.ItemName) || strings.TrimSpace(in.ItemName) == "" || utf8.RuneCountInString(in.ItemName) > 200 {
		return 0, fmt.Errorf("%w: item_name must contain 1-200 characters and not be blank", domain.ErrInvalid)
	}
	if in.Quantity <= 0 {
		return 0, fmt.Errorf("%w: quantity must be positive", domain.ErrInvalid)
	}
	if in.UnitPriceCents < 0 {
		return 0, fmt.Errorf("%w: unit_price_cents cannot be negative", domain.ErrInvalid)
	}
	// Divide before multiplying, so the overflow check itself cannot overflow.
	if in.UnitPriceCents > math.MaxInt64/in.Quantity {
		return 0, fmt.Errorf("%w: total_cents exceeds int64", domain.ErrInvalid)
	}
	return in.Quantity * in.UnitPriceCents, nil
}

func (s *Service) Create(ctx context.Context, in Input) (Purchase, error) {
	if err := ctx.Err(); err != nil {
		return Purchase{}, err
	}
	total, err := Validate(in)
	if err != nil {
		return Purchase{}, err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Purchase{}, fmt.Errorf("generate purchase ID: %w", err)
	}
	p, err := s.repo.CreateOrGet(Purchase{Input: in, ID: hex.EncodeToString(id[:]), TotalCents: total, Status: domain.Pending})
	if err != nil {
		return Purchase{}, err
	}
	// Commit locally before the RPC. Retrying the same key repairs a failed handoff.
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	if err := s.approvals.CreateApproval(callCtx, p.ID); err != nil {
		return Purchase{}, fmt.Errorf("purchase saved; retry CreatePurchase with the same client_request_id and details to ensure approval: %w", err)
	}
	return s.repo.Get(p.ID)
}

func (s *Service) Get(ctx context.Context, id string) (Purchase, error) {
	if err := ctx.Err(); err != nil {
		return Purchase{}, err
	}
	if err := domain.ValidateID("purchase_id", id); err != nil {
		return Purchase{}, err
	}
	return s.repo.Get(id)
}

func (s *Service) List(ctx context.Context) ([]Purchase, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.repo.List(), nil
}

func (s *Service) SetApprovalStatus(ctx context.Context, id string, decision domain.Status) (Purchase, error) {
	if err := ctx.Err(); err != nil {
		return Purchase{}, err
	}
	if err := domain.ValidateID("purchase_id", id); err != nil {
		return Purchase{}, err
	}
	if err := domain.ValidateDecision(decision); err != nil {
		return Purchase{}, err
	}
	return s.repo.SetStatus(id, decision)
}

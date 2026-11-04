package approval

import (
	"context"
	"fmt"
	"time"

	"procureflow-ai/internal/domain"
)

type PurchaseUpdater interface {
	SetApprovalStatus(context.Context, string, domain.Status) error
}

type Service struct {
	repo      *Repository
	purchases PurchaseUpdater
	timeout   time.Duration
}

func NewService(repo *Repository, purchases PurchaseUpdater, timeout time.Duration) *Service {
	return &Service{repo: repo, purchases: purchases, timeout: timeout}
}

func (s *Service) Create(ctx context.Context, id string) (Approval, error) {
	if err := ctx.Err(); err != nil {
		return Approval{}, err
	}
	if err := domain.ValidateID("purchase_id", id); err != nil {
		return Approval{}, err
	}
	return s.repo.CreateOrGet(id), nil
}

func (s *Service) Get(ctx context.Context, id string) (Approval, error) {
	if err := ctx.Err(); err != nil {
		return Approval{}, err
	}
	if err := domain.ValidateID("purchase_id", id); err != nil {
		return Approval{}, err
	}
	return s.repo.Get(id)
}

func (s *Service) List(ctx context.Context) ([]Approval, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.repo.List(), nil
}

func (s *Service) Decide(ctx context.Context, id string, decision domain.Status) (Approval, error) {
	if err := ctx.Err(); err != nil {
		return Approval{}, err
	}
	if err := domain.ValidateID("purchase_id", id); err != nil {
		return Approval{}, err
	}
	if err := domain.ValidateDecision(decision); err != nil {
		return Approval{}, err
	}
	a, err := s.repo.Decide(id, decision)
	if err != nil {
		return Approval{}, err
	}
	// Always propagate identical retries: the previous response may have been lost.
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	if err := s.purchases.SetApprovalStatus(callCtx, id, decision); err != nil {
		return Approval{}, fmt.Errorf("decision saved; retry DecideApproval with the same purchase_id and status to synchronize purchase: %w", err)
	}
	return a, nil
}

package approval

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"procureflow-ai/internal/domain"
)

type Approval struct {
	PurchaseID string
	Status     domain.Status
}

type Repository interface {
	CreateOrGet(context.Context, string) (Approval, error)
	Get(context.Context, string) (Approval, error)
	List(context.Context) ([]Approval, error)
	Decide(context.Context, string, domain.Status) (Approval, error)
}

type MemoryRepository struct {
	mu    sync.RWMutex
	items map[string]Approval
}

func NewRepository() *MemoryRepository { return &MemoryRepository{items: make(map[string]Approval)} }

func (r *MemoryRepository) CreateOrGet(ctx context.Context, id string) (Approval, error) {
	if err := ctx.Err(); err != nil {
		return Approval{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if a, ok := r.items[id]; ok {
		return a, nil
	}
	a := Approval{PurchaseID: id, Status: domain.Pending}
	r.items[id] = a
	return a, nil
}

func (r *MemoryRepository) Get(ctx context.Context, id string) (Approval, error) {
	if err := ctx.Err(); err != nil {
		return Approval{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.items[id]
	if !ok {
		return Approval{}, fmt.Errorf("%w: approval", domain.ErrNotFound)
	}
	return a, nil
}

func (r *MemoryRepository) List(ctx context.Context) ([]Approval, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]Approval, 0, len(r.items))
	for _, a := range r.items {
		items = append(items, a)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].PurchaseID < items[j].PurchaseID })
	return items, nil
}

func (r *MemoryRepository) Decide(ctx context.Context, id string, decision domain.Status) (Approval, error) {
	if err := ctx.Err(); err != nil {
		return Approval{}, err
	}
	if err := domain.ValidateDecision(decision); err != nil {
		return Approval{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.items[id]
	if !ok {
		return Approval{}, fmt.Errorf("%w: approval", domain.ErrNotFound)
	}
	if a.Status != domain.Pending && a.Status != decision {
		return Approval{}, fmt.Errorf("%w: approval already %s", domain.ErrConflict, a.Status)
	}
	a.Status = decision
	r.items[id] = a
	return a, nil
}

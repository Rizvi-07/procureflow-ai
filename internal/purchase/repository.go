package purchase

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"procureflow-ai/internal/domain"
)

type Input struct {
	ClientRequestID string
	EmployeeID      string
	ItemName        string
	Quantity        int64
	UnitPriceCents  int64
}

type Purchase struct {
	Input
	ID         string
	TotalCents int64
	Status     domain.Status
}

type Repository interface {
	CreateOrGet(context.Context, Purchase) (Purchase, error)
	Get(context.Context, string) (Purchase, error)
	List(context.Context) ([]Purchase, error)
	SetStatus(context.Context, string, domain.Status) (Purchase, error)
}

// MemoryRepository returns values, never pointers into shared maps.
type MemoryRepository struct {
	mu        sync.RWMutex
	byID      map[string]Purchase
	byRequest map[string]string
}

func NewRepository() *MemoryRepository {
	return &MemoryRepository{byID: make(map[string]Purchase), byRequest: make(map[string]string)}
}

// CreateOrGet checks the idempotency key and inserts under the same lock.
func (r *MemoryRepository) CreateOrGet(ctx context.Context, p Purchase) (Purchase, error) {
	if err := ctx.Err(); err != nil {
		return Purchase{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if id, ok := r.byRequest[p.ClientRequestID]; ok {
		existing := r.byID[id]
		if existing.Input != p.Input {
			return Purchase{}, fmt.Errorf("%w: client_request_id already used with different details", domain.ErrExists)
		}
		return existing, nil
	}
	r.byID[p.ID] = p
	r.byRequest[p.ClientRequestID] = p.ID
	return p, nil
}

func (r *MemoryRepository) Get(ctx context.Context, id string) (Purchase, error) {
	if err := ctx.Err(); err != nil {
		return Purchase{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byID[id]
	if !ok {
		return Purchase{}, fmt.Errorf("%w: purchase", domain.ErrNotFound)
	}
	return p, nil
}

func (r *MemoryRepository) List(ctx context.Context) ([]Purchase, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]Purchase, 0, len(r.byID))
	for _, p := range r.byID {
		items = append(items, p)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *MemoryRepository) SetStatus(ctx context.Context, id string, decision domain.Status) (Purchase, error) {
	if err := ctx.Err(); err != nil {
		return Purchase{}, err
	}
	if err := domain.ValidateDecision(decision); err != nil {
		return Purchase{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byID[id]
	if !ok {
		return Purchase{}, fmt.Errorf("%w: purchase", domain.ErrNotFound)
	}
	if p.Status != domain.Pending && p.Status != decision {
		return Purchase{}, fmt.Errorf("%w: purchase already %s", domain.ErrConflict, p.Status)
	}
	p.Status = decision
	r.byID[id] = p
	return p, nil
}

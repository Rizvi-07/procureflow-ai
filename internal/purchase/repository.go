package purchase

import (
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

// Repository returns values, never pointers into shared maps.
type Repository struct {
	mu        sync.RWMutex
	byID      map[string]Purchase
	byRequest map[string]string
}

func NewRepository() *Repository {
	return &Repository{byID: make(map[string]Purchase), byRequest: make(map[string]string)}
}

// CreateOrGet checks the idempotency key and inserts under the same lock.
func (r *Repository) CreateOrGet(p Purchase) (Purchase, error) {
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

func (r *Repository) Get(id string) (Purchase, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byID[id]
	if !ok {
		return Purchase{}, fmt.Errorf("%w: purchase", domain.ErrNotFound)
	}
	return p, nil
}

func (r *Repository) List() []Purchase {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]Purchase, 0, len(r.byID))
	for _, p := range r.byID {
		items = append(items, p)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func (r *Repository) SetStatus(id string, decision domain.Status) (Purchase, error) {
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

package approval

import (
	"fmt"
	"sort"
	"sync"

	"procureflow-ai/internal/domain"
)

type Approval struct {
	PurchaseID string
	Status     domain.Status
}

type Repository struct {
	mu    sync.RWMutex
	items map[string]Approval
}

func NewRepository() *Repository { return &Repository{items: make(map[string]Approval)} }

func (r *Repository) CreateOrGet(id string) Approval {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a, ok := r.items[id]; ok {
		return a
	}
	a := Approval{PurchaseID: id, Status: domain.Pending}
	r.items[id] = a
	return a
}

func (r *Repository) Get(id string) (Approval, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.items[id]
	if !ok {
		return Approval{}, fmt.Errorf("%w: approval", domain.ErrNotFound)
	}
	return a, nil
}

func (r *Repository) List() []Approval {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]Approval, 0, len(r.items))
	for _, a := range r.items {
		items = append(items, a)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].PurchaseID < items[j].PurchaseID })
	return items
}

func (r *Repository) Decide(id string, decision domain.Status) (Approval, error) {
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

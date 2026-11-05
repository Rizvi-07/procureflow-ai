package approval

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"procureflow-ai/internal/domain"
)

type updaterFunc func(context.Context, string, domain.Status) error

func (f updaterFunc) SetApprovalStatus(ctx context.Context, id string, decision domain.Status) error {
	return f(ctx, id, decision)
}

func TestDecisionsAndRetry(t *testing.T) {
	for _, decision := range []domain.Status{domain.Approved, domain.Rejected} {
		t.Run(string(decision), func(t *testing.T) {
			ctx := context.Background()
			calls := 0
			svc := NewService(NewRepository(), updaterFunc(func(ctx context.Context, id string, status domain.Status) error {
				calls++
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("missing deadline")
				}
				if id != "purchase-1" || status != decision {
					t.Fatalf("unexpected propagation %s %s", id, status)
				}
				if calls == 1 {
					return domain.ErrUnavailable
				}
				return nil
			}), time.Second)
			a, err := svc.Create(ctx, "purchase-1")
			if err != nil || a.Status != domain.Pending {
				t.Fatalf("create=%+v %v", a, err)
			}
			if _, err := svc.Decide(ctx, a.PurchaseID, decision); !errors.Is(err, domain.ErrUnavailable) {
				t.Fatalf("failure=%v", err)
			}
			saved, _ := svc.Get(ctx, a.PurchaseID)
			if saved.Status != decision {
				t.Fatal("decision not saved before propagation")
			}
			for i := 0; i < 2; i++ {
				if _, err := svc.Decide(ctx, a.PurchaseID, decision); err != nil {
					t.Fatal(err)
				}
			}
			if calls != 3 {
				t.Fatalf("propagations=%d", calls)
			}
			repeat, _ := svc.Create(ctx, a.PurchaseID)
			if repeat.Status != decision {
				t.Fatal("create reset decision")
			}
			other := domain.Approved
			if decision == other {
				other = domain.Rejected
			}
			if _, err := svc.Decide(ctx, a.PurchaseID, other); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("conflict=%v", err)
			}
			afterConflict, err := svc.Get(ctx, a.PurchaseID)
			if err != nil || afterConflict.Status != decision {
				t.Fatalf("conflicting decision changed saved approval=%+v error=%v", afterConflict, err)
			}
			for _, invalid := range []domain.Status{domain.Pending, "", "UNKNOWN"} {
				if _, err := svc.Decide(ctx, a.PurchaseID, invalid); !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("invalid=%v", err)
				}
			}
			if _, err := svc.Decide(ctx, "missing", decision); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("missing=%v", err)
			}
			if _, err := svc.Create(ctx, " "); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("invalid id=%v", err)
			}
			if calls != 3 {
				t.Fatal("invalid/conflicting decision propagated")
			}
		})
	}
}

func TestConcurrentApprovalTransitions(t *testing.T) {
	var approved, rejected atomic.Int32
	svc := NewService(NewRepository(), updaterFunc(func(_ context.Context, _ string, d domain.Status) error {
		if d == domain.Approved {
			approved.Add(1)
		} else {
			rejected.Add(1)
		}
		return nil
	}), time.Second)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := svc.Create(ctx, "purchase-1"); err != nil {
				t.Error(err)
				return
			}
			d := domain.Approved
			if i%2 == 0 {
				d = domain.Rejected
			}
			if _, err := svc.Decide(ctx, "purchase-1", d); err != nil && !errors.Is(err, domain.ErrConflict) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	items, _ := svc.List(ctx)
	if len(items) != 1 {
		t.Fatalf("approvals=%d", len(items))
	}
	if (approved.Load() == 0) == (rejected.Load() == 0) {
		t.Fatalf("conflicting outcomes propagated: approved=%d rejected=%d", approved.Load(), rejected.Load())
	}
}

func TestDecisionDeadline(t *testing.T) {
	svc := NewService(NewRepository(), updaterFunc(func(ctx context.Context, _ string, _ domain.Status) error { <-ctx.Done(); return ctx.Err() }), 10*time.Millisecond)
	ctx := context.Background()
	if _, err := svc.Create(ctx, "purchase-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Decide(ctx, "purchase-1", domain.Approved); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline=%v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := svc.Decide(canceled, "purchase-1", domain.Approved); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

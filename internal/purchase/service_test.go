package purchase

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"procureflow-ai/internal/domain"
)

type creatorFunc func(context.Context, string) error

func (f creatorFunc) CreateApproval(ctx context.Context, id string) error { return f(ctx, id) }

func validInput() Input {
	return Input{ClientRequestID: "request-1", EmployeeID: "employee-1", ItemName: "Desk lamp", Quantity: 2, UnitPriceCents: 2500}
}

func TestValidate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*Input)
		total   int64
		invalid bool
	}{
		{"normal", func(*Input) {}, 5000, false},
		{"zero price", func(p *Input) { p.UnitPriceCents = 0 }, 0, false},
		{"maximum total", func(p *Input) { p.Quantity = 1; p.UnitPriceCents = math.MaxInt64 }, math.MaxInt64, false},
		{"maximum quantity free", func(p *Input) { p.Quantity = math.MaxInt64; p.UnitPriceCents = 0 }, 0, false},
		{"overflow", func(p *Input) { p.UnitPriceCents = math.MaxInt64 }, 0, true},
		{"empty employee", func(p *Input) { p.EmployeeID = "" }, 0, true},
		{"blank employee", func(p *Input) { p.EmployeeID = " " }, 0, true},
		{"blank item", func(p *Input) { p.ItemName = " \t\n" }, 0, true},
		{"long item", func(p *Input) { p.ItemName = strings.Repeat("a", 201) }, 0, true},
		{"invalid utf8", func(p *Input) { p.ItemName = string([]byte{0xff}) }, 0, true},
		{"zero quantity", func(p *Input) { p.Quantity = 0 }, 0, true},
		{"negative quantity", func(p *Input) { p.Quantity = -1 }, 0, true},
		{"negative price", func(p *Input) { p.UnitPriceCents = -1 }, 0, true},
		{"empty request key", func(p *Input) { p.ClientRequestID = "" }, 0, true},
		{"invalid request key", func(p *Input) { p.ClientRequestID = "bad key!" }, 0, true},
		{"long request key", func(p *Input) { p.ClientRequestID = strings.Repeat("a", 129) }, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput()
			tc.change(&in)
			total, err := Validate(in)
			if tc.invalid {
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil || total != tc.total {
				t.Fatalf("total=%d, err=%v", total, err)
			}
		})
	}
}

func TestConcurrentCreateIdempotency(t *testing.T) {
	repo := NewRepository()
	svc := NewService(repo, creatorFunc(func(context.Context, string) error { return nil }), time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := svc.Create(context.Background(), validInput())
			if err != nil || p.TotalCents != 5000 || p.Status != domain.Pending {
				t.Errorf("purchase=%+v err=%v", p, err)
			}
		}()
	}
	wg.Wait()
	items, err := repo.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("created %d purchases", len(items))
	}
	p, err := svc.Create(context.Background(), validInput())
	if err != nil || p.ID != items[0].ID {
		t.Fatalf("repeat=%+v err=%v", p, err)
	}
	changed := validInput()
	changed.Quantity++
	if _, err := svc.Create(context.Background(), changed); !errors.Is(err, domain.ErrExists) {
		t.Fatalf("conflict=%v", err)
	}
	items[0].ItemName = "modified snapshot"
	fresh, _ := repo.Get(context.Background(), p.ID)
	if fresh.ItemName != validInput().ItemName {
		t.Fatal("list exposed shared mutable state")
	}
}

func TestCreateDeadlineAndRetry(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository()
	calls := 0
	svc := NewService(repo, creatorFunc(func(ctx context.Context, _ string) error {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("missing deadline")
		}
		if calls == 1 {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}), 10*time.Millisecond)
	if _, err := svc.Create(ctx, validInput()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v", err)
	}
	stored, err := repo.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 {
		t.Fatal("purchase must survive RPC failure")
	}
	p, err := svc.Create(ctx, validInput())
	if err != nil || p.ID != stored[0].ID || calls != 2 {
		t.Fatalf("retry=%+v error=%v calls=%d", p, err, calls)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := svc.Create(canceled, validInput()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

func TestPurchaseTransitions(t *testing.T) {
	for _, decision := range []domain.Status{domain.Approved, domain.Rejected} {
		t.Run(string(decision), func(t *testing.T) {
			ctx := context.Background()
			svc := NewService(NewRepository(), creatorFunc(func(context.Context, string) error { return nil }), time.Second)
			p, err := svc.Create(ctx, validInput())
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if _, err := svc.SetApprovalStatus(ctx, p.ID, decision); err != nil {
					t.Fatal(err)
				}
			}
			other := domain.Approved
			if decision == other {
				other = domain.Rejected
			}
			if _, err := svc.SetApprovalStatus(ctx, p.ID, other); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("conflict=%v", err)
			}
			if _, err := svc.SetApprovalStatus(ctx, p.ID, domain.Pending); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("pending=%v", err)
			}
			if _, err := svc.SetApprovalStatus(ctx, "missing", decision); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("missing=%v", err)
			}
		})
	}
}

func TestInvalidCreateLeavesNoPurchaseOrApproval(t *testing.T) {
	for _, tc := range []struct {
		name     string
		quantity int64
		price    int64
	}{
		{"zero quantity", 0, 2500},
		{"negative quantity", -1, 2500},
		{"negative price", 2, -1},
		{"amount overflow", 2, math.MaxInt64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := NewRepository()
			svc := NewService(repo, creatorFunc(func(context.Context, string) error {
				t.Fatal("invalid purchase reached approval service")
				return nil
			}), time.Second)
			in := validInput()
			in.Quantity, in.UnitPriceCents = tc.quantity, tc.price
			if _, err := svc.Create(context.Background(), in); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("Create error=%v; want ErrInvalid", err)
			}
			items, err := repo.List(context.Background())
			if err != nil || len(items) != 0 {
				t.Fatalf("invalid create stored purchases=%v error=%v", items, err)
			}
		})
	}
}

func TestDuplicateCreatePreservesOriginalDetails(t *testing.T) {
	ctx := context.Background()
	approvalCalls := 0
	svc := NewService(NewRepository(), creatorFunc(func(context.Context, string) error {
		approvalCalls++
		return nil
	}), time.Second)
	original, err := svc.Create(ctx, validInput())
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := svc.Create(ctx, validInput())
	if err != nil || repeated != original {
		t.Fatalf("duplicate=%+v original=%+v error=%v", repeated, original, err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Input)
	}{
		{"employee", func(in *Input) { in.EmployeeID = "another-employee" }},
		{"item", func(in *Input) { in.ItemName = "Another fictional item" }},
		{"quantity", func(in *Input) { in.Quantity++ }},
		{"price", func(in *Input) { in.UnitPriceCents++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := validInput()
			tc.change(&changed)
			if _, err := svc.Create(ctx, changed); !errors.Is(err, domain.ErrExists) {
				t.Fatalf("changed duplicate error=%v; want ErrExists", err)
			}
			stored, err := svc.Get(ctx, original.ID)
			if err != nil || stored != original {
				t.Fatalf("original changed: %+v error=%v", stored, err)
			}
		})
	}
	if approvalCalls != 2 {
		t.Fatalf("approval calls=%d; conflicting creates must not propagate", approvalCalls)
	}
}

func TestGetMissingPurchase(t *testing.T) {
	svc := NewService(NewRepository(), creatorFunc(func(context.Context, string) error { return nil }), time.Second)
	if _, err := svc.Get(context.Background(), domain.NewUUID()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get error=%v; want ErrNotFound", err)
	}
}

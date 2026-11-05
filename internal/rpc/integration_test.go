package rpc_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "procureflow-ai/gen/procureflow/v1"
	"procureflow-ai/internal/approval"
	"procureflow-ai/internal/auth"
	"procureflow-ai/internal/purchase"
	"procureflow-ai/internal/rpc"
	"procureflow-ai/internal/server"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

// These wrappers inject faults at the wire boundary; successful calls still run
// the real handlers, repositories, generated clients and protobuf encoding.
type faultyApproval struct {
	*rpc.ApprovalHandler
	loseReply atomic.Bool
	block     atomic.Bool
}

func (h *faultyApproval) CreateApproval(ctx context.Context, req *pb.CreateApprovalRequest) (*pb.Approval, error) {
	if h.block.Load() {
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	a, err := h.ApprovalHandler.CreateApproval(ctx, req)
	if err == nil && h.loseReply.Swap(false) {
		return nil, status.Error(codes.Unavailable, "injected lost response")
	}
	return a, err
}

type faultyPurchase struct {
	*rpc.PurchaseHandler
	unavailable atomic.Bool
	loseReply   atomic.Bool
	block       atomic.Bool
}

func (h *faultyPurchase) SetApprovalStatus(ctx context.Context, req *pb.SetApprovalStatusRequest) (*pb.Purchase, error) {
	if h.unavailable.Load() {
		return nil, status.Error(codes.Unavailable, "injected unavailable peer")
	}
	if h.block.Load() {
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	p, err := h.PurchaseHandler.SetApprovalStatus(ctx, req)
	if err == nil && h.loseReply.Swap(false) {
		return nil, status.Error(codes.Unavailable, "injected lost response")
	}
	return p, err
}

type pair struct {
	purchaseTarget string
	approvalTarget string
	p              pb.PurchaseServiceClient
	a              pb.ApprovalServiceClient
	ph             *faultyPurchase
	ah             *faultyApproval
}

var transportKey = []byte("01234567890123456789012345678901")

// Existing transport scenarios deliberately exercise authorized identities.
// Separate authorization tests below use fixed actors, never method-based roles.
func scenarioClient(target string) (*grpc.ClientConn, error) {
	return grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		kind, id, role := "actor", "employee-demo", auth.Manager
		switch method {
		case "/procureflow.v1.PurchaseService/SetApprovalStatus":
			kind, id, role = "service", "approval-service", ""
		case "/procureflow.v1.ApprovalService/CreateApproval":
			kind, id, role = "service", "purchase-service", ""
		case "/procureflow.v1.ApprovalService/DecideApproval":
			id = "manager-demo"
		}
		out, err := auth.Outgoing(ctx, transportKey, kind, id, role)
		if err != nil {
			return err
		}
		return invoke(out, method, req, reply, cc, opts...)
	}))
}

func startPair(t *testing.T) pair {
	t.Helper()
	pl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pl.Close() })
	al, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { al.Close() })
	pc, err := scenarioClient(pl.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	ac, err := scenarioClient(al.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ac.Close() })
	p := pb.NewPurchaseServiceClient(pc)
	a := pb.NewApprovalServiceClient(ac)
	ph := &faultyPurchase{PurchaseHandler: &rpc.PurchaseHandler{Service: purchase.NewService(purchase.NewRepository(), rpc.ApprovalClient{Client: a}, 100*time.Millisecond)}}
	ownerConnection, err := grpc.NewClient(pl.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(auth.ServiceClient(transportKey, "approval-service")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ownerConnection.Close() })
	ah := &faultyApproval{ApprovalHandler: &rpc.ApprovalHandler{Service: approval.NewService(approval.NewRepository(), rpc.PurchaseClient{Client: p}, 100*time.Millisecond), Owners: pb.NewPurchaseServiceClient(ownerConnection)}}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	ps := server.New(logger, pb.PurchaseService_ServiceDesc.ServiceName, func(s *grpc.Server) { pb.RegisterPurchaseServiceServer(s, ph) }, grpc.ChainUnaryInterceptor(auth.ServerInterceptor(transportKey)))
	as := server.New(logger, pb.ApprovalService_ServiceDesc.ServiceName, func(s *grpc.Server) { pb.RegisterApprovalServiceServer(s, ah) }, grpc.ChainUnaryInterceptor(auth.ServerInterceptor(transportKey)))
	t.Cleanup(ps.Stop)
	t.Cleanup(as.Stop)
	go func() { _ = ps.Serve(pl) }()
	go func() { _ = as.Serve(al) }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, entry := range []struct {
		conn *grpc.ClientConn
		name string
	}{{pc, pb.PurchaseService_ServiceDesc.ServiceName}, {ac, pb.ApprovalService_ServiceDesc.ServiceName}} {
		h, err := healthpb.NewHealthClient(entry.conn).Check(ctx, &healthpb.HealthCheckRequest{Service: entry.name}, grpc.WaitForReady(true))
		if err != nil || h.GetStatus() != healthpb.HealthCheckResponse_SERVING {
			t.Fatalf("health=%v err=%v", h, err)
		}
	}
	return pair{p: p, a: a, ph: ph, ah: ah, purchaseTarget: pl.Addr().String(), approvalTarget: al.Addr().String()}
}

func input() *pb.CreatePurchaseRequest {
	return &pb.CreatePurchaseRequest{ClientRequestId: "demo-request", EmployeeId: "employee-demo", ItemName: "Fictional desk lamp", Quantity: 2, UnitPriceCents: 2500}
}

func callContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func wantCode(t *testing.T, err error, code codes.Code) {
	t.Helper()
	if status.Code(err) != code {
		t.Fatalf("code=%s want=%s error=%v", status.Code(err), code, err)
	}
}

func TestGRPCCreateApproveReadAndErrors(t *testing.T) {
	s := startPair(t)
	ctx := callContext(t)
	p, err := s.p.CreatePurchase(ctx, input())
	if err != nil {
		t.Fatal(err)
	}
	if p.TotalCents != 5000 || p.Status != pb.Status_STATUS_PENDING {
		t.Fatalf("purchase=%v", p)
	}
	a, err := s.a.GetApproval(ctx, &pb.GetApprovalRequest{PurchaseId: p.Id})
	if err != nil || a.GetStatus() != pb.Status_STATUS_PENDING {
		t.Fatalf("approval=%v err=%v", a, err)
	}
	for i := 0; i < 2; i++ {
		_, err := s.a.DecideApproval(ctx, &pb.DecideApprovalRequest{PurchaseId: p.Id, Status: pb.Status_STATUS_APPROVED})
		if err != nil {
			t.Fatal(err)
		}
	}
	fresh, err := s.p.GetPurchase(ctx, &pb.GetPurchaseRequest{PurchaseId: p.Id})
	if err != nil || fresh.GetStatus() != pb.Status_STATUS_APPROVED {
		t.Fatalf("read=%v err=%v", fresh, err)
	}
	repeat, err := s.p.CreatePurchase(ctx, input())
	if err != nil || repeat.GetId() != p.Id || repeat.GetStatus() != pb.Status_STATUS_APPROVED {
		t.Fatalf("repeat=%v err=%v", repeat, err)
	}
	changed := input()
	changed.Quantity++
	_, err = s.p.CreatePurchase(ctx, changed)
	wantCode(t, err, codes.AlreadyExists)
	_, err = s.a.DecideApproval(ctx, &pb.DecideApprovalRequest{PurchaseId: p.Id, Status: pb.Status_STATUS_REJECTED})
	wantCode(t, err, codes.FailedPrecondition)
	_, err = s.p.SetApprovalStatus(ctx, &pb.SetApprovalStatusRequest{PurchaseId: p.Id, Status: pb.Status_STATUS_REJECTED})
	wantCode(t, err, codes.FailedPrecondition)
	_, err = s.a.DecideApproval(ctx, &pb.DecideApprovalRequest{PurchaseId: p.Id, Status: pb.Status(99)})
	wantCode(t, err, codes.InvalidArgument)
	_, err = s.p.SetApprovalStatus(ctx, &pb.SetApprovalStatusRequest{PurchaseId: p.Id, Status: pb.Status_STATUS_PENDING})
	wantCode(t, err, codes.InvalidArgument)
	_, err = s.p.CreatePurchase(ctx, &pb.CreatePurchaseRequest{})
	wantCode(t, err, codes.InvalidArgument)
	_, err = s.a.CreateApproval(ctx, &pb.CreateApprovalRequest{})
	wantCode(t, err, codes.InvalidArgument)
	_, err = s.p.GetPurchase(ctx, &pb.GetPurchaseRequest{PurchaseId: "missing"})
	wantCode(t, err, codes.NotFound)
	_, err = s.a.GetApproval(ctx, &pb.GetApprovalRequest{PurchaseId: "missing"})
	wantCode(t, err, codes.NotFound)
	_, err = s.a.DecideApproval(ctx, &pb.DecideApprovalRequest{PurchaseId: "missing", Status: pb.Status_STATUS_APPROVED})
	wantCode(t, err, codes.NotFound)
	pl, err := s.p.ListPurchases(ctx, &pb.ListPurchasesRequest{})
	if err != nil || len(pl.GetPurchases()) != 1 {
		t.Fatalf("purchases=%v err=%v", pl, err)
	}
	al, err := s.a.ListApprovals(ctx, &pb.ListApprovalsRequest{})
	if err != nil || len(al.GetApprovals()) != 1 {
		t.Fatalf("approvals=%v err=%v", al, err)
	}
}

func TestGRPCCreateLostReplyAndRetry(t *testing.T) {
	s := startPair(t)
	ctx := callContext(t)
	s.ah.loseReply.Store(true)
	_, err := s.p.CreatePurchase(ctx, input())
	wantCode(t, err, codes.Unavailable)
	if !strings.Contains(err.Error(), "same client_request_id") {
		t.Fatalf("missing retry advice: %v", err)
	}
	items, err := s.p.ListPurchases(ctx, &pb.ListPurchasesRequest{})
	if err != nil || len(items.GetPurchases()) != 1 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	retry, err := s.p.CreatePurchase(ctx, input())
	if err != nil || retry.GetId() != items.Purchases[0].Id {
		t.Fatalf("retry=%v err=%v", retry, err)
	}
	approvals, err := s.a.ListApprovals(ctx, &pb.ListApprovalsRequest{})
	if err != nil || len(approvals.GetApprovals()) != 1 {
		t.Fatalf("approvals=%v err=%v", approvals, err)
	}
}

func TestGRPCDecisionFailureAndRepair(t *testing.T) {
	for _, mode := range []string{"unavailable", "lost reply", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			s := startPair(t)
			ctx := callContext(t)
			p, err := s.p.CreatePurchase(ctx, input())
			if err != nil {
				t.Fatal(err)
			}
			want := codes.Unavailable
			switch mode {
			case "unavailable":
				s.ph.unavailable.Store(true)
			case "lost reply":
				s.ph.loseReply.Store(true)
			case "deadline":
				s.ph.block.Store(true)
				want = codes.DeadlineExceeded
			}
			decision := &pb.DecideApprovalRequest{PurchaseId: p.Id, Status: pb.Status_STATUS_REJECTED}
			_, err = s.a.DecideApproval(ctx, decision)
			wantCode(t, err, want)
			if !strings.Contains(err.Error(), "retry DecideApproval") {
				t.Fatalf("missing retry advice: %v", err)
			}
			a, err := s.a.GetApproval(ctx, &pb.GetApprovalRequest{PurchaseId: p.Id})
			if err != nil || a.GetStatus() != pb.Status_STATUS_REJECTED {
				t.Fatalf("saved approval=%v err=%v", a, err)
			}
			before, err := s.p.GetPurchase(ctx, &pb.GetPurchaseRequest{PurchaseId: p.Id})
			if err != nil {
				t.Fatal(err)
			}
			if mode != "lost reply" && before.Status != pb.Status_STATUS_PENDING {
				t.Fatal("purchase changed during failed propagation")
			}
			s.ph.unavailable.Store(false)
			s.ph.block.Store(false)
			if _, err := s.a.DecideApproval(ctx, decision); err != nil {
				t.Fatal(err)
			}
			after, err := s.p.GetPurchase(ctx, &pb.GetPurchaseRequest{PurchaseId: p.Id})
			if err != nil || after.GetStatus() != pb.Status_STATUS_REJECTED {
				t.Fatalf("repaired=%v err=%v", after, err)
			}
		})
	}
}

func TestGRPCCreateDeadline(t *testing.T) {
	s := startPair(t)
	ctx := callContext(t)
	s.ah.block.Store(true)
	_, err := s.p.CreatePurchase(ctx, input())
	wantCode(t, err, codes.DeadlineExceeded)
	s.ah.block.Store(false)
	if _, err := s.p.CreatePurchase(ctx, input()); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = s.p.GetPurchase(canceled, &pb.GetPurchaseRequest{PurchaseId: "missing"})
	wantCode(t, err, codes.Canceled)
}

func TestGRPCConcurrentDuplicateCreates(t *testing.T) {
	s := startPair(t)
	ctx := callContext(t)
	var wg sync.WaitGroup
	ids := make(chan string, 16)
	for i := 0; i < cap(ids); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := s.p.CreatePurchase(ctx, input())
			if err != nil {
				t.Error(err)
				return
			}
			ids <- p.Id
		}()
	}
	wg.Wait()
	close(ids)
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("different purchase IDs for same key")
		}
	}
	pl, err := s.p.ListPurchases(ctx, &pb.ListPurchasesRequest{})
	if err != nil || len(pl.GetPurchases()) != 1 {
		t.Fatalf("purchases=%v err=%v", pl, err)
	}
	al, err := s.a.ListApprovals(ctx, &pb.ListApprovalsRequest{})
	if err != nil || len(al.GetApprovals()) != 1 {
		t.Fatalf("approvals=%v err=%v", al, err)
	}
}

package rpc_test

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	pb "procureflow-ai/gen/procureflow/v1"
	"procureflow-ai/internal/auth"
	"testing"
	"time"
)

type unavailableOwner struct{ pb.PurchaseServiceClient }

func (unavailableOwner) GetPurchase(context.Context, *pb.GetPurchaseRequest, ...grpc.CallOption) (*pb.Purchase, error) {
	return nil, status.Error(codes.Unavailable, "fictional ownership lookup failure")
}

func TestServiceAuthorization(t *testing.T) {
	pair := startPair(t)
	pc, err := grpc.NewClient(pair.purchaseTarget, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	ac, err := grpc.NewClient(pair.approvalTarget, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer ac.Close()
	p, a := pb.NewPurchaseServiceClient(pc), pb.NewApprovalServiceClient(ac)
	base, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	actor := func(id, role string) context.Context {
		ctx, err := auth.Outgoing(base, transportKey, "actor", id, role)
		if err != nil {
			t.Fatal(err)
		}
		return ctx
	}
	employee, other, manager := actor("emp-101", auth.Employee), actor("emp-102", auth.Employee), actor("manager-demo", auth.Manager)
	want := func(err error, code codes.Code) {
		t.Helper()
		if status.Code(err) != code {
			t.Fatalf("code=%v want=%v err=%v", status.Code(err), code, err)
		}
	}
	_, err = p.ListPurchases(base, &pb.ListPurchasesRequest{})
	want(err, codes.Unauthenticated)
	input := &pb.CreatePurchaseRequest{ClientRequestId: "auth-key", EmployeeId: "emp-101", ItemName: "Fictional lamp", Quantity: 1, UnitPriceCents: 2500}
	created, err := p.CreatePurchase(employee, input)
	want(err, codes.OK)
	_, err = p.CreatePurchase(other, input)
	want(err, codes.PermissionDenied)
	input.EmployeeId = "emp-102"
	_, err = p.CreatePurchase(other, input)
	want(err, codes.AlreadyExists)
	_, err = p.GetPurchase(other, &pb.GetPurchaseRequest{PurchaseId: created.Id})
	want(err, codes.NotFound)
	list, err := p.ListPurchases(other, &pb.ListPurchasesRequest{})
	want(err, codes.OK)
	if len(list.Purchases) != 0 {
		t.Fatal("another employee's request leaked")
	}
	list, err = p.ListPurchases(employee, &pb.ListPurchasesRequest{})
	want(err, codes.OK)
	if len(list.Purchases) != 1 {
		t.Fatal("own request missing")
	}
	decision := &pb.DecideApprovalRequest{PurchaseId: created.Id, Status: pb.Status_STATUS_APPROVED}
	_, err = a.DecideApproval(employee, decision)
	want(err, codes.PermissionDenied)
	_, err = p.SetApprovalStatus(manager, &pb.SetApprovalStatusRequest{PurchaseId: created.Id, Status: pb.Status_STATUS_APPROVED})
	want(err, codes.PermissionDenied)
	owners := pair.ah.Owners
	pair.ah.Owners = unavailableOwner{}
	_, err = a.DecideApproval(manager, decision)
	want(err, codes.Unavailable)
	pending, err := a.GetApproval(manager, &pb.GetApprovalRequest{PurchaseId: created.Id})
	want(err, codes.OK)
	if pending.Status != pb.Status_STATUS_PENDING {
		t.Fatal("ownership failure changed approval")
	}
	pair.ah.Owners = owners
	for i := 0; i < 2; i++ {
		_, err = a.DecideApproval(manager, decision)
		want(err, codes.OK)
	}
	saved, err := p.GetPurchase(employee, &pb.GetPurchaseRequest{PurchaseId: created.Id})
	want(err, codes.OK)
	if saved.Status != pb.Status_STATUS_APPROVED {
		t.Fatal("signed service propagation failed")
	}
	own, err := p.CreatePurchase(manager, &pb.CreatePurchaseRequest{ClientRequestId: "manager-own", EmployeeId: "manager-demo", ItemName: "Fictional desk", Quantity: 1})
	want(err, codes.OK)
	for _, s := range []pb.Status{pb.Status_STATUS_APPROVED, pb.Status_STATUS_REJECTED} {
		_, err = a.DecideApproval(manager, &pb.DecideApprovalRequest{PurchaseId: own.Id, Status: s})
		want(err, codes.PermissionDenied)
	}
	own, err = p.GetPurchase(manager, &pb.GetPurchaseRequest{PurchaseId: own.Id})
	want(err, codes.OK)
	if own.Status != pb.Status_STATUS_PENDING {
		t.Fatal("self-review mutated purchase")
	}
}

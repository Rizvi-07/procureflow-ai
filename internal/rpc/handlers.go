package rpc

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	pb "procureflow-ai/gen/procureflow/v1"
	"procureflow-ai/internal/approval"
	"procureflow-ai/internal/auth"
	"procureflow-ai/internal/domain"
	"procureflow-ai/internal/purchase"
	"time"
)

func toStatus(s domain.Status) pb.Status {
	switch s {
	case domain.Pending:
		return pb.Status_STATUS_PENDING
	case domain.Approved:
		return pb.Status_STATUS_APPROVED
	case domain.Rejected:
		return pb.Status_STATUS_REJECTED
	default:
		return pb.Status_STATUS_UNSPECIFIED
	}
}

func fromStatus(s pb.Status) domain.Status {
	switch s {
	case pb.Status_STATUS_PENDING:
		return domain.Pending
	case pb.Status_STATUS_APPROVED:
		return domain.Approved
	case pb.Status_STATUS_REJECTED:
		return domain.Rejected
	default:
		return ""
	}
}

func purchaseMessage(p purchase.Purchase) *pb.Purchase {
	return &pb.Purchase{Id: p.ID, ClientRequestId: p.ClientRequestID, EmployeeId: p.EmployeeID,
		ItemName: p.ItemName, Quantity: p.Quantity, UnitPriceCents: p.UnitPriceCents,
		TotalCents: p.TotalCents, Status: toStatus(p.Status)}
}

func approvalMessage(a approval.Approval) *pb.Approval {
	return &pb.Approval{PurchaseId: a.PurchaseID, Status: toStatus(a.Status)}
}

type PurchaseHandler struct {
	pb.UnimplementedPurchaseServiceServer
	Service *purchase.Service
}

func (h *PurchaseHandler) CreatePurchase(ctx context.Context, req *pb.CreatePurchaseRequest) (*pb.Purchase, error) {
	c, err := requireIdentity(ctx, "/procureflow.v1.PurchaseService/CreatePurchase")
	if err != nil {
		return nil, err
	}
	if _, err := purchase.Validate(purchase.Input{ClientRequestID: req.GetClientRequestId(), EmployeeID: req.GetEmployeeId(), ItemName: req.GetItemName(), Quantity: req.GetQuantity(), UnitPriceCents: req.GetUnitPriceCents()}); err != nil {
		return nil, rpcError(err)
	}
	if req.GetEmployeeId() != c.Subject {
		return nil, status.Error(codes.PermissionDenied, "employee_id must match authenticated identity")
	}
	p, err := h.Service.Create(ctx, purchase.Input{ClientRequestID: req.GetClientRequestId(), EmployeeID: req.GetEmployeeId(),
		ItemName: req.GetItemName(), Quantity: req.GetQuantity(), UnitPriceCents: req.GetUnitPriceCents()})
	if err != nil {
		return nil, rpcError(err)
	}
	return purchaseMessage(p), nil
}

func (h *PurchaseHandler) GetPurchase(ctx context.Context, req *pb.GetPurchaseRequest) (*pb.Purchase, error) {
	c, err := requireIdentity(ctx, "/procureflow.v1.PurchaseService/GetPurchase")
	if err != nil {
		return nil, err
	}
	p, err := h.Service.Get(ctx, req.GetPurchaseId())
	if err != nil {
		return nil, rpcError(err)
	}
	if c.Kind == "actor" && !auth.CanReview(c.Role) && p.EmployeeID != c.Subject {
		return nil, status.Error(codes.NotFound, "purchase not found")
	}
	return purchaseMessage(p), nil
}

func (h *PurchaseHandler) ListPurchases(ctx context.Context, _ *pb.ListPurchasesRequest) (*pb.ListPurchasesResponse, error) {
	c, err := requireIdentity(ctx, "/procureflow.v1.PurchaseService/ListPurchases")
	if err != nil {
		return nil, err
	}
	items, err := h.Service.List(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	response := &pb.ListPurchasesResponse{Purchases: make([]*pb.Purchase, 0, len(items))}
	for _, p := range items {
		if !auth.CanReview(c.Role) && p.EmployeeID != c.Subject {
			continue
		}
		response.Purchases = append(response.Purchases, purchaseMessage(p))
	}
	return response, nil
}

func (h *PurchaseHandler) SetApprovalStatus(ctx context.Context, req *pb.SetApprovalStatusRequest) (*pb.Purchase, error) {
	if _, err := requireIdentity(ctx, "/procureflow.v1.PurchaseService/SetApprovalStatus"); err != nil {
		return nil, err
	}
	p, err := h.Service.SetApprovalStatus(ctx, req.GetPurchaseId(), fromStatus(req.GetStatus()))
	if err != nil {
		return nil, rpcError(err)
	}
	return purchaseMessage(p), nil
}

type ApprovalHandler struct {
	pb.UnimplementedApprovalServiceServer
	Service *approval.Service
	Owners  pb.PurchaseServiceClient
}

func (h *ApprovalHandler) CreateApproval(ctx context.Context, req *pb.CreateApprovalRequest) (*pb.Approval, error) {
	if _, err := requireIdentity(ctx, "/procureflow.v1.ApprovalService/CreateApproval"); err != nil {
		return nil, err
	}
	a, err := h.Service.Create(ctx, req.GetPurchaseId())
	if err != nil {
		return nil, rpcError(err)
	}
	return approvalMessage(a), nil
}

func (h *ApprovalHandler) GetApproval(ctx context.Context, req *pb.GetApprovalRequest) (*pb.Approval, error) {
	if _, err := requireIdentity(ctx, "/procureflow.v1.ApprovalService/GetApproval"); err != nil {
		return nil, err
	}
	a, err := h.Service.Get(ctx, req.GetPurchaseId())
	if err != nil {
		return nil, rpcError(err)
	}
	return approvalMessage(a), nil
}

func (h *ApprovalHandler) ListApprovals(ctx context.Context, _ *pb.ListApprovalsRequest) (*pb.ListApprovalsResponse, error) {
	if _, err := requireIdentity(ctx, "/procureflow.v1.ApprovalService/ListApprovals"); err != nil {
		return nil, err
	}
	items, err := h.Service.List(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	response := &pb.ListApprovalsResponse{Approvals: make([]*pb.Approval, 0, len(items))}
	for _, a := range items {
		response.Approvals = append(response.Approvals, approvalMessage(a))
	}
	return response, nil
}

func (h *ApprovalHandler) DecideApproval(ctx context.Context, req *pb.DecideApprovalRequest) (*pb.Approval, error) {
	c, err := requireIdentity(ctx, "/procureflow.v1.ApprovalService/DecideApproval")
	if err != nil {
		return nil, err
	}
	if err := domain.ValidateID("purchase_id", req.GetPurchaseId()); err != nil {
		return nil, rpcError(err)
	}
	if err := domain.ValidateDecision(fromStatus(req.GetStatus())); err != nil {
		return nil, rpcError(err)
	}
	if h.Owners == nil {
		return nil, status.Error(codes.Unavailable, "purchase ownership lookup unavailable")
	}
	ownerCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	p, err := h.Owners.GetPurchase(ownerCtx, &pb.GetPurchaseRequest{PurchaseId: req.GetPurchaseId()})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, status.Error(codes.NotFound, "purchase not found")
		}
		return nil, rpcError(dependencyError(err))
	}
	if p == nil {
		return nil, status.Error(codes.Unavailable, "purchase ownership unavailable")
	}
	if p.EmployeeId == c.Subject {
		return nil, status.Error(codes.PermissionDenied, "managers cannot decide their own purchases")
	}
	a, err := h.Service.Decide(ctx, req.GetPurchaseId(), fromStatus(req.GetStatus()))
	if err != nil {
		return nil, rpcError(err)
	}
	return approvalMessage(a), nil
}

func requireIdentity(ctx context.Context, method string) (auth.Claims, error) {
	c, ok := auth.Identity(ctx)
	if !ok {
		return c, status.Error(codes.Unauthenticated, "authenticated identity required")
	}
	if !auth.Allowed(c, method) {
		return c, status.Error(codes.PermissionDenied, "operation not allowed for this identity")
	}
	return c, nil
}

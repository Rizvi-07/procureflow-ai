package rpc

import (
	"context"

	pb "procureflow-ai/gen/procureflow/v1"
	"procureflow-ai/internal/approval"
	"procureflow-ai/internal/domain"
	"procureflow-ai/internal/purchase"
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
	p, err := h.Service.Create(ctx, purchase.Input{ClientRequestID: req.GetClientRequestId(), EmployeeID: req.GetEmployeeId(),
		ItemName: req.GetItemName(), Quantity: req.GetQuantity(), UnitPriceCents: req.GetUnitPriceCents()})
	if err != nil {
		return nil, rpcError(err)
	}
	return purchaseMessage(p), nil
}

func (h *PurchaseHandler) GetPurchase(ctx context.Context, req *pb.GetPurchaseRequest) (*pb.Purchase, error) {
	p, err := h.Service.Get(ctx, req.GetPurchaseId())
	if err != nil {
		return nil, rpcError(err)
	}
	return purchaseMessage(p), nil
}

func (h *PurchaseHandler) ListPurchases(ctx context.Context, _ *pb.ListPurchasesRequest) (*pb.ListPurchasesResponse, error) {
	items, err := h.Service.List(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	response := &pb.ListPurchasesResponse{Purchases: make([]*pb.Purchase, 0, len(items))}
	for _, p := range items {
		response.Purchases = append(response.Purchases, purchaseMessage(p))
	}
	return response, nil
}

func (h *PurchaseHandler) SetApprovalStatus(ctx context.Context, req *pb.SetApprovalStatusRequest) (*pb.Purchase, error) {
	p, err := h.Service.SetApprovalStatus(ctx, req.GetPurchaseId(), fromStatus(req.GetStatus()))
	if err != nil {
		return nil, rpcError(err)
	}
	return purchaseMessage(p), nil
}

type ApprovalHandler struct {
	pb.UnimplementedApprovalServiceServer
	Service *approval.Service
}

func (h *ApprovalHandler) CreateApproval(ctx context.Context, req *pb.CreateApprovalRequest) (*pb.Approval, error) {
	a, err := h.Service.Create(ctx, req.GetPurchaseId())
	if err != nil {
		return nil, rpcError(err)
	}
	return approvalMessage(a), nil
}

func (h *ApprovalHandler) GetApproval(ctx context.Context, req *pb.GetApprovalRequest) (*pb.Approval, error) {
	a, err := h.Service.Get(ctx, req.GetPurchaseId())
	if err != nil {
		return nil, rpcError(err)
	}
	return approvalMessage(a), nil
}

func (h *ApprovalHandler) ListApprovals(ctx context.Context, _ *pb.ListApprovalsRequest) (*pb.ListApprovalsResponse, error) {
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
	a, err := h.Service.Decide(ctx, req.GetPurchaseId(), fromStatus(req.GetStatus()))
	if err != nil {
		return nil, rpcError(err)
	}
	return approvalMessage(a), nil
}

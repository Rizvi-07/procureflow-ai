package rpc

import (
	"context"

	pb "procureflow-ai/gen/procureflow/v1"
	"procureflow-ai/internal/domain"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Dial creates a reusable lazy connection. Every caller supplies a deadline.
// Plaintext transport is intentionally limited to this local development phase.
func Dial(target string) (*grpc.ClientConn, error) {
	return grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

type ApprovalClient struct{ Client pb.ApprovalServiceClient }

func (c ApprovalClient) CreateApproval(ctx context.Context, id string) error {
	_, err := c.Client.CreateApproval(ctx, &pb.CreateApprovalRequest{PurchaseId: id})
	return dependencyError(err)
}

type PurchaseClient struct{ Client pb.PurchaseServiceClient }

func (c PurchaseClient) SetApprovalStatus(ctx context.Context, id string, decision domain.Status) error {
	_, err := c.Client.SetApprovalStatus(ctx, &pb.SetApprovalStatusRequest{PurchaseId: id, Status: toStatus(decision)})
	return dependencyError(err)
}

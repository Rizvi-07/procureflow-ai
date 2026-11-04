package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	pb "procureflow-ai/gen/procureflow/v1"
	"procureflow-ai/internal/config"
	"procureflow-ai/internal/rpc"

	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	requestID := flag.String("request-id", fmt.Sprintf("demo-%d", time.Now().UnixNano()), "reuse this key to demonstrate idempotency")
	flag.Parse()
	if err := run(*requestID); err != nil {
		slog.Error("smoke failed", "error", err)
		os.Exit(1)
	}
}

func run(requestID string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pconn, err := rpc.Dial(cfg.PurchaseTarget)
	if err != nil {
		return err
	}
	defer pconn.Close()
	aconn, err := rpc.Dial(cfg.ApprovalTarget)
	if err != nil {
		return err
	}
	defer aconn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, conn := range []*grpc.ClientConn{pconn, aconn} {
		h, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{}, grpc.WaitForReady(true))
		if err != nil {
			return fmt.Errorf("health check: %w", err)
		}
		if h.Status != healthpb.HealthCheckResponse_SERVING {
			return fmt.Errorf("service is not serving")
		}
	}
	purchases := pb.NewPurchaseServiceClient(pconn)
	approvals := pb.NewApprovalServiceClient(aconn)
	input := &pb.CreatePurchaseRequest{ClientRequestId: requestID, EmployeeId: "employee-demo", ItemName: "Fictional desk lamp", Quantity: 2, UnitPriceCents: 2500}
	p, err := purchases.CreatePurchase(ctx, input)
	if err != nil {
		return err
	}
	fmt.Printf("created purchase=%s total_cents=%d status=%s\n", p.Id, p.TotalCents, p.Status)
	repeat, err := purchases.CreatePurchase(ctx, input)
	if err != nil {
		return err
	}
	if repeat.Id != p.Id {
		return fmt.Errorf("create idempotency failed")
	}
	for i := 0; i < 2; i++ {
		if _, err := approvals.DecideApproval(ctx, &pb.DecideApprovalRequest{PurchaseId: p.Id, Status: pb.Status_STATUS_APPROVED}); err != nil {
			return err
		}
	}
	p, err = purchases.GetPurchase(ctx, &pb.GetPurchaseRequest{PurchaseId: p.Id})
	if err != nil {
		return err
	}
	if p.Status != pb.Status_STATUS_APPROVED || p.TotalCents != 5000 {
		return fmt.Errorf("unexpected purchase after approval: %v", p)
	}
	fmt.Printf("read purchase=%s status=%s; create and decision retries succeeded\n", p.Id, p.Status)
	return nil
}

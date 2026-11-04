package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	pb "procureflow-ai/gen/procureflow/v1"
	"procureflow-ai/internal/approval"
	"procureflow-ai/internal/config"
	"procureflow-ai/internal/purchase"
	"procureflow-ai/internal/rpc"
	"procureflow-ai/internal/server"

	"google.golang.org/grpc"
)

func Main(name string) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", name)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, name, logger); err != nil {
		logger.Error("service failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, name string, logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	addr, target := cfg.PurchaseAddr, cfg.ApprovalTarget
	if name == "approval-service" {
		addr, target = cfg.ApprovalAddr, cfg.PurchaseTarget
	}
	conn, err := rpc.Dial(target)
	if err != nil {
		return fmt.Errorf("create peer client: %w", err)
	}
	defer conn.Close()
	var s *grpc.Server
	switch name {
	case "purchase-service":
		svc := purchase.NewService(purchase.NewRepository(), rpc.ApprovalClient{Client: pb.NewApprovalServiceClient(conn)}, cfg.RPCTimeout)
		s = server.New(logger, pb.PurchaseService_ServiceDesc.ServiceName, func(s *grpc.Server) {
			pb.RegisterPurchaseServiceServer(s, &rpc.PurchaseHandler{Service: svc})
		})
	case "approval-service":
		svc := approval.NewService(approval.NewRepository(), rpc.PurchaseClient{Client: pb.NewPurchaseServiceClient(conn)}, cfg.RPCTimeout)
		s = server.New(logger, pb.ApprovalService_ServiceDesc.ServiceName, func(s *grpc.Server) {
			pb.RegisterApprovalServiceServer(s, &rpc.ApprovalHandler{Service: svc})
		})
	default:
		return fmt.Errorf("unknown service %q", name)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()
	logger.Info("service listening", "address", listener.Addr().String(), "peer", target, "rpc_timeout", cfg.RPCTimeout)
	err = server.Serve(ctx, s, listener, cfg.ShutdownTimeout)
	logger.Info("service stopped")
	return err
}

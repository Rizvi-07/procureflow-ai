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
	"procureflow-ai/internal/auth"
	"procureflow-ai/internal/config"
	"procureflow-ai/internal/postgres"
	"procureflow-ai/internal/purchase"
	"procureflow-ai/internal/rpc"
	"procureflow-ai/internal/server"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
	key, err := auth.LoadServiceKey()
	if err != nil {
		return err
	}
	var purchaseRepo purchase.Repository = purchase.NewRepository()
	var approvalRepo approval.Repository = approval.NewRepository()
	storage := "memory"
	if cfg.DatabaseURL != "" {
		pool, err := postgres.Open(ctx, cfg.DatabaseURL, cfg.DatabaseTimeout)
		if err != nil {
			return err
		}
		defer pool.Close()
		purchaseRepo = purchase.NewPostgresRepository(pool, cfg.DatabaseTimeout)
		approvalRepo = approval.NewPostgresRepository(pool, cfg.DatabaseTimeout)
		storage = "postgresql"
	}
	addr, target := cfg.PurchaseAddr, cfg.ApprovalTarget
	if name == "approval-service" {
		addr, target = cfg.ApprovalAddr, cfg.PurchaseTarget
	}
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(auth.ServiceClient(key, name)))
	if err != nil {
		return fmt.Errorf("create peer client: %w", err)
	}
	defer conn.Close()
	var s *grpc.Server
	switch name {
	case "purchase-service":
		svc := purchase.NewService(purchaseRepo, rpc.ApprovalClient{Client: pb.NewApprovalServiceClient(conn)}, cfg.RPCTimeout)
		s = server.New(logger, pb.PurchaseService_ServiceDesc.ServiceName, func(s *grpc.Server) {
			pb.RegisterPurchaseServiceServer(s, &rpc.PurchaseHandler{Service: svc})
		}, grpc.ChainUnaryInterceptor(auth.ServerInterceptor(key)))
	case "approval-service":
		svc := approval.NewService(approvalRepo, rpc.PurchaseClient{Client: pb.NewPurchaseServiceClient(conn)}, cfg.RPCTimeout)
		s = server.New(logger, pb.ApprovalService_ServiceDesc.ServiceName, func(s *grpc.Server) {
			pb.RegisterApprovalServiceServer(s, &rpc.ApprovalHandler{Service: svc, Owners: pb.NewPurchaseServiceClient(conn)})
		}, grpc.ChainUnaryInterceptor(auth.ServerInterceptor(key)))
	default:
		return fmt.Errorf("unknown service %q", name)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()
	logger.Info("service listening", "address", listener.Addr().String(), "peer", target, "rpc_timeout", cfg.RPCTimeout, "storage", storage)
	err = server.Serve(ctx, s, listener, cfg.ShutdownTimeout)
	logger.Info("service stopped")
	return err
}

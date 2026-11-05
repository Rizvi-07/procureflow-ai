package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	pb "procureflow-ai/gen/procureflow/v1"
	"procureflow-ai/internal/config"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func NewServer(cfg config.GatewayConfig, handler http.Handler) *http.Server {
	return &http.Server{Addr: cfg.Addr, Handler: handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout, ReadTimeout: cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout, IdleTimeout: cfg.IdleTimeout, MaxHeaderBytes: 16 << 10}
}

// Serve waits for cancellation and drains active HTTP requests before returning.
func Serve(ctx context.Context, s *http.Server, listener net.Listener, shutdownTimeout time.Duration) error {
	result := make(chan error, 1)
	go func() { result <- s.Serve(listener) }()
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := s.Shutdown(shutdownCtx); err != nil {
			_ = s.Close()
		}
		err := <-result
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func Run(ctx context.Context, logger *slog.Logger) error {
	cfg, err := config.LoadGateway()
	if err != nil {
		return err
	}
	// Own reusable gRPC connections. The gateway does not load database config.
	purchases, err := grpc.NewClient(cfg.PurchaseTarget, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("create purchase client: %w", err)
	}
	defer purchases.Close()
	approvals, err := grpc.NewClient(cfg.ApprovalTarget, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("create approval client: %w", err)
	}
	defer approvals.Close()
	handler := NewHandler(pb.NewPurchaseServiceClient(purchases), pb.NewApprovalServiceClient(approvals), cfg.RPCTimeout, cfg.MaxBodyBytes, cfg.VueOrigin, logger)
	s := NewServer(cfg, handler)
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}
	defer listener.Close()
	logger.Info("gateway listening", "address", listener.Addr().String(), "vue_origin", cfg.VueOrigin, "demo_only", true)
	err = Serve(ctx, s, listener, cfg.ShutdownTimeout)
	logger.Info("gateway stopped")
	return err
}

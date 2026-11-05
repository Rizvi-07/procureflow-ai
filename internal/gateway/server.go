package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	ai "procureflow-ai/gen/assistant/v1"
	pb "procureflow-ai/gen/procureflow/v1"
	"procureflow-ai/internal/auth"
	"procureflow-ai/internal/config"
	"procureflow-ai/internal/postgres"

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
	key, err := auth.LoadServiceKey()
	if err != nil {
		return err
	}
	authURL := os.Getenv("AUTH_DATABASE_URL")
	if authURL == "" {
		return fmt.Errorf("AUTH_DATABASE_URL is required; migrate and seed identity storage first")
	}
	pool, err := postgres.Open(ctx, authURL, 5*time.Second)
	if err != nil {
		return fmt.Errorf("identity storage: %w", err)
	}
	defer pool.Close()
	users := auth.NewPostgresUsers(pool, 2*time.Second)
	existing, err := users.List(ctx)
	if err != nil {
		return fmt.Errorf("identity schema unavailable; run migrations and auth-seed")
	}
	hasAdmin := false
	for _, u := range existing {
		if u.Role == auth.Admin {
			hasAdmin = true
		}
	}
	if !hasAdmin {
		return fmt.Errorf("identity storage has no admin; run auth-seed before startup")
	}
	secure := os.Getenv("AUTH_COOKIE_SECURE") == "true"
	if value := os.Getenv("AUTH_COOKIE_SECURE"); value != "" && value != "true" && value != "false" {
		return fmt.Errorf("AUTH_COOKIE_SECURE must be true or false")
	}
	// Procurement uses reusable gRPC connections; SQL access is limited to auth.*.
	purchases, err := grpc.NewClient(cfg.PurchaseTarget, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(auth.ActorClient(key)))
	if err != nil {
		return fmt.Errorf("create purchase client: %w", err)
	}
	defer purchases.Close()
	approvals, err := grpc.NewClient(cfg.ApprovalTarget, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(auth.ActorClient(key)))
	if err != nil {
		return fmt.Errorf("create approval client: %w", err)
	}
	defer approvals.Close()
	handler := NewHandler(pb.NewPurchaseServiceClient(purchases), pb.NewApprovalServiceClient(approvals), cfg.RPCTimeout, cfg.MaxBodyBytes, cfg.VueOrigin, logger)
	handler.accounts = auth.NewPersistentSessions(users, secure)
	if cfg.AssistantTarget != "" {
		// NewClient is lazy: assistant availability never gates procurement startup.
		connection, err := grpc.NewClient(cfg.AssistantTarget, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(auth.ActorClient(key)))
		if err != nil {
			return fmt.Errorf("create optional assistant client")
		}
		defer connection.Close()
		handler.assistant = ai.NewAssistantServiceClient(connection)
		handler.assistantTimeout = cfg.AssistantTimeout
	}
	s := NewServer(cfg, handler)
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}
	defer listener.Close()
	logger.Info("gateway listening", "address", listener.Addr().String(), "vue_origin", cfg.VueOrigin, "authentication", "local_sessions")
	err = Serve(ctx, s, listener, cfg.ShutdownTimeout)
	logger.Info("gateway stopped")
	return err
}

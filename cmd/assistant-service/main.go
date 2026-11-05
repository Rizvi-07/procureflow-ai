package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	pb "procureflow-ai/gen/assistant/v1"
	"procureflow-ai/internal/assistant"
	"procureflow-ai/internal/auth"
	"procureflow-ai/internal/server"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "assistant-service")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("assistant stopped", "error", err)
		os.Exit(1)
	}
}
func run(ctx context.Context, logger *slog.Logger) error {
	key, err := auth.LoadServiceKey()
	if err != nil {
		return err
	}
	cfg, err := assistant.LoadConfig()
	if err != nil {
		return err
	}
	var provider assistant.Provider
	var store assistant.Retriever
	if cfg.Configured() {
		startup, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		pool, err := assistant.OpenIndex(startup, cfg.DatabaseURL, true)
		if err != nil {
			return err
		}
		defer pool.Close()
		index := &assistant.PostgresStore{Pool: pool, Model: cfg.EmbeddingModel, Dimensions: cfg.Dimensions}
		if err = index.Verify(startup); err != nil {
			return err
		}
		store = index
		api := assistant.NewOpenAI(cfg)
		defer api.Close()
		provider = api
	}
	svc := assistant.NewService(cfg, provider, store)
	s := server.New(logger, pb.AssistantService_ServiceDesc.ServiceName, func(s *grpc.Server) { pb.RegisterAssistantServiceServer(s, svc) }, grpc.ChainUnaryInterceptor(auth.ServerInterceptor(key)))
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	logger.Info("assistant listening", "address", cfg.Addr, "configured", cfg.Configured(), "read_only", true)
	return server.Serve(ctx, s, listener, 5*time.Second)
}

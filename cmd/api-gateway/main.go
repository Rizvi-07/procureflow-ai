package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"procureflow-ai/internal/gateway"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "api-gateway")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := gateway.Run(ctx, logger); err != nil {
		logger.Error("gateway failed", "error", err)
		os.Exit(1)
	}
}

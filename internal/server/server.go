package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

func New(logger *slog.Logger, serviceName string, register func(*grpc.Server)) *grpc.Server {
	s := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		response, err := handler(ctx, req)
		level := slog.LevelInfo
		if err != nil {
			level = slog.LevelWarn
		}
		logger.Log(ctx, level, "rpc completed", "method", info.FullMethod, "code", status.Code(err).String(), "duration", time.Since(start))
		return response, err
	}))
	register(s)
	h := health.NewServer()
	healthpb.RegisterHealthServer(s, h)
	h.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	h.SetServingStatus(serviceName, healthpb.HealthCheckResponse_SERVING)
	return s
}

// Serve drains in-flight requests on cancellation, with a bounded forced stop.
func Serve(ctx context.Context, s *grpc.Server, listener net.Listener, timeout time.Duration) error {
	result := make(chan error, 1)
	go func() { result <- s.Serve(listener) }()
	select {
	case err := <-result:
		if errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}
		return err
	case <-ctx.Done():
		done := make(chan struct{})
		go func() { s.GracefulStop(); close(done) }()
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			s.Stop()
			<-done
		}
		<-result
		return nil
	}
}

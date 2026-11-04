package server

import (
	"context"
	"net"
	"testing"
	"time"

	"procureflow-ai/internal/rpc"

	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

type blockingHealth struct {
	healthpb.UnimplementedHealthServer
	entered chan struct{}
	release chan struct{}
}

func (h *blockingHealth) Check(ctx context.Context, _ *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	close(h.entered)
	select {
	case <-h.release:
		return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}
}

func TestShutdownWithActiveRequest(t *testing.T) {
	for _, force := range []bool{false, true} {
		name := "graceful"
		if force {
			name = "forced"
		}
		t.Run(name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			s := grpc.NewServer()
			defer s.Stop()
			h := &blockingHealth{entered: make(chan struct{}), release: make(chan struct{})}
			healthpb.RegisterHealthServer(s, h)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stopped := make(chan error, 1)
			go func() { stopped <- Serve(ctx, s, listener, 50*time.Millisecond) }()
			conn, err := rpc.Dial(listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			callCtx, cancelCall := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancelCall()
			response := make(chan error, 1)
			go func() {
				_, err := healthpb.NewHealthClient(conn).Check(callCtx, &healthpb.HealthCheckRequest{}, grpc.WaitForReady(true))
				response <- err
			}()
			select {
			case <-h.entered:
			case <-callCtx.Done():
				t.Fatal("request never entered handler")
			}
			cancel()
			if !force {
				close(h.release)
			}
			select {
			case err := <-stopped:
				if err != nil {
					t.Fatal(err)
				}
			case <-callCtx.Done():
				t.Fatal("shutdown exceeded bound")
			}
			err = <-response
			if !force && err != nil {
				t.Fatalf("graceful stop interrupted request: %v", err)
			}
			if force && err == nil {
				t.Fatal("forced stop should interrupt blocked request")
			}
		})
	}
}

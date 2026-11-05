package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func TestHTTPProbe(t *testing.T) {
	for _, code := range []int{http.StatusOK, http.StatusServiceUnavailable, http.StatusFound} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }))
			defer s.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := check(ctx, "", s.URL, ""); (err == nil) != (code == http.StatusOK) {
				t.Fatalf("status=%d err=%v", code, err)
			}
		})
	}
	if err := check(context.Background(), "", "", ""); err == nil {
		t.Fatal("missing probe target accepted")
	}
	if err := check(context.Background(), "localhost:50051", "http://localhost", ""); err == nil {
		t.Fatal("two probe targets accepted")
	}
}

func TestGRPCProbe(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	s := grpc.NewServer()
	defer s.Stop()
	h := health.NewServer()
	healthpb.RegisterHealthServer(s, h)
	go func() { _ = s.Serve(listener) }()
	for _, state := range []healthpb.HealthCheckResponse_ServingStatus{healthpb.HealthCheckResponse_SERVING, healthpb.HealthCheckResponse_NOT_SERVING} {
		h.SetServingStatus("demo", state)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := check(ctx, listener.Addr().String(), "", "demo")
		cancel()
		if (err == nil) != (state == healthpb.HealthCheckResponse_SERVING) {
			t.Fatalf("state=%s err=%v", state, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := check(ctx, listener.Addr().String(), "", "demo"); err == nil {
		t.Fatal("probe ignored cancellation")
	}
}

package gateway

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"procureflow-ai/internal/config"
)

func TestServerTimeouts(t *testing.T) {
	cfg := config.GatewayConfig{ReadHeaderTimeout: time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 3 * time.Second, IdleTimeout: 4 * time.Second}
	s := NewServer(cfg, http.NotFoundHandler())
	if s.ReadHeaderTimeout != cfg.ReadHeaderTimeout || s.ReadTimeout != cfg.ReadTimeout || s.WriteTimeout != cfg.WriteTimeout || s.IdleTimeout != cfg.IdleTimeout || s.MaxHeaderBytes == 0 {
		t.Fatal("server timeouts/limits missing")
	}
}

func TestHTTPGracefulShutdown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	s := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		writeJSON(w, 200, map[string]string{"status": "finished"})
	})}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- Serve(ctx, s, listener, time.Second) }()
	client := &http.Client{Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	response := make(chan error, 1)
	go func() {
		r, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			_, err = io.Copy(io.Discard, r.Body)
			r.Body.Close()
		}
		response <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not enter handler")
	}
	cancel()
	close(release)
	select {
	case err := <-response:
		if err != nil {
			t.Fatalf("active request interrupted: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request did not finish")
	}
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("gateway shutdown stalled")
	}
}

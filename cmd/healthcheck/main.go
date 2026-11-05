// healthcheck is a small container probe for the existing HTTP and gRPC health APIs.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	grpcTarget := flag.String("grpc", "", "gRPC target to probe")
	httpTarget := flag.String("http", "", "HTTP health URL to probe")
	service := flag.String("service", "", "registered gRPC service name")
	timeout := flag.Duration("timeout", 3*time.Second, "probe deadline")
	flag.Parse()
	if *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "timeout must be positive")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if err := check(ctx, *grpcTarget, *httpTarget, *service); err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		os.Exit(1)
	}
}

func check(ctx context.Context, grpcTarget, httpTarget, service string) error {
	if (grpcTarget == "") == (httpTarget == "") {
		return fmt.Errorf("specify exactly one of -grpc or -http")
	}
	if httpTarget != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpTarget, nil)
		if err != nil {
			return err
		}
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		defer client.CloseIdleConnections()
		response, err := client.Do(req)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("HTTP status %d", response.StatusCode)
		}
		return nil
	}
	conn, err := grpc.NewClient(grpcTarget, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()
	response, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{Service: service}, grpc.WaitForReady(true))
	if err != nil {
		return err
	}
	if response.Status != healthpb.HealthCheckResponse_SERVING {
		return fmt.Errorf("gRPC service is %s", response.Status)
	}
	return nil
}

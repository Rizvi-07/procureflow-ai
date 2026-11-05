package rpc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"procureflow-ai/internal/domain"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDependencyFailureTranslation(t *testing.T) {
	for _, tc := range []struct {
		name string
		code codes.Code
		want error
	}{
		{"timeout", codes.DeadlineExceeded, context.DeadlineExceeded},
		{"cancellation", codes.Canceled, context.Canceled},
		{"unavailable", codes.Unavailable, domain.ErrUnavailable},
		{"internal peer failure", codes.Internal, domain.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := dependencyError(status.Error(tc.code, "private upstream detail"))
			if !errors.Is(err, tc.want) || strings.Contains(err.Error(), "private upstream detail") {
				t.Fatalf("translated dependency error=%v; want sanitized %v", err, tc.want)
			}
			// Business services wrap dependency errors with saved-state/retry advice.
			wrapped := fmt.Errorf("saved; retry identical request: %w", err)
			wantCode := tc.code
			if errors.Is(tc.want, domain.ErrUnavailable) {
				wantCode = codes.Unavailable
			}
			if got := status.Code(rpcError(wrapped)); got != wantCode {
				t.Fatalf("outbound gRPC code=%s; want %s", got, wantCode)
			}
		})
	}
	if dependencyError(nil) != nil || rpcError(nil) != nil {
		t.Fatal("successful dependency call became an error")
	}
}

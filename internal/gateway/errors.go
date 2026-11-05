package gateway

import (
	"context"
	"errors"
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func upstreamError(w http.ResponseWriter, err error) {
	code := status.Code(err)
	if errors.Is(err, context.DeadlineExceeded) {
		code = codes.DeadlineExceeded
	}
	if errors.Is(err, context.Canceled) {
		code = codes.Canceled
	}
	httpCode, message, retryable := http.StatusBadGateway, "backend service failed", false
	switch code {
	case codes.InvalidArgument, codes.OutOfRange:
		httpCode, message = http.StatusBadRequest, "backend rejected invalid purchase or decision input"
	case codes.NotFound:
		httpCode, message = http.StatusNotFound, "purchase or approval was not found"
	case codes.AlreadyExists:
		httpCode, message = http.StatusConflict, "client_request_id already exists with different purchase details"
	case codes.FailedPrecondition, codes.Aborted:
		httpCode, message = http.StatusConflict, "operation conflicts with the current purchase or approval state"
	case codes.Unauthenticated:
		httpCode, message = http.StatusUnauthorized, "backend requires authentication"
	case codes.PermissionDenied:
		httpCode, message = http.StatusForbidden, "backend denied this operation"
	case codes.ResourceExhausted:
		httpCode, message, retryable = http.StatusTooManyRequests, "backend capacity exceeded; retry the same operation", true
	case codes.Unavailable:
		httpCode, message, retryable = http.StatusServiceUnavailable, "backend unavailable; retry with the same create key and details or the same decision", true
	case codes.DeadlineExceeded:
		httpCode, message, retryable = http.StatusGatewayTimeout, "backend deadline exceeded; a write may have completed, so retry identically", true
	case codes.Canceled:
		httpCode, message, retryable = http.StatusRequestTimeout, "request canceled; a write may have completed, so retry identically", true
	}
	if retryable {
		w.Header().Set("Retry-After", "1")
	}
	name, ok := grpcErrorNames[code]
	if !ok {
		name = "BAD_GATEWAY"
	}
	writeError(w, httpCode, name, message, retryable)
}

var grpcErrorNames = map[codes.Code]string{
	codes.InvalidArgument: "INVALID_ARGUMENT", codes.OutOfRange: "OUT_OF_RANGE",
	codes.NotFound: "NOT_FOUND", codes.AlreadyExists: "ALREADY_EXISTS",
	codes.FailedPrecondition: "FAILED_PRECONDITION", codes.Aborted: "ABORTED",
	codes.Unauthenticated: "UNAUTHENTICATED", codes.PermissionDenied: "PERMISSION_DENIED",
	codes.ResourceExhausted: "RESOURCE_EXHAUSTED", codes.Unavailable: "UNAVAILABLE",
	codes.DeadlineExceeded: "DEADLINE_EXCEEDED", codes.Canceled: "CANCELED",
	codes.Internal: "INTERNAL", codes.DataLoss: "DATA_LOSS", codes.Unimplemented: "UNIMPLEMENTED",
}

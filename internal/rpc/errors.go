// Package rpc translates between generated messages and domain operations.
package rpc

import (
	"context"
	"errors"

	"procureflow-ai/internal/domain"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func rpcError(err error) error {
	if err == nil {
		return nil
	}
	code := codes.Internal
	switch {
	case errors.Is(err, context.Canceled):
		code = codes.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		code = codes.DeadlineExceeded
	case errors.Is(err, domain.ErrInvalid):
		code = codes.InvalidArgument
	case errors.Is(err, domain.ErrNotFound):
		code = codes.NotFound
	case errors.Is(err, domain.ErrExists):
		code = codes.AlreadyExists
	case errors.Is(err, domain.ErrConflict):
		code = codes.FailedPrecondition
	case errors.Is(err, domain.ErrUnavailable):
		code = codes.Unavailable
	}
	if code == codes.Internal {
		return status.Error(code, "internal service error")
	}
	return status.Error(code, err.Error())
}

// Never forward arbitrary dependency messages to callers. A saved local change
// can be retried even if the remote operation succeeded but its response was lost.
func dependencyError(err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.Canceled:
		return context.Canceled
	case codes.DeadlineExceeded:
		return context.DeadlineExceeded
	default:
		return domain.ErrUnavailable
	}
}

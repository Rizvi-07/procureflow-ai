// Package domain contains shared business vocabulary, not transport code.
package domain

import (
	"errors"
	"fmt"
	"regexp"
)

type Status string

const (
	Pending  Status = "PENDING"
	Approved Status = "APPROVED"
	Rejected Status = "REJECTED"
)

var (
	ErrInvalid     = errors.New("invalid argument")
	ErrNotFound    = errors.New("not found")
	ErrExists      = errors.New("already exists")
	ErrConflict    = errors.New("conflicting decision")
	ErrUnavailable = errors.New("dependency unavailable")
	idPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
)

func ValidateID(field, value string) error {
	if !idPattern.MatchString(value) {
		return fmt.Errorf("%w: %s must be 1-128 letters, digits, underscores or hyphens, starting alphanumeric", ErrInvalid, field)
	}
	return nil
}

func ValidateDecision(status Status) error {
	if status != Approved && status != Rejected {
		return fmt.Errorf("%w: decision must be APPROVED or REJECTED", ErrInvalid)
	}
	return nil
}

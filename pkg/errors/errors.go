// ================== pkg/errors/errors.go ==================
package errors

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNotFound     = errors.New("resource not found")
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrBadRequest   = errors.New("bad request")
	ErrInternal     = errors.New("internal server error")
	ErrDuplicate    = errors.New("resource already exists")
	ErrConflict     = errors.New("conflict")
	ErrValidation   = errors.New("validation failed")
)

// Helper functions to check error types
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrNotFound) ||
		strings.Contains(err.Error(), "not found") ||
		strings.Contains(err.Error(), "does not exist")
}

func IsUnauthorized(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrUnauthorized) ||
		strings.Contains(err.Error(), "unauthorized")
}

func IsForbidden(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrForbidden) ||
		strings.Contains(err.Error(), "forbidden") ||
		strings.Contains(err.Error(), "not the owner") ||
		strings.Contains(err.Error(), "permission denied")
}

func IsConflict(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrConflict) ||
		errors.Is(err, ErrDuplicate) ||
		strings.Contains(err.Error(), "already") ||
		strings.Contains(err.Error(), "duplicate") ||
		strings.Contains(err.Error(), "conflict")
}

func IsValidation(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrValidation) ||
		strings.Contains(err.Error(), "validation") ||
		strings.Contains(err.Error(), "invalid") ||
		strings.Contains(err.Error(), "required") ||
		strings.Contains(err.Error(), "no valid fields")
}

func IsBadRequest(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrBadRequest)
}

// Wrap functions to preserve error context
func WrapNotFound(message string) error {
	return fmt.Errorf("%s: %w", message, ErrNotFound)
}

func WrapUnauthorized(message string) error {
	return fmt.Errorf("%s: %w", message, ErrUnauthorized)
}

func WrapForbidden(message string) error {
	return fmt.Errorf("%s: %w", message, ErrForbidden)
}

func WrapConflict(message string) error {
	return fmt.Errorf("%s: %w", message, ErrConflict)
}

func WrapValidation(message string) error {
	return fmt.Errorf("%s: %w", message, ErrValidation)
}

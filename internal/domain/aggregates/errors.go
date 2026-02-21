package aggregates

import (
	"errors"
	"fmt"
	"strings"
)

// ErrorCode standardizes aggregate failure semantics across domains.
type ErrorCode string

const (
	CodeValidation         ErrorCode = "validation"
	CodeNotFound           ErrorCode = "not_found"
	CodeConflict           ErrorCode = "conflict"
	CodeInvariantViolation ErrorCode = "invariant_violation"
	CodePreconditionFailed ErrorCode = "precondition_failed"
	CodeRetryable          ErrorCode = "retryable"
	CodeInternal           ErrorCode = "internal"
)

// Error is the canonical aggregate error wrapper.
type Error struct {
	Code    ErrorCode
	Op      string
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	op := strings.TrimSpace(e.Op)
	msg := strings.TrimSpace(e.Message)
	switch {
	case op != "" && msg != "":
		return fmt.Sprintf("%s: %s (%s)", op, msg, e.Code)
	case op != "":
		return fmt.Sprintf("%s (%s)", op, e.Code)
	case msg != "":
		return fmt.Sprintf("%s (%s)", msg, e.Code)
	default:
		return string(e.Code)
	}
}

func (e *Error) Unwrap() error { return e.Cause }

// NewError builds an aggregate error with explicit code + operation.
func NewError(code ErrorCode, op, message string, cause error) error {
	return &Error{
		Code:    code,
		Op:      strings.TrimSpace(op),
		Message: strings.TrimSpace(message),
		Cause:   cause,
	}
}

// Wrap annotates an existing error with aggregate error semantics.
func Wrap(code ErrorCode, op string, err error) error {
	if err == nil {
		return nil
	}
	return NewError(code, op, err.Error(), err)
}

// IsCode checks whether err (or wrapped err) carries the given aggregate code.
func IsCode(err error, code ErrorCode) bool {
	var aggErr *Error
	if !errors.As(err, &aggErr) {
		return false
	}
	return aggErr.Code == code
}

// CodeOf extracts the aggregate error code when available.
func CodeOf(err error) ErrorCode {
	var aggErr *Error
	if !errors.As(err, &aggErr) {
		return ""
	}
	return aggErr.Code
}

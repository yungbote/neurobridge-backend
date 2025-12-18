package errors

import "errors"

var (
	// ErrNotFound is a generic sentinel for missing resources.
	ErrNotFound = errors.New("not found")
	// ErrUnauthorized is a generic sentinel for auth failures.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrInvalidArgument is a generic sentinel for invalid input.
	ErrInvalidArgument = errors.New("invalid argument")
)

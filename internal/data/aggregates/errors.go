package aggregates

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	domainagg "github.com/yungbote/neurobridge-backend/internal/domain/aggregates"
	"gorm.io/gorm"
)

var (
	// ErrValidation indicates caller input validation failure.
	ErrValidation = errors.New("aggregate validation")
	// ErrInvariant indicates invariant rule violation.
	ErrInvariant = errors.New("aggregate invariant violation")
	// ErrConflict indicates optimistic/concurrency conflict.
	ErrConflict = errors.New("aggregate conflict")
	// ErrRetryable indicates transient retryable failure.
	ErrRetryable = errors.New("aggregate retryable")
)

// ValidationError tags an error as validation failure.
func ValidationError(msg string) error {
	return errors.Join(ErrValidation, errors.New(strings.TrimSpace(msg)))
}

// InvariantError tags an error as invariant violation.
func InvariantError(msg string) error {
	return errors.Join(ErrInvariant, errors.New(strings.TrimSpace(msg)))
}

// ConflictError tags an error as conflict failure.
func ConflictError(msg string) error {
	return errors.Join(ErrConflict, errors.New(strings.TrimSpace(msg)))
}

// RetryableError tags an error as retryable failure.
func RetryableError(msg string) error {
	return errors.Join(ErrRetryable, errors.New(strings.TrimSpace(msg)))
}

// MapError maps infrastructure/domain failures into aggregate error codes.
func MapError(op string, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*domainagg.Error); ok {
		return err
	}
	switch {
	case errors.Is(err, ErrValidation):
		return domainagg.Wrap(domainagg.CodeValidation, op, err)
	case errors.Is(err, ErrInvariant):
		return domainagg.Wrap(domainagg.CodeInvariantViolation, op, err)
	case errors.Is(err, ErrConflict):
		return domainagg.Wrap(domainagg.CodeConflict, op, err)
	case errors.Is(err, ErrRetryable):
		return domainagg.Wrap(domainagg.CodeRetryable, op, err)
	case errors.Is(err, gorm.ErrRecordNotFound):
		return domainagg.Wrap(domainagg.CodeNotFound, op, err)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return domainagg.Wrap(domainagg.CodeRetryable, op, err)
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch strings.TrimSpace(pgErr.Code) {
		case "23505":
			return domainagg.Wrap(domainagg.CodeConflict, op, err) // unique_violation
		case "23503":
			return domainagg.Wrap(domainagg.CodePreconditionFailed, op, err) // foreign_key_violation
		case "40001", "40P01", "55P03":
			return domainagg.Wrap(domainagg.CodeRetryable, op, err) // serialization/deadlock/lock_not_available
		}
	}

	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "duplicate key"), strings.Contains(msg, "already exists"):
		return domainagg.Wrap(domainagg.CodeConflict, op, err)
	case strings.Contains(msg, "deadlock"),
		strings.Contains(msg, "serialization"),
		strings.Contains(msg, "timeout"),
		strings.Contains(msg, "temporar"):
		return domainagg.Wrap(domainagg.CodeRetryable, op, err)
	default:
		return domainagg.Wrap(domainagg.CodeInternal, op, err)
	}
}

func notImplemented(op string) error {
	return domainagg.NewError(domainagg.CodeInternal, op, "aggregate implementation not yet migrated", nil)
}

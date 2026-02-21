package qdrant

import "fmt"

type OperationErrorCode string

const (
	OperationErrorValidation        OperationErrorCode = "validation_failed"
	OperationErrorUnsupportedFilter OperationErrorCode = "unsupported_filter"
	OperationErrorEncodeFailed      OperationErrorCode = "encode_failed"
	OperationErrorDecodeFailed      OperationErrorCode = "decode_failed"
	OperationErrorTransportFailed   OperationErrorCode = "transport_failed"
	OperationErrorTimeout           OperationErrorCode = "timeout"
	OperationErrorQueryFailed       OperationErrorCode = "query_failed"
)

type OperationError struct {
	Code       OperationErrorCode
	Operation  string
	StatusCode int
	Message    string
	Cause      error
}

func (e *OperationError) Error() string {
	if e == nil {
		return "qdrant operation failed"
	}
	if e.Message != "" {
		return fmt.Sprintf(
			"qdrant operation failed (op=%s code=%s status=%d): %s",
			e.Operation,
			e.Code,
			e.StatusCode,
			e.Message,
		)
	}
	if e.Cause != nil {
		return fmt.Sprintf(
			"qdrant operation failed (op=%s code=%s status=%d): %v",
			e.Operation,
			e.Code,
			e.StatusCode,
			e.Cause,
		)
	}
	return fmt.Sprintf(
		"qdrant operation failed (op=%s code=%s status=%d)",
		e.Operation,
		e.Code,
		e.StatusCode,
	)
}

func (e *OperationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func opErr(op string, code OperationErrorCode, msg string, cause error) error {
	return &OperationError{
		Code:      code,
		Operation: op,
		Message:   msg,
		Cause:     cause,
	}
}

package aggregates

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

var SagaAggregateContract = Contract{
	Name:             "Jobs.SagaAggregate",
	WriteTxOwnership: WriteTxOwnedByAggregate,
	ReadPolicy:       ReadPolicyInvariantScoped,
	Notes:            "Owns saga run + action atomic state progression for durable compensation boundaries.",
}

// SagaAggregate owns saga transition invariants.
//
// Write method failures should return *aggregates.Error with codes:
// CodeValidation, CodeNotFound, CodeConflict, CodeInvariantViolation, CodeRetryable, CodeInternal.
type SagaAggregate interface {
	Aggregate

	// AppendAction atomically appends the next saga action in sequence.
	AppendAction(ctx context.Context, in AppendSagaActionInput) (AppendSagaActionResult, error)

	// TransitionStatus atomically transitions saga run status.
	TransitionStatus(ctx context.Context, in TransitionSagaStatusInput) (TransitionSagaStatusResult, error)
}

type AppendSagaActionInput struct {
	SagaID         uuid.UUID
	ActionID       uuid.UUID
	Kind           string
	Payload        json.RawMessage
	IdempotencyKey string
	AppendedAt     time.Time
}

type AppendSagaActionResult struct {
	SagaID     uuid.UUID
	ActionID   uuid.UUID
	Seq        int64
	Status     string
	AppendedAt time.Time
}

type TransitionSagaStatusInput struct {
	SagaID       uuid.UUID
	FromStatus   string
	ToStatus     string
	Reason       string
	TransitionAt time.Time
	Metadata     map[string]any
}

type TransitionSagaStatusResult struct {
	SagaID       uuid.UUID
	Status       string
	TransitionAt time.Time
}

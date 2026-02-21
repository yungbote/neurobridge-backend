package aggregates

import (
	"context"
	"time"

	"github.com/google/uuid"
)

var RuntimeAggregateContract = Contract{
	Name:             "Paths.RuntimeAggregate",
	WriteTxOwnership: WriteTxOwnedByAggregate,
	ReadPolicy:       ReadPolicyInvariantScoped,
	Notes: "Owns atomic state transitions across path_run/node_run/activity_run and related " +
		"runtime trace writes.",
}

// RuntimeAggregate owns path runtime transition invariants.
//
// Write method failures should return *aggregates.Error with codes:
// CodeValidation, CodeNotFound, CodeConflict, CodeInvariantViolation, CodeRetryable, CodeInternal.
type RuntimeAggregate interface {
	Aggregate

	// StartPathRun initializes a runtime path run and its initial related state atomically.
	StartPathRun(ctx context.Context, in StartPathRunInput) (StartPathRunResult, error)

	// AdvancePathRun atomically applies a runtime transition and records decision metadata.
	AdvancePathRun(ctx context.Context, in AdvancePathRunInput) (AdvancePathRunResult, error)
}

type StartPathRunInput struct {
	UserID         uuid.UUID
	PathID         uuid.UUID
	PathRunID      uuid.UUID
	InitialNodeID  *uuid.UUID
	InitialState   string
	IdempotencyKey string
	EventAt        time.Time
	Metadata       map[string]any
}

type StartPathRunResult struct {
	PathRunID    uuid.UUID
	NodeRunID    *uuid.UUID
	ActiveNodeID *uuid.UUID
	CurrentState string
	TransitionAt time.Time
}

type AdvancePathRunInput struct {
	UserID          uuid.UUID
	PathID          uuid.UUID
	PathRunID       uuid.UUID
	FromState       string
	ToState         string
	FromNodeID      *uuid.UUID
	ToNodeID        *uuid.UUID
	FromActivityID  *uuid.UUID
	ToActivityID    *uuid.UUID
	DecisionTraceID *uuid.UUID
	IdempotencyKey  string
	Reason          string
	EventAt         time.Time
	Metadata        map[string]any
}

type AdvancePathRunResult struct {
	PathRunID     uuid.UUID
	PathRunState  string
	NodeRunID     *uuid.UUID
	ActivityRunID *uuid.UUID
	TransitionID  uuid.UUID
	TransitionAt  time.Time
}

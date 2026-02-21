package aggregates

import (
	"context"
	"time"

	"github.com/google/uuid"
)

var ThreadAggregateContract = Contract{
	Name:             "Chat.ThreadAggregate",
	WriteTxOwnership: WriteTxOwnedByAggregate,
	ReadPolicy:       ReadPolicyInvariantScoped,
	Notes:            "Owns atomic thread/message/turn/state consistency for chat turn progression writes.",
}

// ThreadAggregate owns chat thread progression invariants.
//
// Write method failures should return *aggregates.Error with codes:
// CodeValidation, CodeNotFound, CodeConflict, CodeInvariantViolation, CodeRetryable, CodeInternal.
type ThreadAggregate interface {
	Aggregate

	// CommitTurn atomically persists user+assistant message pair, turn state, and thread metadata changes.
	CommitTurn(ctx context.Context, in CommitTurnInput) (CommitTurnResult, error)

	// MarkTurnFailed atomically records turn failure status and associated thread/message updates.
	MarkTurnFailed(ctx context.Context, in MarkTurnFailedInput) (MarkTurnFailedResult, error)
}

type CommitTurnInput struct {
	UserID           uuid.UUID
	ThreadID         uuid.UUID
	TurnID           uuid.UUID
	JobID            *uuid.UUID
	UserMessage      TurnMessageInput
	AssistantMessage TurnMessageInput
	TurnStatus       string
	IdempotencyKey   string
	EventAt          time.Time
	ThreadMetadata   map[string]any
	TurnMetadata     map[string]any
}

type TurnMessageInput struct {
	MessageID uuid.UUID
	Role      string
	Content   string
	Metadata  map[string]any
}

type CommitTurnResult struct {
	ThreadID           uuid.UUID
	TurnID             uuid.UUID
	UserMessageID      uuid.UUID
	AssistantMessageID uuid.UUID
	TurnStatus         string
	CommittedAt        time.Time
}

type MarkTurnFailedInput struct {
	UserID       uuid.UUID
	ThreadID     uuid.UUID
	TurnID       uuid.UUID
	JobID        *uuid.UUID
	FailureCode  string
	FailureCause string
	FailedAt     time.Time
	Metadata     map[string]any
}

type MarkTurnFailedResult struct {
	ThreadID   uuid.UUID
	TurnID     uuid.UUID
	TurnStatus string
	RecordedAt time.Time
}

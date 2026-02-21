package aggregates

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

var NodeDocAggregateContract = Contract{
	Name:             "DocGen.NodeDocAggregate",
	WriteTxOwnership: WriteTxOwnedByAggregate,
	ReadPolicy:       ReadPolicyInvariantScoped,
	Notes:            "Owns atomic node doc revision/variant/exposure/outcome lifecycle consistency.",
}

// NodeDocAggregate owns node-doc lifecycle invariants.
//
// Write method failures should return *aggregates.Error with codes:
// CodeValidation, CodeNotFound, CodeConflict, CodeInvariantViolation, CodeRetryable, CodeInternal.
type NodeDocAggregate interface {
	Aggregate

	// CommitRevision atomically persists a node-doc revision and associated generation trace/state effects.
	CommitRevision(ctx context.Context, in CommitNodeDocRevisionInput) (CommitNodeDocRevisionResult, error)

	// RecordVariantOutcome atomically persists doc variant outcome and exposure progression effects.
	RecordVariantOutcome(ctx context.Context, in RecordDocVariantOutcomeInput) (RecordDocVariantOutcomeResult, error)
}

type CommitNodeDocRevisionInput struct {
	UserID          uuid.UUID
	PathID          uuid.UUID
	PathNodeID      uuid.UUID
	DocID           uuid.UUID
	RevisionID      uuid.UUID
	GenerationRunID *uuid.UUID
	Operation       string
	Status          string
	BeforeJSON      json.RawMessage
	AfterJSON       json.RawMessage
	TraceMetadata   map[string]any
	CommittedAt     time.Time
}

type CommitNodeDocRevisionResult struct {
	DocID       uuid.UUID
	RevisionID  uuid.UUID
	Status      string
	CommittedAt time.Time
}

type RecordDocVariantOutcomeInput struct {
	UserID       uuid.UUID
	PathID       uuid.UUID
	PathNodeID   uuid.UUID
	DocID        uuid.UUID
	VariantID    uuid.UUID
	ExposureID   uuid.UUID
	OutcomeID    uuid.UUID
	OutcomeLabel string
	ObservedAt   time.Time
	Metadata     map[string]any
}

type RecordDocVariantOutcomeResult struct {
	VariantID  uuid.UUID
	ExposureID uuid.UUID
	OutcomeID  uuid.UUID
	RecordedAt time.Time
}

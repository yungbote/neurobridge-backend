package aggregates

import (
	"context"
	"time"

	"github.com/google/uuid"
)

var UserConceptAggregateContract = Contract{
	Name:             "Learning.UserConceptAggregate",
	WriteTxOwnership: WriteTxOwnedByAggregate,
	ReadPolicy:       ReadPolicyInvariantScoped,
	Notes: "Owns multi-entity consistency for concept state, evidence, calibration, and readiness " +
		"updates in one aggregate write boundary.",
}

// UserConceptAggregate owns user concept-state invariant writes.
//
// Write method failures should return *aggregates.Error with codes:
// CodeValidation, CodeNotFound, CodeConflict, CodeInvariantViolation, CodeRetryable, CodeInternal.
type UserConceptAggregate interface {
	Aggregate

	// ApplyEvidence atomically applies new evidence and related concept-state changes.
	ApplyEvidence(ctx context.Context, in ApplyUserConceptEvidenceInput) (ApplyUserConceptEvidenceResult, error)

	// ResolveMisconception atomically records misconception-resolution effects.
	ResolveMisconception(ctx context.Context, in ResolveUserMisconceptionInput) (ResolveUserMisconceptionResult, error)
}

type ApplyUserConceptEvidenceInput struct {
	UserID          uuid.UUID
	ConceptID       uuid.UUID
	EvidenceID      uuid.UUID
	Source          string
	EventAt         time.Time
	MasteryDelta    float64
	ConfidenceDelta float64
	Correctness     *bool
	Metadata        map[string]any
}

type ApplyUserConceptEvidenceResult struct {
	UserConceptStateID       uuid.UUID
	UserConceptEvidenceID    uuid.UUID
	UserConceptCalibrationID uuid.UUID
	ReadinessUpdated         bool
	AlertRaised              bool
	AppliedAt                time.Time
}

type ResolveUserMisconceptionInput struct {
	UserID            uuid.UUID
	ConceptID         uuid.UUID
	MisconceptionID   uuid.UUID
	ResolutionState   string
	ResolvedAt        time.Time
	ResolutionSummary string
	Metadata          map[string]any
}

type ResolveUserMisconceptionResult struct {
	UserMisconceptionInstanceID uuid.UUID
	ResolutionRecorded          bool
	ReadinessUpdated            bool
	AppliedAt                   time.Time
}

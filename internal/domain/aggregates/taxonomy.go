package aggregates

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

var TaxonomyAggregateContract = Contract{
	Name:             "Library.TaxonomyAggregate",
	WriteTxOwnership: WriteTxOwnedByAggregate,
	ReadPolicy:       ReadPolicyInvariantScoped,
	Notes:            "Owns atomic taxonomy graph/state/snapshot mutations for user library organization.",
}

// TaxonomyAggregate owns taxonomy consistency invariants.
//
// Write method failures should return *aggregates.Error with codes:
// CodeValidation, CodeNotFound, CodeConflict, CodeInvariantViolation, CodeRetryable, CodeInternal.
type TaxonomyAggregate interface {
	Aggregate

	// ApplyTaxonomyRefinement atomically applies node/edge/membership mutations and state updates.
	ApplyTaxonomyRefinement(ctx context.Context, in ApplyTaxonomyRefinementInput) (ApplyTaxonomyRefinementResult, error)

	// CommitTaxonomySnapshot atomically persists a new snapshot and updates taxonomy state pointers.
	CommitTaxonomySnapshot(ctx context.Context, in CommitTaxonomySnapshotInput) (CommitTaxonomySnapshotResult, error)
}

type ApplyTaxonomyRefinementInput struct {
	UserID        uuid.UUID
	Facet         string
	MutationID    uuid.UUID
	NodesUpsert   []TaxonomyNodeMutation
	EdgesUpsert   []TaxonomyEdgeMutation
	MembersUpsert []TaxonomyMembershipMutation
	NodesDelete   []uuid.UUID
	EdgesDelete   []uuid.UUID
	MembersDelete []uuid.UUID
	StatePatch    TaxonomyStatePatch
	AppliedAt     time.Time
	Metadata      map[string]any
}

type TaxonomyNodeMutation struct {
	NodeID      uuid.UUID
	Key         string
	Kind        string
	Name        string
	Description string
	Version     int
}

type TaxonomyEdgeMutation struct {
	EdgeID     uuid.UUID
	Kind       string
	FromNodeID uuid.UUID
	ToNodeID   uuid.UUID
	Weight     float64
	Version    int
}

type TaxonomyMembershipMutation struct {
	MembershipID uuid.UUID
	PathID       uuid.UUID
	NodeID       uuid.UUID
	Weight       float64
	AssignedBy   string
	Version      int
}

type TaxonomyStatePatch struct {
	Dirty                *bool
	NewPathsSinceRefine  *int
	PendingUnsortedPaths *int
}

type ApplyTaxonomyRefinementResult struct {
	UserID              uuid.UUID
	Facet               string
	NodesUpserted       int
	EdgesUpserted       int
	MembershipsUpserted int
	AppliedAt           time.Time
}

type CommitTaxonomySnapshotInput struct {
	UserID       uuid.UUID
	Facet        string
	SnapshotID   uuid.UUID
	SnapshotJSON json.RawMessage
	Version      int
	BuiltAt      time.Time
	Metadata     map[string]any
}

type CommitTaxonomySnapshotResult struct {
	SnapshotID  uuid.UUID
	Version     int
	CommittedAt time.Time
}

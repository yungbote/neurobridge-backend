# Aggregate Repositories

## Purpose
Aggregate repositories are behavioral write boundaries for invariant-critical flows.

They are not structural wrappers around table repos.

## Layering
- `internal/data/repos/*`: table repos, schema-aware CRUD primitives.
- `internal/domain/aggregates`: domain-facing aggregate contracts (no GORM/dbctx/table-repo types).
- `internal/data/aggregates/*`: aggregate implementations that compose table repos internally.
- service/module/job layer: calls aggregate methods for invariant-critical writes.

## Composition Boundary Placement
Aggregates belong inside existing domain groups in `internal/app/repos.go`.

Correct examples:
- `repos.Learning.UserConcept`
- `repos.Paths.Runtime`
- `repos.Library.Taxonomy`
- `repos.Chat.Thread`
- `repos.DocGen.NodeDoc`
- `repos.Jobs.Saga`

Forbidden:
- `type Repos struct { Aggregates ... }`
- any cross-domain global aggregate bucket.

## Contract Rules
- Aggregate APIs are intent-first (`ApplyEvidence`, `AdvancePathRun`, `CommitTurn`).
- Aggregate methods use explicit input/output structs.
- Aggregate methods return aggregate error semantics (`validation`, `conflict`, `invariant_violation`, `retryable`, etc.).
- Write methods own transaction boundaries internally and commit/rollback atomically.

## API Shape: Good vs Bad
Good (behavioral boundary):

```go
type UserConceptAggregate interface {
	Aggregate
	ApplyEvidence(ctx context.Context, in ApplyUserConceptEvidenceInput) (ApplyUserConceptEvidenceResult, error)
}
```

Why this is good:
- caller describes intent, not table operations,
- aggregate owns invariant checks and transaction scope,
- table-repo details stay hidden.

Bad (wrapper that leaks table repos):

```go
type UserConceptAggregateRepo struct {
	StateRepo       repos.UserConceptStateRepo
	EvidenceRepo    repos.UserConceptEvidenceRepo
	CalibrationRepo repos.UserConceptCalibrationRepo
}

func (a *UserConceptAggregateRepo) State() repos.UserConceptStateRepo {
	return a.StateRepo
}
```

Why this is bad:
- service layer can still orchestrate table writes directly,
- invariants remain distributed outside the aggregate,
- abstraction adds structure without semantic ownership.

## Read Policy
- Writes that enforce invariants must go through aggregates.
- Aggregates can perform scoped reads needed for invariant decisions.
- Broad queries/read-model access remain on table repos.
- Analytics/reporting paths can keep direct table-repo reads.

## Approved Exceptions For Direct Table Repos
Direct table repos remain allowed when invariant ownership is not required:

- read-only query paths and projection/read-model fetches,
- analytics/reporting jobs that do not mutate consistency boundaries,
- simple single-table writes with no cross-entity invariant coupling.

When in doubt, default to table repos first and promote to an aggregate only after detection evidence confirms invariant + atomicity pressure.

## When To Create An Aggregate
Create one when a flow has all of:
- multi-repo write coordination,
- cross-entity invariant ownership,
- atomicity/concurrency pressure.

Do not create one for:
- single-table simple writes,
- broad read/query projections,
- cosmetic symmetry across domains.

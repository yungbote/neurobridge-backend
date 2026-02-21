# Repo Contributor Notes

## When Adding A New Repo
Use this checklist to keep composition boundaries clean.

- Decide the owning domain first (`Auth`, `Users`, `Events`, `Learning`, `Materials`, `Concepts`, `Activities`, `Paths`, `Runtime`, `Library`, `DocGen`, `Jobs`, `Chat`).
- Add the interface alias and constructor in `internal/data/repos/repos.go`.
- Add the field to the matching grouped struct in `internal/app/repos.go`.
- Wire it in the matching domain constructor (`wireXRepos`).
- Use grouped access only in app wiring (`repos.<Domain>.<Entity>`).
- If a constructor needs many repos, prefer a typed `Deps` struct over positional args.
- Add or update tests for new `Deps` constructor paths when wiring changes are non-trivial.

## When Adding A New Aggregate
Use this checklist before introducing a new aggregate contract/implementation.

- Confirm need with detection evidence: multi-repo write + invariant ownership + atomicity pressure.
- Define/extend contract in `internal/domain/aggregates` using intent-first methods and explicit I/O types.
- Keep contracts infrastructure-free (no `gorm`, no `dbctx`, no table repo types).
- Add implementation in `internal/data/aggregates/*` that composes table repos internally.
- Place aggregate in the owning domain group in `internal/app/repos.go` (for example `Learning.UserConcept`).
- Do not create top-level `Repos.Aggregates`.
- Keep broad query/read-model use on table repos unless invariant-scoped reads are needed.
- Add integration tests that verify invariant enforcement + rollback on partial failure.

## Aggregate Decision Framework
Use this gate before writing a new aggregate API:

- Confirm the flow coordinates `2+` table repos in one write path.
- Confirm an invariant spans multiple rows/entities.
- Confirm partial failure would leave invalid state.
- Confirm the operation needs one transaction boundary owned by the callee.
- If fewer than two of the above are true, keep table-level repos.

Approved direct table-repo exceptions:

- read-only queries/read-model projections,
- analytics/reporting jobs,
- simple single-table non-invariant writes.

## Adding A New Aggregate Method
- Start from intent (`ApplyEvidence`, `CommitTurn`, `AdvanceRuntime`) instead of CRUD names.
- Add explicit input/output structs in `internal/domain/aggregates`.
- Implement method in `internal/data/aggregates/*` and compose table repos internally.
- Keep transaction ownership in the aggregate implementation, not service/module/job callers.
- Update `internal/app/repos.go` inside the owning domain group only (never `Repos.Aggregates`).
- Wire call sites to consume the aggregate method rather than coordinating table repos directly.

## Testing Invariants And Rollback
- Add happy-path integration test for the aggregate method.
- Add invariant-violation test (must fail with aggregate error semantics).
- Add rollback test by injecting an inner write failure and asserting no partial writes remain.
- Add concurrency/CAS conflict test when the method updates shared mutable state.
- Re-run boundary checks:
  - `./scripts/check_repo_boundaries.sh`
  - `./scripts/check_aggregate_guardrails.sh`

## Placement Guide
- `Auth`: login/session identity/token/nonces.
- `Users`: profile, personalization, session state, gaze.
- `Events`: user events and cursors.
- `Learning`: learner knowledge state/model/calibration/misconceptions/readiness.
- `Materials`: files/chunks/assets/material sets/artifacts.
- `Concepts`: concept graph entities, evidence, priors, versioning/rollback.
- `Activities`: learning activities/variants/citations/patterns/completion.
- `Paths`: path structure and runtime path/node/activity runs.
- `Runtime`: runtime decision traces/snapshots.
- `Library`: taxonomy and user library indexing.
- `DocGen`: node docs/revisions/variants/probes/doc generation traces.
- `Jobs`: job runs and saga persistence.
- `Chat`: chat threads/messages/state/memory/claims/docs/turns.

## Anti-Patterns To Avoid
- Adding new top-level flat fields to `type Repos`.
- Referencing repos as `repos.<Entity>` in app wiring.
- Expanding positional constructor arg lists when a `Deps` struct is appropriate.
- Creating aggregate wrappers that just expose inner table repos.
- Introducing a top-level cross-domain aggregate bucket (`Repos.Aggregates`).

## CI Boundary Check
Boundary regressions are blocked by:

- `scripts/check_repo_boundaries.sh`
- `scripts/check_aggregate_guardrails.sh`

Run it locally before pushing:

```bash
./scripts/check_repo_boundaries.sh
./scripts/check_aggregate_guardrails.sh
```

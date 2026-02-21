# Repo Composition Boundaries

## Purpose
This backend intentionally uses grouped repository composition boundaries in `internal/app/repos.go` instead of a flat service-locator surface.

The goal is to keep wiring explicit by subsystem and avoid hidden cross-domain coupling.

## Current Shape
`type Repos struct` exposes grouped domains only:

- `Auth`
- `Users`
- `Events`
- `Learning`
- `Materials`
- `Concepts`
- `Activities`
- `Paths`
- `Runtime`
- `Library`
- `DocGen`
- `Jobs`
- `Chat`

Wiring follows the same structure via focused constructors:

- `wireAuthRepos`
- `wireUserRepos`
- `wireEventRepos`
- `wireLearningRepos`
- `wireMaterialRepos`
- `wireConceptRepos`
- `wireActivityRepos`
- `wirePathRepos`
- `wireRuntimeRepos`
- `wireLibraryRepos`
- `wireDocGenRepos`
- `wireJobRepos`
- `wireChatRepos`

## Naming Rules
- Repos are referenced as `repos.<Domain>.<Entity>` in app wiring.
- Domain names are PascalCase and pluralized where it improves readability (`Users`, `Paths`, `Materials`).
- Entity fields are concrete repo names (`Path`, `PathNode`, `MaterialFile`, `JobRun`).
- Avoid exposing one domain's repo through another domain's struct.

## Wiring Rules
- `internal/app/services.go` and `internal/app/http.go` must use grouped repo access only.
- Prefer typed `Deps` structs for high-fanout constructor boundaries.
- Keep backward-compat constructor shims only temporarily; new call sites should use `WithDeps` constructors.

## Aggregate Placement Rules
- Aggregate contracts are domain-scoped members inside existing groups (for example `repos.Learning.UserConcept`).
- Do not create a top-level `Repos.Aggregates` bucket.
- Aggregate implementations live under `internal/data/aggregates/*` and compose table repos internally.
- Services/modules/jobs should call aggregate methods for invariant-critical writes.

## Guardrail
A CI guard scripts enforce this boundary in app wiring and migrated aggregate flows:

- `scripts/check_repo_boundaries.sh`
- `scripts/check_aggregate_guardrails.sh`

It fails when flat repo access patterns are reintroduced (for example `repos.MaterialFile` or `a.Repos.MaterialFile`).

## Related Notes
Contributor migration guidance (how to add or place new repos):

- `docs/repo-contributor-notes.md`
- `docs/architecture/aggregate-repositories.md`

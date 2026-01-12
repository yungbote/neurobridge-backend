# Library module

This module owns library taxonomy **use-cases** (routing, refining, snapshotting) and optional Neo4j projections.

## Entry points
- Public module entry point is `internal/modules/library` (`Usecases` in `usecases.go`).
- `internal/modules/library/steps` contains implementation details; pipelines/handlers should call the module, not steps.

# Domain modules

`internal/modules/**` contains domain modules (modular monolith). Each module owns:

- use-cases / domain services
- any module-local prompt specs and schemas
- optional graph writers (Neo4j upserts) for that domain

Orchestration layers (`internal/jobs/**`, `internal/http/**`) should call module APIs and avoid
embedding business logic directly.


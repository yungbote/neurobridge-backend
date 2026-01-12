# Learning module

This module owns learning-specific **use-cases** (material → concepts → paths → content).

## Entry points
- Public module entry point is `internal/modules/learning` (`Usecases` in `usecases.go`).
- `internal/modules/learning/steps` contains implementation details (keep orchestration layers calling the module, not steps).

## Notes
- Some use-cases optionally sync graphs into Neo4j when `NEO4J_URI` is configured.
- LLM-backed steps use `internal/platform/openai` with structured outputs where possible.

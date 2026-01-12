# Platform (infrastructure adapters)

`internal/platform/**` is the long-term home for shared infrastructure adapters and cross-cutting utilities:

- external service clients (OpenAI, Neo4j, Pinecone, GCP, etc.)
- logging, config, timeouts, HTTP helpers
- DB helpers and transactional utilities

Stage 2 introduced `internal/platform/**` as the **boundary**; Stage 3 migrated implementations into
`internal/platform/**` once dependency direction and tests made the moves low-risk.

## Status
- Implementations live in `internal/platform/**`.
- Legacy paths (`internal/clients/**`, `internal/pkg/**`) are removed; import `internal/platform/**` directly.

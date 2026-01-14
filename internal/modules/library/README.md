# Library module

This module owns library taxonomy **use-cases** (routing, refining, snapshotting) and optional Neo4j projections.

The library taxonomy is a per-user DAG used to organize completed/ready paths into stable “topic anchors” and earned bottom-up categories for navigation and recommendations.

## Entry points
- Public entrypoint: `neurobridge-backend/internal/modules/library/usecases.go` (`library.Usecases`).
- Taxonomy API surface: `neurobridge-backend/internal/modules/library/taxonomy_api.go`
- Implementation details: `neurobridge-backend/internal/modules/library/steps` (jobs/handlers should call `Usecases`, not step functions directly).

## How it’s used

### HTTP
Read endpoints are served by:
- Router: `neurobridge-backend/internal/http/router.go` (`/api/library/...`)
- Handler: `neurobridge-backend/internal/http/handlers/library.go`

The primary read path is a **snapshot** for fast homepage loads.

### Jobs
The taxonomy is maintained asynchronously:
- `library_taxonomy_route` (per path): assigns the new path to stable topic anchors (and possibly inbox) and rebuilds the snapshot.
- `library_taxonomy_refine` (per user): earns bottom-up categories under anchors and recomputes related edges + snapshot.

Jobs are typically enqueued after a path build completes.

## Storage model (Postgres)
Tables are GORM AutoMigrate’d and live under `neurobridge-backend/internal/domain` with repos under `neurobridge-backend/internal/data/repos/learning` and `.../user` as needed.

Core taxonomy tables (see `docs/backend/library-taxonomy.md` for full details):
- `library_taxonomy_node`
- `library_taxonomy_edge`
- `library_taxonomy_membership`
- `library_taxonomy_state`
- `library_taxonomy_snapshot`
- `library_path_embedding`

## Configuration knobs
Most knobs are env vars (defaults in `neurobridge-infra/local/docker-compose.yml` and `neurobridge-infra/local/.env.example`), including:
- `LIBRARY_TAXONOMY_MAX_MEMBERSHIPS_PER_FACET`
- `LIBRARY_TAXONOMY_MAX_NEW_NODES_PER_FACET`
- `LIBRARY_TAXONOMY_ASSIGN_SIMILARITY_THRESHOLD_PCT`
- `LIBRARY_TAXONOMY_RELATED_SIMILARITY_THRESHOLD_PCT`
- `LIBRARY_TAXONOMY_REFINE_NEW_PATHS_THRESHOLD`
- `LIBRARY_TAXONOMY_REFINE_UNSORTED_THRESHOLD`

## Neo4j integration (optional)
If `NEO4J_URI` is configured, taxonomy graphs can be projected into Neo4j (best-effort). Postgres remains canonical.

## Extending the library module (safely)
- Preserve anchor stability: anchors are meant to be ultra-stable top-level navigation.
- Add new facets only after defining clear invariants and UX semantics; today the primary facet is `"topic"`.
- Keep snapshot building deterministic and fast; heavy work belongs in jobs.

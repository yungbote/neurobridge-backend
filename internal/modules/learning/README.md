# Learning module

This module owns learning-specific **use-cases** and LLM-backed pipelines:

- materials → chunks/embeddings
- intent intake → concept graph → path planning
- node docs (lessons) + media planning/rendering
- practice generation (activities, drills, quick checks, flashcards)
- graph-assisted retrieval (GraphRAG) and optional Neo4j projections

## Entry points
- Public entrypoint: `neurobridge-backend/internal/modules/learning/usecases.go` (`learning.Usecases`).
- Implementation details: `neurobridge-backend/internal/modules/learning/steps` (jobs should call `Usecases`, not step functions directly).
- Content schemas + validators: `neurobridge-backend/internal/modules/learning/content`.
- Graph-assisted retrieval: `neurobridge-backend/internal/modules/learning/graphrag`.
- Ingestion: `neurobridge-backend/internal/modules/learning/ingestion`.

## Notes
- End-to-end system flow (uploads → path → lessons): `docs/file-uploads-to-path-generation.md`
- Graph-assisted RAG: `docs/backend/graphrag.md`

## Job pipeline surface area (internal/jobs/pipeline/*)
Most learning work runs as jobs, especially the `learning_build` DAG and its child stages:
- `web_resources_seed` (prompt-only builds)
- `ingest_chunks`, `embed_chunks`, `material_set_summarize`
- `path_intake`, `concept_graph_build`, `path_structure_refine`, `material_kg_build`
- `path_plan_build`, `path_cover_render`
- `node_figures_plan_build`, `node_figures_render`
- `node_videos_plan_build`, `node_videos_render`
- `node_doc_build`, `node_doc_patch`
- `realize_activities` + audits/finalization (`coverage_coherence_audit`, `priors_refresh`, `completed_unit_refresh`, etc)

The orchestration layer is in `neurobridge-backend/internal/jobs/pipeline/learning_build`.

## Content contracts (schemas + validation)
The learning system enforces strict JSON contracts for model outputs and validates persisted forms.

Node docs:
- Generation schema: `neurobridge-backend/internal/modules/learning/content/schema/node_doc_gen_v1.json`
- Persisted schema: `content.NodeDocV1` validated by `neurobridge-backend/internal/modules/learning/content/validate.go`
- Builder: `neurobridge-backend/internal/modules/learning/steps/node_doc_build.go`

Quick checks:
- Persisted as `type="quick_check"` blocks inside `NodeDocV1`.
- Attempt endpoint: `POST /api/path-nodes/:id/quick-checks/:block_id/attempt` (deterministic grading for MCQ/true-false).
- Implementation: `neurobridge-backend/internal/modules/learning/quick_check.go`

Flashcards:
- Persisted as `type="flashcard"` blocks inside `NodeDocV1`.
- Front/back recall prompts with citations (no grading endpoint yet).

Drills:
- Generated via `POST /api/path-nodes/:id/drills/:kind` and stored as drill instances.
- Implementation: `neurobridge-backend/internal/modules/learning/drills.go`

## Neo4j integration (optional)
When `NEO4J_URI` is configured, some steps will upsert graph projections (best-effort):
- Neo4j writers: `neurobridge-backend/internal/data/graph`
- Learning graph writers typically live alongside the corresponding pipeline steps.

The system must remain correct without Neo4j; Postgres remains canonical.

## Extending the learning module (safely)
- Prefer adding new generation outputs as schema-driven JSON with validators + repair passes.
- Keep validations strict; add deterministic repair only for predictable “near miss” failures.
- Add regression tests for any deterministic repair, schema changes, or ordering rules.
- Treat graphs as derived products: never use Neo4j as the only source of truth.

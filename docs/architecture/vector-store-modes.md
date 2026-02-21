# Vector Store Modes

## Operational Docs
- Mode selection + env/run examples: `docs/operations/vector-store-mode-guide.md`
- Local Qdrant troubleshooting runbook: `docs/operations/vector-store-qdrant-local-troubleshooting.md`

## Contract
- Vector provider selection is mode-driven by object storage mode defaults:
- `OBJECT_STORAGE_MODE=gcs_emulator` -> vector provider `qdrant`.
- `OBJECT_STORAGE_MODE=gcs` -> vector provider `pinecone`.
- Provider resolution metadata is carried in app config:
- `VectorProvider`
- `VectorProviderModeSource`

## Bootstrap Behavior
- Qdrant path (`qdrant`):
- Validates `QDRANT_URL`, `QDRANT_COLLECTION`, `QDRANT_VECTOR_DIM`.
- Verifies `GET /readyz`.
- Verifies collection exists and vector dimension matches config.
- Startup fails fast on invalid/unreachable Qdrant config.
- Pinecone path (`pinecone`):
- Uses existing Pinecone config and bootstrap path.
- If `PINECONE_API_KEY` is missing, backend keeps existing explicit degraded behavior (vector store disabled).
- No cross-provider fallback:
- Emulator mode never initializes Pinecone.
- Cloud mode never initializes Qdrant.

## Interface Stability
- Services/jobs keep using the same vector-store contract:
- `Upsert`
- `QueryMatches`
- `QueryIDs`
- `DeleteIDs`
- No callsite provider branching is required.

## Namespace + ID Semantics
- Namespace qualification remains `<prefix>` or `<prefix>:<namespace>`.
- Qdrant adapter persists:
- `_nb_namespace` for namespace isolation.
- `_nb_vector_id` for stable logical ID mapping.
- Qdrant point IDs are deterministic UUIDs derived from `qualified_namespace|vector_id`.

## Compensation Semantics
- Canonical saga vector compensation action kind: `vector_delete_ids`.
- Legacy compatibility: `pinecone_delete_ids` is still accepted for previously written rows.
- Rollback behavior is provider-agnostic and executes through the shared vector-store interface.

## Observability
- Provider bootstrap signals:
- `nb_vector_store_provider_active{provider}`
- `nb_vector_store_provider_bootstrap_total{provider,status,code}`
- Operation signals:
- `nb_vector_store_operation_total{provider,operation,status}`
- `nb_vector_store_operation_duration_seconds{provider,operation,status}`

## GCP Provider Invariance
- Vector mode changes do not change GCP provider selection.
- DocumentAI, Vision, Speech, and Video integrations continue using GCP clients in all vector modes.

## Guardrails
- Local vector-emulator verification:
- `scripts/smoke_vector_emulator_mode.sh`
- Non-local leakage prevention:
- `neurobridge-infra/gcloud/scripts/check_vector_store_mode_guardrail.sh`

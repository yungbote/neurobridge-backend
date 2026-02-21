# Vector Store Mode Guide

## Scope
Operational entry point for selecting vector provider behavior by mode.
For architecture details, see `docs/architecture/vector-store-modes.md`.

## Mode Contract
- `OBJECT_STORAGE_MODE=gcs_emulator`:
- Vector provider resolves to Qdrant.
- `OBJECT_STORAGE_MODE=gcs`:
- Vector provider resolves to Pinecone.

No mode should initialize both providers.

## Environment Examples

### Local Emulator (Dev)
```env
OBJECT_STORAGE_MODE=gcs_emulator
QDRANT_URL=http://qdrant:6333
QDRANT_COLLECTION=neurobridge
QDRANT_NAMESPACE_PREFIX=nb
QDRANT_VECTOR_DIM=3072
QDRANT_COLLECTION_DISTANCE=Cosine
```

### Cloud (Dev/Staging/Prod)
```env
OBJECT_STORAGE_MODE=gcs
PINECONE_API_VERSION=2025-10
PINECONE_BASE_URL=https://api.pinecone.io
PINECONE_INDEX_NAME=neurobridge
PINECONE_INDEX_HOST=<pinecone-index-host>
PINECONE_NAMESPACE_PREFIX=nb
```

Notes:
- `PINECONE_API_KEY` is secret-managed and not stored in configmap.
- Qdrant env vars are local-only and should not be present in non-local manifests.

## Local Quickstart
1. Start local stack:
```bash
docker compose -f neurobridge-infra/local/docker-compose.yml up -d qdrant qdrant-init backend-api backend-worker
```
2. Run smoke validation:
```bash
cd neurobridge-backend
./scripts/smoke_vector_emulator_mode.sh
```

## Cloud Guardrails
Run before non-local deploy:
```bash
cd neurobridge-infra
./gcloud/scripts/check_object_storage_mode_guardrail.sh
./gcloud/scripts/check_vector_store_mode_guardrail.sh
```

## GCP Service Providers
Vector provider mode does not alter GCP service-provider behavior:
- DocumentAI stays on GCP.
- Vision stays on GCP.
- Speech stays on GCP.
- Video Intelligence stays on GCP.

# Vector Store (Qdrant Local) Troubleshooting

## Scope
Runbook for local emulator vector mode (`OBJECT_STORAGE_MODE=gcs_emulator` -> Qdrant).

## 1) Startup Fails With Qdrant Config Error
Symptoms:
- Startup fails with missing/invalid Qdrant env errors.

Checks:
```bash
echo "$QDRANT_URL"
echo "$QDRANT_COLLECTION"
echo "$QDRANT_VECTOR_DIM"
```

Fix:
- Set:
- `QDRANT_URL` (absolute URL, for local usually `http://qdrant:6333` in compose or `http://127.0.0.1:6333` from host).
- `QDRANT_COLLECTION` (example: `neurobridge`).
- `QDRANT_VECTOR_DIM` (positive integer, example: `3072`).

## 2) Qdrant Not Reachable
Symptoms:
- Bootstrap fails with connect/ready-check errors.
- Smoke script waits indefinitely then fails.

Checks:
```bash
docker compose -f neurobridge-infra/local/docker-compose.yml ps qdrant qdrant-init
curl -fsS "http://127.0.0.1:6333/readyz"
```

Fix:
```bash
docker compose -f neurobridge-infra/local/docker-compose.yml up -d qdrant qdrant-init
```

## 3) Collection Missing Or Wrong Dimension
Symptoms:
- Bootstrap fails with collection validation mismatch.

Checks:
```bash
curl -fsS "http://127.0.0.1:6333/collections/neurobridge"
```

Fix:
- Ensure `qdrant-init` ran successfully.
- Ensure `QDRANT_VECTOR_DIM` matches collection vector size.
- Re-run init:
```bash
docker compose -f neurobridge-infra/local/docker-compose.yml up -d qdrant-init
```

## 4) Query Fails On Filter Operator
Symptoms:
- Query returns unsupported filter/operator errors.

Cause:
- Current adapter supports the subset in active callsites (`field=value`, `$in`, plus typed handling for `$eq/$ne/$and/$or/$not`).

Fix:
- Use supported filter shapes for vector queries.
- If introducing new operators, add adapter translation coverage and tests.

## 5) Persistence Validation Across Restart
Use smoke script:
```bash
cd neurobridge-backend
./scripts/smoke_vector_emulator_mode.sh
```

This validates:
- app-path write/query,
- direct Qdrant payload-id presence,
- persistence after Qdrant restart,
- cleanup path.

## 6) Explicit Unavailable Behavior
Validation test:
```bash
cd neurobridge-backend
go test ./internal/app -run TestResolveVectorStoreProviderQdrantUnavailableIsExplicit -count=1
```

Expected:
- provider bootstrap fails with explicit connect error classification.
- emulator mode never falls back to Pinecone.

## 7) Logs And Metrics
Logs:
- provider bootstrap logs include provider/mode/source and error codes.

Metrics:
- `nb_vector_store_provider_active{provider}`
- `nb_vector_store_provider_bootstrap_total{provider,status,code}`
- `nb_vector_store_operation_total{provider,operation,status}`
- `nb_vector_store_operation_duration_seconds{provider,operation,status}`

# Object Storage Mode Guide

## Scope
This guide is the operational entry point for selecting and running backend object storage mode.
For architecture details, see `docs/architecture/object-storage-modes.md`.

## Mode Contract
- `OBJECT_STORAGE_MODE=gcs`:
- Default mode.
- Uses real GCS buckets.
- `OBJECT_STORAGE_MODE=gcs_emulator`:
- Local emulator mode.
- Requires `STORAGE_EMULATOR_HOST`.

Supported values are only `gcs` and `gcs_emulator`.

## Environment Examples

### Local Emulator (Dev)
```env
OBJECT_STORAGE_MODE=gcs_emulator
STORAGE_EMULATOR_HOST=http://fake-gcs:4443
OBJECT_STORAGE_PUBLIC_BASE_URL=http://localhost:4443
AVATAR_GCS_BUCKET_NAME=neurobridge-avatar
MATERIAL_GCS_BUCKET_NAME=neurobridge-materials
```

### Real GCS (Dev/Staging/Prod)
```env
OBJECT_STORAGE_MODE=gcs
STORAGE_EMULATOR_HOST=
OBJECT_STORAGE_PUBLIC_BASE_URL=
AVATAR_GCS_BUCKET_NAME=neurobridge-avatar
MATERIAL_GCS_BUCKET_NAME=neurobridge-materials
```

## Local Dev Quickstart
1. Set local env values in `neurobridge-infra/local/.env` (or export in shell) using the emulator example above.
2. Start local stack:
```bash
docker compose -f neurobridge-infra/local/docker-compose.yml up -d fake-gcs fake-gcs-init backend-api backend-worker
```
3. Run local smoke test:
```bash
cd neurobridge-backend
./scripts/smoke_storage_emulator_mode.sh
```
4. Confirm startup logs include:
- `mode=gcs_emulator`
- `mode_source=explicit_or_default` (or `compatibility_fallback` for legacy mode resolution)
- `emulator_host=...`

## Migration Notes (Legacy Emulator Setup)
Legacy behavior still works:
- `OBJECT_STORAGE_MODE` unset
- `STORAGE_EMULATOR_HOST` set

Backend will auto-select emulator mode via compatibility fallback.
Target state is explicit mode selection:
- Set `OBJECT_STORAGE_MODE=gcs_emulator` for local emulator use.
- Set `OBJECT_STORAGE_MODE=gcs` for non-local environments.

## Compatibility Shim Decision
- Decision: keep the compatibility shim (`OBJECT_STORAGE_MODE` unset + `STORAGE_EMULATOR_HOST` set) for now.
- Phase 7 simplification:
- Fallback remains supported, but temporary startup warning was removed.
- Mode-source telemetry remains available via logs/metrics (`compatibility_fallback`).

## Observability Signals

### Logs
Object storage bootstrap logs now include:
- `mode`
- `mode_source`
- `compatibility_fallback`
- `emulator_host`
- `error_code` (on provider bootstrap errors)

### Metrics
- `nb_object_storage_mode_active{mode}` gauge.
- `nb_object_storage_provider_bootstrap_total{mode,status,code}` counter.

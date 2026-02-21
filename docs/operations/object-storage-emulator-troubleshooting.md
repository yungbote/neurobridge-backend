# Object Storage Emulator Troubleshooting

## Scope
Runbook for `OBJECT_STORAGE_MODE=gcs_emulator` issues in local development.

## 1) Backend Fails At Startup With Mode/Host Error
Symptoms:
- Startup fails with `invalid OBJECT_STORAGE_MODE`.
- Startup fails with `requires STORAGE_EMULATOR_HOST`.

Checks:
```bash
echo "$OBJECT_STORAGE_MODE"
echo "$STORAGE_EMULATOR_HOST"
```

Fix:
- Use only `gcs` or `gcs_emulator`.
- When `gcs_emulator`, set `STORAGE_EMULATOR_HOST` to absolute URL (example `http://fake-gcs:4443`).

## 2) Emulator Endpoint Unreachable
Symptoms:
- Download/upload calls fail with connect errors.
- Smoke script fails waiting for emulator.

Checks:
```bash
docker compose -f neurobridge-infra/local/docker-compose.yml ps fake-gcs fake-gcs-init
curl -fsS "http://127.0.0.1:4443/storage/v1/b?project=local-dev"
```

Fix:
- Start local emulator:
```bash
docker compose -f neurobridge-infra/local/docker-compose.yml up -d fake-gcs fake-gcs-init
```
- Ensure `STORAGE_EMULATOR_HOST` matches container-network endpoint used by backend (`http://fake-gcs:4443` in compose).

## 3) Bucket Not Found
Symptoms:
- Upload/read errors mentioning missing bucket.

Checks:
```bash
curl -sS "http://127.0.0.1:4443/storage/v1/b?project=local-dev"
```

Fix:
- Ensure `fake-gcs-init` completed successfully.
- Verify `AVATAR_GCS_BUCKET_NAME` and `MATERIAL_GCS_BUCKET_NAME` match created buckets.
- Re-run init:
```bash
docker compose -f neurobridge-infra/local/docker-compose.yml up -d fake-gcs-init
```

## 4) Generated URLs Not Usable From Host
Symptoms:
- Backend returns URLs with `http://fake-gcs:4443/...` that browser cannot resolve.

Fix:
- Set host-accessible base URL:
```env
OBJECT_STORAGE_PUBLIC_BASE_URL=http://localhost:4443
```
- Keep `STORAGE_EMULATOR_HOST=http://fake-gcs:4443` for container-to-container traffic.

## 5) Video Intelligence Missing In Emulator Mode
Symptoms:
- Warnings indicating Video Intelligence failed in emulator mode.

Expected behavior:
- `AnnotateVideoGCS` is always attempted to preserve GCP provider usage.
- The API is cloud-only (`gs://`), so calls can fail if the object only exists in local emulator storage.

## 6) Verify With Smoke Test
```bash
cd neurobridge-backend
./scripts/smoke_storage_emulator_mode.sh
```

This validates:
- emulator startup,
- bucket CRUD integration,
- ingestion mode-policy routing.

## 7) Observe Logs And Metrics
Log fields:
- `mode`, `mode_source`, `compatibility_fallback`, `emulator_host`, `error_code`.

Metrics:
- `nb_object_storage_mode_active{mode}`
- `nb_object_storage_provider_bootstrap_total{mode,status,code}`

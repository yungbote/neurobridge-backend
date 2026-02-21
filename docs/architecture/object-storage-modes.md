# Object Storage Modes

## Operational Docs
- Mode selection + env/run examples: `docs/operations/object-storage-mode-guide.md`
- Emulator troubleshooting runbook: `docs/operations/object-storage-emulator-troubleshooting.md`

## Contract
- `OBJECT_STORAGE_MODE` controls bucket client mode.
- Supported values:
- `gcs` (default): real Google Cloud Storage buckets.
- `gcs_emulator`: local/storage-emulator mode.

## Validation Rules
- If `OBJECT_STORAGE_MODE=gcs_emulator`, `STORAGE_EMULATOR_HOST` is required and must be an absolute URL (example: `http://fake-gcs:4443`).
- If `OBJECT_STORAGE_MODE` is unset and `STORAGE_EMULATOR_HOST` is set, backend auto-selects emulator mode as a compatibility fallback.
- Any other `OBJECT_STORAGE_MODE` value fails startup with a validation error.

## Required Bucket Env Vars (Both Modes)
- `AVATAR_GCS_BUCKET_NAME`
- `MATERIAL_GCS_BUCKET_NAME`

## URL Shaping Env Vars (Optional)
- `AVATAR_CDN_DOMAIN`
- `MATERIAL_CDN_DOMAIN`
- `OBJECT_STORAGE_PUBLIC_BASE_URL`

## Public URL Strategy
- Category CDN domain takes priority when configured (`AVATAR_CDN_DOMAIN` / `MATERIAL_CDN_DOMAIN`).
- In `gcs_emulator` mode without CDN domains:
- Backend emits emulator-host URLs as `BASE_URL/{bucket}/{key}`.
- `BASE_URL` is `OBJECT_STORAGE_PUBLIC_BASE_URL` when set, otherwise `STORAGE_EMULATOR_HOST`.
- In `gcs` mode without CDN domains:
- Backend emits `https://storage.googleapis.com/{bucket}/{key}` (unchanged behavior).

## Cloud OCR/ASR/Video Behavior In `gcs_emulator` Mode
- DocumentAI:
- `gs://` online/batch paths are not used in emulator mode.
- Backend falls back to byte-based `ProcessBytes` flow where feasible.
- Vision OCR:
- Async `OCRFileInGCS` (`gs://` input/output) is not used in emulator mode.
- Backend falls back to per-page image OCR using `OCRImageBytes` after PDF page render.
- Speech transcription:
- `TranscribeAudioGCS` (`gs://`) is not used in emulator mode.
- Backend falls back to `TranscribeAudioBytes`.
- Video Intelligence:
- `AnnotateVideoGCS` is still attempted in emulator mode to preserve GCP provider usage.
- The API remains cloud-only (`gs://`), so failures are surfaced as explicit warnings when emulator-backed objects are not cloud-reachable.
- Ingestion continues with local audio extraction, frame OCR, and captioning fallbacks when Video Intelligence fails.

## Examples
- Local emulator:
- `OBJECT_STORAGE_MODE=gcs_emulator`
- `OBJECT_STORAGE_PUBLIC_BASE_URL=http://localhost:4443`
- `STORAGE_EMULATOR_HOST=http://fake-gcs:4443`
- Cloud GCS:
- `OBJECT_STORAGE_MODE=gcs`
- `STORAGE_EMULATOR_HOST` unset

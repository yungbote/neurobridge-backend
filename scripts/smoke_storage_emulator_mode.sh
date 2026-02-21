#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPOSE_FILE="${NB_INFRA_LOCAL_COMPOSE_FILE:-${BACKEND_ROOT}/../neurobridge-infra/local/docker-compose.yml}"
EMULATOR_HOST="${NB_GCS_EMULATOR_HOST:-${STORAGE_EMULATOR_HOST:-http://127.0.0.1:${GCS_EMULATOR_PORT:-4443}}}"
EMULATOR_HOST="${EMULATOR_HOST%/}"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required for emulator smoke test" >&2
  exit 1
fi

if [ ! -f "${COMPOSE_FILE}" ]; then
  echo "compose file not found: ${COMPOSE_FILE}" >&2
  exit 1
fi

echo "[smoke] starting fake-gcs emulator via ${COMPOSE_FILE}"
docker compose -f "${COMPOSE_FILE}" up -d fake-gcs fake-gcs-init

echo "[smoke] waiting for emulator at ${EMULATOR_HOST}"
for i in $(seq 1 60); do
  if curl -fsS "${EMULATOR_HOST}/storage/v1/b?project=local-dev" >/dev/null 2>&1; then
    break
  fi
  if [ "${i}" -eq 60 ]; then
    echo "fake-gcs not ready at ${EMULATOR_HOST}" >&2
    exit 1
  fi
  sleep 1
done

echo "[smoke] running storage emulator integration and mode-policy tests"
(
  cd "${BACKEND_ROOT}"
  NB_RUN_GCS_EMULATOR_INTEGRATION=true \
  NB_GCS_EMULATOR_HOST="${EMULATOR_HOST}" \
    go test ./internal/platform/gcp -run 'TestBucketServiceEmulatorCRUDLifecycle' -count=1
  go test ./internal/modules/learning/ingestion/pipeline ./internal/modules/learning/ingestion/extractor ./internal/app -count=1
)

echo "[smoke] completed successfully"

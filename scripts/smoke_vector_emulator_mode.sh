#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPOSE_FILE="${NB_INFRA_LOCAL_COMPOSE_FILE:-${BACKEND_ROOT}/../neurobridge-infra/local/docker-compose.yml}"

QDRANT_URL="${NB_QDRANT_URL:-${QDRANT_URL:-http://127.0.0.1:${QDRANT_HTTP_PORT:-6333}}}"
QDRANT_URL="${QDRANT_URL%/}"
QDRANT_COLLECTION="${NB_QDRANT_COLLECTION:-${QDRANT_COLLECTION:-neurobridge}}"
QDRANT_NAMESPACE_PREFIX="${NB_QDRANT_NAMESPACE_PREFIX:-${QDRANT_NAMESPACE_PREFIX:-nb}}"
QDRANT_VECTOR_DIM="${NB_QDRANT_VECTOR_DIM:-${QDRANT_VECTOR_DIM:-3072}}"

SMOKE_NAMESPACE="${NB_VECTOR_SMOKE_NAMESPACE:-phase6_smoke}"
SMOKE_ID="${NB_VECTOR_SMOKE_ID:-phase6-$(date +%s)-$$}"
GOCACHE_DIR="${GOCACHE:-/tmp/neurobridge_gocache}"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required for vector emulator smoke test" >&2
  exit 1
fi
if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required for vector emulator smoke test" >&2
  exit 1
fi
if ! command -v go >/dev/null 2>&1; then
  echo "go is required for vector emulator smoke test" >&2
  exit 1
fi
if [ ! -f "${COMPOSE_FILE}" ]; then
  echo "compose file not found: ${COMPOSE_FILE}" >&2
  exit 1
fi

echo "[smoke-vector] compose=${COMPOSE_FILE}"
echo "[smoke-vector] qdrant_url=${QDRANT_URL}"
echo "[smoke-vector] collection=${QDRANT_COLLECTION} namespace_prefix=${QDRANT_NAMESPACE_PREFIX} vector_dim=${QDRANT_VECTOR_DIM}"
echo "[smoke-vector] smoke_namespace=${SMOKE_NAMESPACE} smoke_id=${SMOKE_ID}"

echo "[smoke-vector] starting qdrant + init"
docker compose -f "${COMPOSE_FILE}" up -d qdrant qdrant-init

echo "[smoke-vector] waiting for qdrant readiness"
for i in $(seq 1 60); do
  if curl -fsS "${QDRANT_URL}/readyz" >/dev/null 2>&1; then
    break
  fi
  if [ "${i}" -eq 60 ]; then
    echo "qdrant not ready at ${QDRANT_URL}" >&2
    exit 1
  fi
  sleep 1
done

echo "[smoke-vector] app-path write/query via emulator-mode provider"
(
  cd "${BACKEND_ROOT}"
  NB_RUN_VECTOR_EMULATOR_SMOKE=true \
  NB_VECTOR_SMOKE_NAMESPACE="${SMOKE_NAMESPACE}" \
  NB_VECTOR_SMOKE_ID="${SMOKE_ID}" \
  QDRANT_URL="${QDRANT_URL}" \
  QDRANT_COLLECTION="${QDRANT_COLLECTION}" \
  QDRANT_NAMESPACE_PREFIX="${QDRANT_NAMESPACE_PREFIX}" \
  QDRANT_VECTOR_DIM="${QDRANT_VECTOR_DIM}" \
  GOCACHE="${GOCACHE_DIR}" \
    go test ./internal/app -run TestVectorProviderEmulatorModeSmokeUpsertAndQuery -count=1
)

echo "[smoke-vector] verify qdrant stored expected payload id"
QUALIFIED_NS="${QDRANT_NAMESPACE_PREFIX}:${SMOKE_NAMESPACE}"
SCROLL_BODY="$(cat <<EOF
{"filter":{"must":[{"key":"_nb_namespace","match":{"value":"${QUALIFIED_NS}"}},{"key":"_nb_vector_id","match":{"value":"${SMOKE_ID}"}}]},"limit":1,"with_payload":true,"with_vector":false}
EOF
)"
SCROLL_RESP="$(curl -fsS -X POST "${QDRANT_URL}/collections/${QDRANT_COLLECTION}/points/scroll" -H "Content-Type: application/json" -d "${SCROLL_BODY}")"
if ! printf "%s" "${SCROLL_RESP}" | grep -q "\"_nb_vector_id\":\"${SMOKE_ID}\""; then
  echo "qdrant scroll did not include expected _nb_vector_id=${SMOKE_ID}" >&2
  echo "response: ${SCROLL_RESP}" >&2
  exit 1
fi

echo "[smoke-vector] restart qdrant to validate named-volume persistence"
docker compose -f "${COMPOSE_FILE}" restart qdrant

echo "[smoke-vector] waiting for qdrant readiness after restart"
for i in $(seq 1 60); do
  if curl -fsS "${QDRANT_URL}/readyz" >/dev/null 2>&1; then
    break
  fi
  if [ "${i}" -eq 60 ]; then
    echo "qdrant not ready after restart at ${QDRANT_URL}" >&2
    exit 1
  fi
  sleep 1
done

echo "[smoke-vector] app-path query existing vector after restart"
(
  cd "${BACKEND_ROOT}"
  NB_RUN_VECTOR_EMULATOR_SMOKE=true \
  NB_VECTOR_SMOKE_NAMESPACE="${SMOKE_NAMESPACE}" \
  NB_VECTOR_SMOKE_ID="${SMOKE_ID}" \
  QDRANT_URL="${QDRANT_URL}" \
  QDRANT_COLLECTION="${QDRANT_COLLECTION}" \
  QDRANT_NAMESPACE_PREFIX="${QDRANT_NAMESPACE_PREFIX}" \
  QDRANT_VECTOR_DIM="${QDRANT_VECTOR_DIM}" \
  GOCACHE="${GOCACHE_DIR}" \
    go test ./internal/app -run TestVectorProviderEmulatorModeSmokeQueryExisting -count=1
)

echo "[smoke-vector] verify explicit unavailable behavior"
(
  cd "${BACKEND_ROOT}"
  GOCACHE="${GOCACHE_DIR}" \
    go test ./internal/app -run TestResolveVectorStoreProviderQdrantUnavailableIsExplicit -count=1
)

echo "[smoke-vector] cleanup smoke vector"
(
  cd "${BACKEND_ROOT}"
  NB_RUN_VECTOR_EMULATOR_SMOKE=true \
  NB_VECTOR_SMOKE_NAMESPACE="${SMOKE_NAMESPACE}" \
  NB_VECTOR_SMOKE_ID="${SMOKE_ID}" \
  QDRANT_URL="${QDRANT_URL}" \
  QDRANT_COLLECTION="${QDRANT_COLLECTION}" \
  QDRANT_NAMESPACE_PREFIX="${QDRANT_NAMESPACE_PREFIX}" \
  QDRANT_VECTOR_DIM="${QDRANT_VECTOR_DIM}" \
  GOCACHE="${GOCACHE_DIR}" \
    go test ./internal/app -run TestVectorProviderEmulatorModeSmokeCleanup -count=1
)

echo "[smoke-vector] completed successfully"

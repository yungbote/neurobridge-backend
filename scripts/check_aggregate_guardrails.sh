#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

failed=0

echo "[aggregate-guard] checking composition root for top-level Repos.Aggregates bucket..."
if rg -n -U -P '(?m)type Repos struct \{(?s:.*?)^\s*Aggregates\s' "internal/app/repos.go"; then
  echo "[aggregate-guard] found forbidden top-level aggregate bucket. Place aggregates inside existing domain groups."
  failed=1
fi

echo "[aggregate-guard] checking aggregate-consumer files for direct table-repo subpackage imports..."
AGGREGATE_CONSUMER_FILES=(
  "internal/services/saga_service.go"
  "internal/modules/chat/steps/respond.go"
  "internal/jobs/pipeline/chat_respond/pipeline.go"
)
TABLE_REPO_SUBPACKAGE_IMPORT='github\.com/yungbote/neurobridge-backend/internal/data/repos/[a-z0-9_]+'
if rg -n -P "$TABLE_REPO_SUBPACKAGE_IMPORT" "${AGGREGATE_CONSUMER_FILES[@]}"; then
  echo "[aggregate-guard] found forbidden table-repo subpackage import in aggregate-consumer files."
  echo "[aggregate-guard] use grouped app repos and aggregate contracts for invariant-critical writes."
  failed=1
fi

echo "[aggregate-guard] checking migrated saga flow does not bypass aggregate..."
if ! rg -n -F 's.aggregate.TransitionStatus(' "internal/services/saga_service.go" >/dev/null; then
  echo "[aggregate-guard] missing aggregate transition call in saga_service.go."
  failed=1
fi
if rg -n -F 's.runs.UpdateFields(' "internal/services/saga_service.go"; then
  echo "[aggregate-guard] found forbidden direct saga status writes via SagaRunRepo in migrated flow."
  failed=1
fi

echo "[aggregate-guard] checking migrated chat failure flow uses Thread aggregate..."
if ! rg -n -F 'deps.ThreadAgg.MarkTurnFailed(' "internal/modules/chat/steps/respond.go" >/dev/null; then
  echo "[aggregate-guard] missing ThreadAgg.MarkTurnFailed call in chat respond flow."
  failed=1
fi
if rg -n -F 'MessageStatusError' "internal/modules/chat/steps/respond.go"; then
  echo "[aggregate-guard] found forbidden direct assistant error status write in migrated chat flow."
  failed=1
fi
if rg -n -P '"status"\s*:\s*"failed"' "internal/modules/chat/steps/respond.go"; then
  echo "[aggregate-guard] found forbidden direct turn failed status write in migrated chat flow."
  failed=1
fi

if [ "$failed" -ne 0 ]; then
  echo "[aggregate-guard] FAILED"
  exit 1
fi

echo "[aggregate-guard] OK"

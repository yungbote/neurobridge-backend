#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

FLAT_REPOS_PATTERN='\brepos\.[A-Z][A-Za-z0-9_]*\b(?!\.)'
FLAT_APP_REPOS_PATTERN='\b(?:a|application)\.Repos\.[A-Z][A-Za-z0-9_]*\b(?!\.)'

APP_WIRING_FILES=(
  "internal/app/app.go"
  "internal/app/http.go"
  "internal/app/services.go"
)

CMD_DIR="cmd"

failed=0

echo "[repo-boundary-check] checking app wiring for flat repo access..."
if rg -n -P "$FLAT_REPOS_PATTERN" "${APP_WIRING_FILES[@]}"; then
  echo "[repo-boundary-check] found forbidden flat app-wiring access (repos.<Entity>). Use repos.<Domain>.<Entity>."
  failed=1
fi

echo "[repo-boundary-check] checking app state access for flat Repos fields..."
if rg -n -P "$FLAT_APP_REPOS_PATTERN" "${APP_WIRING_FILES[@]}"; then
  echo "[repo-boundary-check] found forbidden flat app repo access (a.Repos.<Entity>). Use a.Repos.<Domain>.<Entity>."
  failed=1
fi

echo "[repo-boundary-check] checking commands for flat app repo access..."
if [ -d "$CMD_DIR" ] && rg -n -P "$FLAT_APP_REPOS_PATTERN" "$CMD_DIR"; then
  echo "[repo-boundary-check] found forbidden flat command repo access (application.Repos.<Entity>). Use grouped access."
  failed=1
fi

if [ "$failed" -ne 0 ]; then
  echo "[repo-boundary-check] FAILED"
  exit 1
fi

echo "[repo-boundary-check] OK"

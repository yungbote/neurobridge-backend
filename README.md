# Neurobridge backend

Go API + worker service, plus an inference gateway binary.

## Docs
- Backend docs index: `docs/backend/README.md`
- Abstracted overview: `docs/backend/abstracted-overview.md`
- Developer guide: `docs/backend/developer.md`
- Module walkthrough: `docs/backend/module-walkthrough.md`
- API endpoint index: `docs/backend/api.md`
- Operations runbook: `docs/backend/operations.md`
- End-to-end pipeline: `docs/file-uploads-to-path-generation.md`

## Run locally
Recommended: use Docker Compose from `neurobridge-infra/local/`:
- `cd ../neurobridge-infra/local`
- `docker compose up --build`

## Run directly
Requires a Go toolchain compatible with `go.mod`.

API server:
- `RUN_SERVER=true RUN_WORKER=false RUN_MIGRATIONS=true go run ./cmd`

Worker:
- `RUN_SERVER=false RUN_WORKER=true RUN_MIGRATIONS=false go run ./cmd`

Inference gateway:
- `GOCACHE=$(pwd)/.gocache go run ./cmd/inference`

## Configuration
See `neurobridge-infra/local/.env.example` for the full env var list.

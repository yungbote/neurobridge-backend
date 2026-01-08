# Neurobridge backend

Go API + worker service.

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

## Configuration
See `neurobridge-infra/local/.env.example` for the full env var list.


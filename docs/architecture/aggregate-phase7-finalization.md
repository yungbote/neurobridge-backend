# Aggregate Phase 7 Finalization

Date: 2026-02-13

## Scope
- Final hardening, metrics capture, and cleanup for aggregate-repo migration phases.
- Focus domains: `Learning`, `Paths`, `Library`, `Chat`, `DocGen`, `Jobs`.

## Reproducible Metrics Commands
Run from `neurobridge-backend`:

```bash
go run ./scripts/aggregate_phase7_metrics.go ./ | jq '{
  service_layer_targeted_repo_write_callsites,
  service_methods_coordinating_2plus_targeted_repos,
  invariant_write_flows_aggregate_owned_tx_callsites,
  targeted_service_methods_with_any_repo_writes,
  targeted_service_methods_with_aggregate_writes
}'
```

```bash
rg -n --glob '!**/*_test.go' \
  "s\\.aggregate\\.AppendAction\\(|s\\.aggregate\\.TransitionStatus\\(|deps\\.ThreadAgg\\.MarkTurnFailed\\(" \
  internal/services internal/modules internal/jobs
```

## Final Before/After Metrics

| Metric | Before | After | Source |
| --- | ---: | ---: | --- |
| Direct table-repo write callsites from service layer (targeted domains) | 27 | 23 | After from `scripts/aggregate_phase7_metrics.go`; before derived from recorded removals: Phase 4 (`SagaService.AppendAction` removed 3 orchestration calls) + Phase 6 (`SagaService.MarkSagaStatus` removed 1 direct `SagaRunRepo` write) |
| Service methods coordinating `2+` targeted table repos | 3 | 2 | Before from Phase 3 detection table (`Chat.SendMessage`, `Workflow.UploadMaterialsAndStartLearningBuildWithChat`, `Saga.AppendAction`); after from `scripts/aggregate_phase7_metrics.go` |
| Invariant write flows with aggregate-owned tx boundary | 0 | 3 | Before (pre-migration) none. After validated by production callsites: `s.aggregate.AppendAction`, `s.aggregate.TransitionStatus`, `deps.ThreadAgg.MarkTurnFailed` |

## Fault-Injection Validation
The following tests validate rollback and conflict behavior for migrated aggregates:

```bash
GOCACHE=/tmp/neurobridge_gocache go test ./internal/data/aggregates -run 'TestThreadAggregateMarkTurnFailedRollbackOnInjectedFailure|TestThreadAggregateMarkTurnFailedConcurrentConflict|TestSagaAggregateAppendActionRollbackOnInjectedFailure|TestSagaAggregateTransitionStatusConcurrentConflict' -count=1
```

Status: pass

## Observability Verification
Aggregate telemetry metrics are present and emitted via `internal/data/aggregates/hooks.go` -> `internal/observability/metrics.go`:

- `nb_aggregate_operations_total{operation,status}`
- `nb_aggregate_operation_duration_seconds{operation,status}`
- `nb_aggregate_conflicts_total{operation}`
- `nb_aggregate_retries_total{operation}`

`status` labels now include aggregate error semantics (`success`, `validation`, `invariant_violation`, `conflict`, `retryable`, `internal`, etc.), allowing direct invariant-violation rate measurement.

Example PromQL:

```promql
sum(rate(nb_aggregate_operations_total{status="invariant_violation"}[5m]))
/
sum(rate(nb_aggregate_operations_total[5m]))
```

```promql
sum(rate(nb_aggregate_conflicts_total[5m]))
/
sum(rate(nb_aggregate_operations_total[5m]))
```

## Residual Direct Table-Repo Usage (Targeted Domains)
Captured from `scripts/aggregate_phase7_metrics.go` and labeled intentionally:

- `read_model`: none in this service-layer write inventory; read-model/projection access remains intentionally table-repo based in non-mutating paths.

| Method | Location | Label | Rationale |
| --- | --- | --- | --- |
| `chatService.CreateThread` | `internal/services/chat.go:398` | `simple_write` | Single-table create path (`chat_thread`) with low invariant pressure. |
| `chatService.SendMessage` | `internal/services/chat.go:644` | `legacy_deferred` | Multi-repo orchestration (`messages`, `threads`, `turns`) is still a high-value aggregate candidate for a future cycle. |
| `workflowService.UploadMaterialsAndStartLearningBuildWithChat` | `internal/services/workflow.go:69` | `legacy_deferred` | Cross-domain orchestration boundary; defer until domain-level aggregate boundaries are incrementally expanded. |
| `learningBuildBootstrapService.ensurePathInTx` | `internal/services/learning_build_bootstrap.go:72` | `legacy_deferred` | Candidate exists but ranked lower pain in previous detection cycle. |
| `sagaService.CreateOrGetSaga` | `internal/services/saga_service.go:77` | `simple_write` | Single aggregate-root row create/get path (`saga_run`) with local tx semantics acceptable. |
| `sagaService.Compensate` | `internal/services/saga_service.go:166` | `legacy_deferred` | Action status updates are still table-level and can be promoted in a later cycle if needed. |
| `jobService.Enqueue` | `internal/services/job_service.go:80` | `simple_write` | Simple `job_run` persistence; no cross-entity invariant coupling. |
| `jobService.Dispatch` | `internal/services/job_service.go:168` | `simple_write` | Single-job status transition within job orchestration service. |
| `jobService.CancelForRequestUser` | `internal/services/job_service.go:802` | `simple_write` | Single-entity mutation path. |
| `jobService.RestartForRequestUser` | `internal/services/job_service.go:901` | `simple_write` | Single-entity mutation path. |

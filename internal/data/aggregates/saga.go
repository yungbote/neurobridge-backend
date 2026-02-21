package aggregates

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	domainagg "github.com/yungbote/neurobridge-backend/internal/domain/aggregates"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"gorm.io/datatypes"
)

type SagaAggregateDeps struct {
	Base BaseDeps

	Runs    repos.SagaRunRepo
	Actions repos.SagaActionRepo
}

type sagaAggregate struct {
	deps SagaAggregateDeps
}

func NewSagaAggregate(deps SagaAggregateDeps) domainagg.SagaAggregate {
	deps.Base = deps.Base.withDefaults()
	return &sagaAggregate{deps: deps}
}

func (a *sagaAggregate) Contract() domainagg.Contract {
	return domainagg.SagaAggregateContract
}

func (a *sagaAggregate) AppendAction(ctx context.Context, in domainagg.AppendSagaActionInput) (domainagg.AppendSagaActionResult, error) {
	const op = "Jobs.Saga.AppendAction"
	var out domainagg.AppendSagaActionResult

	kind := normalizeSagaStatus(in.Kind)
	if in.SagaID == uuid.Nil {
		return out, domainagg.NewError(domainagg.CodeValidation, op, "missing saga_id", nil)
	}
	if kind == "" {
		return out, domainagg.NewError(domainagg.CodeValidation, op, "missing saga action kind", nil)
	}
	if a.deps.Runs == nil || a.deps.Actions == nil {
		return out, domainagg.NewError(domainagg.CodeInternal, op, "saga aggregate repos not configured", nil)
	}

	actionID := in.ActionID
	if actionID == uuid.Nil {
		actionID = uuid.New()
	}
	appendedAt := in.AppendedAt.UTC()
	if appendedAt.IsZero() {
		appendedAt = time.Now().UTC()
	}

	payload := normalizeSagaPayload(in.Payload)
	if !json.Valid(payload) {
		return out, domainagg.NewError(domainagg.CodeValidation, op, "payload must be valid JSON", nil)
	}

	err := executeWrite(ctx, a.deps.Base, op, func(dbc dbctx.Context) error {
		sr, err := a.deps.Runs.LockByID(dbc, in.SagaID)
		if err != nil {
			return err
		}
		if sr == nil || sr.ID == uuid.Nil {
			return domainagg.NewError(domainagg.CodeNotFound, op, fmt.Sprintf("saga_run not found: %s", in.SagaID.String()), nil)
		}

		current := normalizeSagaStatus(sr.Status)
		if !canAppendSagaAction(current) {
			return InvariantError(fmt.Sprintf("cannot append action when saga status is %q", current))
		}

		maxSeq, err := a.deps.Actions.GetMaxSeq(dbc, in.SagaID)
		if err != nil {
			return err
		}
		nextSeq := maxSeq + 1

		row := &types.SagaAction{
			ID:        actionID,
			SagaID:    in.SagaID,
			Seq:       nextSeq,
			Kind:      kind,
			Payload:   datatypes.JSON(payload),
			Status:    sagaActionStatusPending,
			CreatedAt: appendedAt,
			UpdatedAt: appendedAt,
		}
		if _, err := a.deps.Actions.Create(dbc, []*types.SagaAction{row}); err != nil {
			return err
		}

		out = domainagg.AppendSagaActionResult{
			SagaID:     row.SagaID,
			ActionID:   row.ID,
			Seq:        row.Seq,
			Status:     row.Status,
			AppendedAt: appendedAt,
		}
		return nil
	})
	return out, err
}

func (a *sagaAggregate) TransitionStatus(ctx context.Context, in domainagg.TransitionSagaStatusInput) (domainagg.TransitionSagaStatusResult, error) {
	const op = "Jobs.Saga.TransitionStatus"
	var out domainagg.TransitionSagaStatusResult
	if in.SagaID == uuid.Nil {
		return out, domainagg.NewError(domainagg.CodeValidation, op, "missing saga_id", nil)
	}
	if a.deps.Runs == nil {
		return out, domainagg.NewError(domainagg.CodeInternal, op, "saga run repo not configured", nil)
	}

	to := normalizeSagaStatus(in.ToStatus)
	if !isKnownSagaStatus(to) {
		return out, domainagg.NewError(domainagg.CodeValidation, op, "invalid target saga status", nil)
	}
	from := normalizeSagaStatus(in.FromStatus)
	transitionAt := in.TransitionAt.UTC()
	if transitionAt.IsZero() {
		transitionAt = time.Now().UTC()
	}

	err := executeWrite(ctx, a.deps.Base, op, func(dbc dbctx.Context) error {
		sr, err := a.deps.Runs.LockByID(dbc, in.SagaID)
		if err != nil {
			return err
		}
		if sr == nil || sr.ID == uuid.Nil {
			return domainagg.NewError(domainagg.CodeNotFound, op, fmt.Sprintf("saga_run not found: %s", in.SagaID.String()), nil)
		}

		current := normalizeSagaStatus(sr.Status)
		if from != "" && from != current {
			return ConflictError(fmt.Sprintf("saga status changed (expected=%s actual=%s)", from, current))
		}
		if current == to {
			out = domainagg.TransitionSagaStatusResult{
				SagaID:       sr.ID,
				Status:       current,
				TransitionAt: transitionAt,
			}
			return nil
		}
		if !isAllowedSagaTransition(current, to) {
			return InvariantError(fmt.Sprintf("invalid saga transition %s -> %s", current, to))
		}

		if err := a.deps.Runs.UpdateFields(dbc, sr.ID, map[string]interface{}{
			"status":     to,
			"updated_at": transitionAt,
		}); err != nil {
			return err
		}

		out = domainagg.TransitionSagaStatusResult{
			SagaID:       sr.ID,
			Status:       to,
			TransitionAt: transitionAt,
		}
		return nil
	})
	return out, err
}

const (
	sagaStatusRunning      = "running"
	sagaStatusSucceeded    = "succeeded"
	sagaStatusFailed       = "failed"
	sagaStatusCompensating = "compensating"
	sagaStatusCompensated  = "compensated"

	sagaActionStatusPending = "pending"
)

func normalizeSagaPayload(raw json.RawMessage) json.RawMessage {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

func normalizeSagaStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

func canAppendSagaAction(status string) bool {
	return status == sagaStatusRunning
}

func isKnownSagaStatus(status string) bool {
	switch status {
	case sagaStatusRunning, sagaStatusSucceeded, sagaStatusFailed, sagaStatusCompensating, sagaStatusCompensated:
		return true
	default:
		return false
	}
}

func isAllowedSagaTransition(from, to string) bool {
	from = normalizeSagaStatus(from)
	to = normalizeSagaStatus(to)
	switch from {
	case sagaStatusRunning:
		return to == sagaStatusSucceeded || to == sagaStatusFailed || to == sagaStatusCompensating
	case sagaStatusFailed:
		return to == sagaStatusCompensating
	case sagaStatusCompensating:
		return to == sagaStatusCompensated || to == sagaStatusFailed
	case sagaStatusSucceeded, sagaStatusCompensated:
		return false
	default:
		return false
	}
}

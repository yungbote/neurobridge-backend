package aggregates

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	domainagg "github.com/yungbote/neurobridge-backend/internal/domain/aggregates"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"gorm.io/datatypes"
)

type ThreadAggregateDeps struct {
	Base BaseDeps

	Threads     repos.ChatThreadRepo
	Messages    repos.ChatMessageRepo
	Turns       repos.ChatTurnRepo
	ThreadState repos.ChatThreadStateRepo
	Summary     repos.ChatSummaryNodeRepo
	Memory      repos.ChatMemoryItemRepo
	Entities    repos.ChatEntityRepo
	Edges       repos.ChatEdgeRepo
	Claims      repos.ChatClaimRepo
	Docs        repos.ChatDocRepo
}

type threadAggregate struct {
	deps ThreadAggregateDeps
}

func NewThreadAggregate(deps ThreadAggregateDeps) domainagg.ThreadAggregate {
	deps.Base = deps.Base.withDefaults()
	return &threadAggregate{deps: deps}
}

func (a *threadAggregate) Contract() domainagg.Contract {
	return domainagg.ThreadAggregateContract
}

func (a *threadAggregate) CommitTurn(ctx context.Context, in domainagg.CommitTurnInput) (domainagg.CommitTurnResult, error) {
	const op = "Chat.Thread.CommitTurn"
	var out domainagg.CommitTurnResult
	err := executeWrite(ctx, a.deps.Base, op, func(_ dbctx.Context) error {
		return notImplemented(op)
	})
	return out, err
}

func (a *threadAggregate) MarkTurnFailed(ctx context.Context, in domainagg.MarkTurnFailedInput) (domainagg.MarkTurnFailedResult, error) {
	const op = "Chat.Thread.MarkTurnFailed"
	var out domainagg.MarkTurnFailedResult
	if in.UserID == uuid.Nil {
		return out, domainagg.NewError(domainagg.CodeValidation, op, "missing user_id", nil)
	}
	if in.ThreadID == uuid.Nil {
		return out, domainagg.NewError(domainagg.CodeValidation, op, "missing thread_id", nil)
	}
	if in.TurnID == uuid.Nil {
		return out, domainagg.NewError(domainagg.CodeValidation, op, "missing turn_id", nil)
	}
	if a.deps.Threads == nil || a.deps.Turns == nil || a.deps.Messages == nil {
		return out, domainagg.NewError(domainagg.CodeInternal, op, "thread aggregate repos not configured", nil)
	}

	failedAt := in.FailedAt.UTC()
	if failedAt.IsZero() {
		failedAt = time.Now().UTC()
	}
	failureCode := strings.TrimSpace(in.FailureCode)
	if failureCode == "" {
		failureCode = "chat_respond_failed"
	}
	failureCause := strings.TrimSpace(in.FailureCause)

	err := executeWrite(ctx, a.deps.Base, op, func(dbc dbctx.Context) error {
		th, err := a.deps.Threads.LockByID(dbc, in.ThreadID)
		if err != nil {
			return err
		}
		if th == nil || th.ID == uuid.Nil || th.UserID != in.UserID {
			return domainagg.NewError(domainagg.CodeNotFound, op, fmt.Sprintf("thread not found: %s", in.ThreadID.String()), nil)
		}

		turn, err := a.deps.Turns.GetByID(dbc, in.UserID, in.TurnID)
		if err != nil {
			return err
		}
		if turn == nil || turn.ID == uuid.Nil {
			return domainagg.NewError(domainagg.CodeNotFound, op, fmt.Sprintf("turn not found: %s", in.TurnID.String()), nil)
		}
		if turn.ThreadID != in.ThreadID {
			return InvariantError("turn does not belong to thread")
		}

		status := normalizeThreadStatus(turn.Status)
		if status == threadTurnStatusError {
			return ConflictError("turn already failed")
		}
		if status != threadTurnStatusQueued && status != threadTurnStatusRunning {
			return InvariantError(fmt.Sprintf("cannot mark turn failed from status %q", status))
		}

		traceMeta := map[string]any{
			"failure_code":  failureCode,
			"failure_cause": failureCause,
		}
		if in.JobID != nil && *in.JobID != uuid.Nil {
			traceMeta["job_id"] = in.JobID.String()
		}
		if in.Metadata != nil {
			for k, v := range in.Metadata {
				traceMeta[k] = v
			}
		}
		traceJSON, _ := json.Marshal(traceMeta)

		updates := map[string]any{
			"status":          threadTurnStatusError,
			"completed_at":    failedAt,
			"retrieval_trace": datatypes.JSON(traceJSON),
		}
		if in.JobID != nil && *in.JobID != uuid.Nil {
			updates["job_id"] = *in.JobID
		}
		ok, err := a.deps.Base.CASGuard.UpdateByStatus(dbc, "chat_turn", turn.ID, []string{
			threadTurnStatusQueued,
			threadTurnStatusRunning,
		}, updates)
		if err != nil {
			return err
		}
		if err := RequireCASSuccess(ok, "chat turn changed while marking failed"); err != nil {
			return err
		}

		if turn.AssistantMessageID != uuid.Nil {
			msgMeta := map[string]any{
				"failure_code":  failureCode,
				"failure_cause": failureCause,
				"turn_id":       turn.ID.String(),
			}
			if in.Metadata != nil {
				for k, v := range in.Metadata {
					msgMeta[k] = v
				}
			}
			msgMetaJSON, _ := json.Marshal(msgMeta)
			if err := a.deps.Messages.UpdateFields(dbc, turn.AssistantMessageID, map[string]interface{}{
				"status":     threadMessageStatusError,
				"metadata":   datatypes.JSON(msgMetaJSON),
				"updated_at": failedAt,
			}); err != nil {
				return err
			}
		}

		out = domainagg.MarkTurnFailedResult{
			ThreadID:   in.ThreadID,
			TurnID:     in.TurnID,
			TurnStatus: threadTurnStatusError,
			RecordedAt: failedAt,
		}
		return nil
	})
	return out, err
}

const (
	threadTurnStatusQueued  = "queued"
	threadTurnStatusRunning = "running"
	threadTurnStatusError   = "error"

	threadMessageStatusError = "error"
)

func normalizeThreadStatus(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

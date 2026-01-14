package services

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type ChatService interface {
	CreateThread(dbc dbctx.Context, title string, pathID *uuid.UUID, jobID *uuid.UUID) (*types.ChatThread, error)
	ListThreads(dbc dbctx.Context, limit int) ([]*types.ChatThread, error)
	GetThread(dbc dbctx.Context, threadID uuid.UUID, limit int) (*types.ChatThread, []*types.ChatMessage, error)
	ListMessages(dbc dbctx.Context, threadID uuid.UUID, limit int, beforeSeq *int64) ([]*types.ChatMessage, error)
	// ListPendingIntakeQuestions returns:
	// - pending intake question prompts (path_intake_questions) whose linked path_intake job is still waiting_user
	// - active, non-blocking intake review prompts (path_intake_review) for in-flight builds
	//
	// This is used by the frontend to show durable notifications even if the realtime SSE message was missed.
	ListPendingIntakeQuestions(dbc dbctx.Context, limit int) ([]*types.ChatMessage, error)

	// SendMessage persists a user message, creates an assistant placeholder message, and enqueues a "chat_respond" job.
	SendMessage(dbc dbctx.Context, threadID uuid.UUID, content string, idempotencyKey string) (*types.ChatMessage, *types.ChatMessage, *types.JobRun, error)

	// RebuildThread enqueues a deterministic rebuild of derived chat artifacts (docs/summaries/graph/memory).
	RebuildThread(dbc dbctx.Context, threadID uuid.UUID) (*types.JobRun, error)

	// DeleteThread soft-deletes the thread/messages and enqueues a purge of derived artifacts + vectors.
	DeleteThread(dbc dbctx.Context, threadID uuid.UUID) (*types.JobRun, error)

	// UpdateMessage updates a message (user-only) and enqueues a rebuild to keep projections consistent.
	UpdateMessage(dbc dbctx.Context, threadID uuid.UUID, messageID uuid.UUID, content string) (*types.JobRun, error)

	// DeleteMessage soft-deletes a message (user-only) and enqueues a rebuild to keep projections consistent.
	DeleteMessage(dbc dbctx.Context, threadID uuid.UUID, messageID uuid.UUID) (*types.JobRun, error)

	// GetTurn fetches a single turn (includes retrieval_trace) for debugging.
	GetTurn(dbc dbctx.Context, turnID uuid.UUID) (*types.ChatTurn, error)
}

type chatService struct {
	db    *gorm.DB
	log   *logger.Logger
	paths repos.PathRepo

	jobRuns repos.JobRunRepo
	jobs    JobService

	threads  repos.ChatThreadRepo
	messages repos.ChatMessageRepo
	turns    repos.ChatTurnRepo
	notify   ChatNotifier
}

func NewChatService(
	db *gorm.DB,
	baseLog *logger.Logger,
	pathRepo repos.PathRepo,
	jobRunRepo repos.JobRunRepo,
	jobService JobService,
	threadRepo repos.ChatThreadRepo,
	messageRepo repos.ChatMessageRepo,
	turnRepo repos.ChatTurnRepo,
	notify ChatNotifier,
) ChatService {
	return &chatService{
		db:       db,
		log:      baseLog.With("service", "ChatService"),
		paths:    pathRepo,
		jobRuns:  jobRunRepo,
		jobs:     jobService,
		threads:  threadRepo,
		messages: messageRepo,
		turns:    turnRepo,
		notify:   notify,
	}
}

func (s *chatService) CreateThread(dbc dbctx.Context, title string, pathID *uuid.UUID, jobID *uuid.UUID) (*types.ChatThread, error) {
	rd := ctxutil.GetRequestData(dbc.Ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("not authenticated")
	}
	if s.threads == nil {
		return nil, fmt.Errorf("chat threads repo not wired")
	}

	title = strings.TrimSpace(title)
	if title == "" {
		title = "New chat"
	}

	transaction := dbc.Tx
	if transaction == nil {
		transaction = s.db
	}
	repoCtx := dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}

	if pathID != nil && *pathID != uuid.Nil && s.paths != nil {
		p, err := s.paths.GetByID(repoCtx, *pathID)
		if err != nil || p == nil || p.UserID == nil || *p.UserID != rd.UserID {
			return nil, fmt.Errorf("path not found")
		}
	}

	// Optional: link to an existing job (e.g., a path build job).
	if jobID != nil && *jobID != uuid.Nil && s.jobRuns != nil {
		rows, err := s.jobRuns.GetByIDs(repoCtx, []uuid.UUID{*jobID})
		if err != nil || len(rows) == 0 || rows[0] == nil || rows[0].OwnerUserID != rd.UserID {
			return nil, fmt.Errorf("job not found")
		}
	}

	now := time.Now().UTC()
	thread := &types.ChatThread{
		ID:            uuid.New(),
		UserID:        rd.UserID,
		PathID:        pathID,
		JobID:         jobID,
		Title:         title,
		Status:        "active",
		Metadata:      datatypes.JSON([]byte(`{}`)),
		NextSeq:       0,
		LastMessageAt: now,
		LastViewedAt:  now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	created, err := s.threads.Create(repoCtx, []*types.ChatThread{thread})
	if err != nil {
		return nil, err
	}
	if len(created) == 0 || created[0] == nil {
		return nil, fmt.Errorf("failed to create thread")
	}

	if s.notify != nil {
		s.notify.ThreadCreated(rd.UserID, created[0])
	}

	return created[0], nil
}

func (s *chatService) ListThreads(dbc dbctx.Context, limit int) ([]*types.ChatThread, error) {
	rd := ctxutil.GetRequestData(dbc.Ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("not authenticated")
	}
	if s.threads == nil {
		return nil, fmt.Errorf("chat threads repo not wired")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = s.db
	}
	repoCtx := dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}
	return s.threads.ListByUser(repoCtx, rd.UserID, limit)
}

func (s *chatService) GetThread(dbc dbctx.Context, threadID uuid.UUID, limit int) (*types.ChatThread, []*types.ChatMessage, error) {
	rd := ctxutil.GetRequestData(dbc.Ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, nil, fmt.Errorf("not authenticated")
	}
	if threadID == uuid.Nil {
		return nil, nil, fmt.Errorf("missing thread id")
	}
	if s.threads == nil || s.messages == nil {
		return nil, nil, fmt.Errorf("chat repos not wired")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = s.db
	}
	repoCtx := dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}

	rows, err := s.threads.GetByIDs(repoCtx, []uuid.UUID{threadID})
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 || rows[0] == nil || rows[0].UserID != rd.UserID {
		return nil, nil, fmt.Errorf("thread not found")
	}

	msgs, err := s.messages.ListByThread(repoCtx, threadID, limit)
	if err != nil {
		return nil, nil, err
	}
	return rows[0], msgs, nil
}

func (s *chatService) ListMessages(dbc dbctx.Context, threadID uuid.UUID, limit int, beforeSeq *int64) ([]*types.ChatMessage, error) {
	rd := ctxutil.GetRequestData(dbc.Ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("not authenticated")
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("missing thread id")
	}
	if s.threads == nil || s.messages == nil {
		return nil, fmt.Errorf("chat repos not wired")
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = s.db
	}
	repoCtx := dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}

	rows, err := s.threads.GetByIDs(repoCtx, []uuid.UUID{threadID})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0] == nil || rows[0].UserID != rd.UserID {
		return nil, fmt.Errorf("thread not found")
	}

	// Cursor-based paging by seq (monotonic).
	var msgs []*types.ChatMessage
	q := transaction.WithContext(dbc.Ctx).
		Model(&types.ChatMessage{}).
		Where("thread_id = ?", threadID)
	if beforeSeq != nil {
		q = q.Where("seq < ?", *beforeSeq)
	}
	if err := q.Order("seq DESC").Limit(limit).Find(&msgs).Error; err != nil {
		return nil, err
	}
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

func (s *chatService) ListPendingIntakeQuestions(dbc dbctx.Context, limit int) ([]*types.ChatMessage, error) {
	rd := ctxutil.GetRequestData(dbc.Ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("not authenticated")
	}
	if s.db == nil {
		return nil, fmt.Errorf("db missing")
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	transaction := dbc.Tx
	if transaction == nil {
		transaction = s.db
	}

	// Pending questions: intake question message whose linked path_intake job is still waiting_user.
	//
	// We join on metadata->>'job_id' (written by path_intake) so we don't surface stale intake prompts from completed builds.
	questions := []*types.ChatMessage{}
	if err := transaction.WithContext(dbc.Ctx).
		Table("chat_message AS m").
		Select("m.*").
		Joins("JOIN job_run AS j ON j.id::text = m.metadata->>'job_id'").
		Where("m.user_id = ? AND m.deleted_at IS NULL", rd.UserID).
		Where("m.metadata->>'kind' = ?", "path_intake_questions").
		Where("j.owner_user_id = ? AND j.job_type = ? AND j.status = ?", rd.UserID, "path_intake", "waiting_user").
		Order("m.created_at DESC").
		Limit(limit).
		Find(&questions).Error; err != nil {
		return nil, err
	}

	// Active review prompts: non-blocking "I’m proceeding; you can override" intake messages for in-flight builds.
	//
	// These are useful after refresh because SSE has no replay.
	//
	// We gate on the thread's root learning_build job still being in a non-terminal state to avoid surfacing stale
	// review prompts from long-finished builds.
	reviews := []*types.ChatMessage{}
	activeStatuses := []string{"queued", "running", "waiting_user"}
	if err := transaction.WithContext(dbc.Ctx).
		Table("chat_message AS m").
		Select("m.*").
		Joins("JOIN chat_thread AS t ON t.id = m.thread_id").
		Joins("JOIN job_run AS jb ON jb.id = t.job_id").
		Where("m.user_id = ? AND m.deleted_at IS NULL", rd.UserID).
		Where("m.metadata->>'kind' = ?", "path_intake_review").
		Where("jb.owner_user_id = ? AND jb.job_type = ? AND jb.status IN ?", rd.UserID, "learning_build", activeStatuses).
		Order("m.created_at DESC").
		Limit(limit).
		Find(&reviews).Error; err != nil {
		return nil, err
	}

	combined := make([]*types.ChatMessage, 0, len(questions)+len(reviews))
	combined = append(combined, questions...)
	combined = append(combined, reviews...)

	seen := map[uuid.UUID]bool{}
	dedup := make([]*types.ChatMessage, 0, len(combined))
	for _, m := range combined {
		if m == nil || m.ID == uuid.Nil {
			continue
		}
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		dedup = append(dedup, m)
	}

	sort.Slice(dedup, func(i, j int) bool {
		a, b := dedup[i], dedup[j]
		if a.CreatedAt.Equal(b.CreatedAt) {
			return a.ID.String() > b.ID.String()
		}
		return a.CreatedAt.After(b.CreatedAt)
	})
	if len(dedup) > limit {
		dedup = dedup[:limit]
	}
	return dedup, nil
}

func (s *chatService) SendMessage(dbc dbctx.Context, threadID uuid.UUID, content string, idempotencyKey string) (*types.ChatMessage, *types.ChatMessage, *types.JobRun, error) {
	rd := ctxutil.GetRequestData(dbc.Ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, nil, nil, fmt.Errorf("not authenticated")
	}
	if threadID == uuid.Nil {
		return nil, nil, nil, fmt.Errorf("missing thread id")
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, nil, nil, fmt.Errorf("missing content")
	}
	if len(content) > 20000 {
		return nil, nil, nil, fmt.Errorf("message too large")
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if len(idempotencyKey) > 200 {
		return nil, nil, nil, fmt.Errorf("idempotency key too long")
	}

	if s.threads == nil || s.messages == nil || s.jobs == nil || s.jobRuns == nil || s.turns == nil {
		return nil, nil, nil, fmt.Errorf("chat service not fully wired")
	}

	transaction := dbc.Tx
	if transaction == nil {
		transaction = s.db
	}
	repoCtx := dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}

	var (
		userMsg    *types.ChatMessage
		asstMsg    *types.ChatMessage
		job        *types.JobRun
		createdNew bool

		dispatchJobIDs []uuid.UUID
		cancelJobIDs   []uuid.UUID
		restartJobIDs  []uuid.UUID
	)

	// Fast-path idempotency (no lock): let clients safely retry while a response is still running.
	if idempotencyKey != "" {
		var existing types.ChatMessage
		err := transaction.WithContext(dbc.Ctx).
			Model(&types.ChatMessage{}).
			Where("thread_id = ? AND user_id = ? AND role = ? AND idempotency_key = ? AND deleted_at IS NULL",
				threadID, rd.UserID, "user", idempotencyKey,
			).
			First(&existing).Error
		if err == nil && existing.ID != uuid.Nil {
			userMsg = &existing

			var existingAsst types.ChatMessage
			_ = transaction.WithContext(dbc.Ctx).
				Model(&types.ChatMessage{}).
				Where("thread_id = ? AND user_id = ? AND seq = ? AND role = ? AND deleted_at IS NULL",
					threadID, rd.UserID, existing.Seq+1, "assistant",
				).
				First(&existingAsst).Error
			if existingAsst.ID != uuid.Nil {
				asstMsg = &existingAsst
			}

			turn, _ := s.turns.GetByUserMessageID(repoCtx, rd.UserID, threadID, existing.ID)
			if turn != nil && turn.JobID != nil && s.jobRuns != nil {
				rows, _ := s.jobRuns.GetByIDs(repoCtx, []uuid.UUID{*turn.JobID})
				if len(rows) > 0 && rows[0] != nil {
					job = rows[0]
				}
			}

			// If this thread is a paused path build, allow idempotent retries to still resume the build.
			if job == nil && s.jobRuns != nil && s.threads != nil {
				// Only auto-resume when the content looks like an actual intake answer.
				if isLikelyIntakeAnswerMessage(existing.Content) || isLikelyStructureSelectionMessage(existing.Content) {
					if resumed, _ := s.maybeResumePausedPathBuild(repoCtx, rd.UserID, threadID); resumed != nil {
						job = resumed
					}
				}
				// Best-effort: return the thread's linked build job for client context (even if already resumed).
				if job == nil {
					if thRows, err := s.threads.GetByIDs(repoCtx, []uuid.UUID{threadID}); err == nil && len(thRows) > 0 && thRows[0] != nil && thRows[0].UserID == rd.UserID {
						if thRows[0].JobID != nil && *thRows[0].JobID != uuid.Nil {
							if jr, err := s.jobRuns.GetByIDs(repoCtx, []uuid.UUID{*thRows[0].JobID}); err == nil && len(jr) > 0 && jr[0] != nil {
								job = jr[0]
							}
						}
					}
				}
			}
			return userMsg, asstMsg, job, nil
		}
		if err != nil && err != gorm.ErrRecordNotFound {
			return nil, nil, nil, err
		}
	}

	// For now, enforce a single runnable chat_respond per thread (keeps OpenAI conversation ordering sane).
	has, err := s.jobRuns.HasRunnableForEntity(repoCtx, rd.UserID, "chat_thread", threadID, "chat_respond")
	if err != nil {
		return nil, nil, nil, err
	}
	if has {
		return nil, nil, nil, fmt.Errorf("thread is busy")
	}

	err = transaction.WithContext(dbc.Ctx).Transaction(func(txx *gorm.DB) error {
		inner := dbctx.Context{Ctx: dbc.Ctx, Tx: txx}
		// Lock thread for concurrency-safe sequencing.
		th, err := s.threads.LockByID(inner, threadID)
		if err != nil {
			return err
		}
		if th == nil || th.ID == uuid.Nil || th.UserID != rd.UserID {
			return fmt.Errorf("thread not found")
		}

		// Idempotency: if the client retries the same message, return the existing turn.
		if idempotencyKey != "" {
			var existing types.ChatMessage
			err := txx.WithContext(dbc.Ctx).
				Model(&types.ChatMessage{}).
				Where("thread_id = ? AND user_id = ? AND role = ? AND idempotency_key = ? AND deleted_at IS NULL",
					threadID, rd.UserID, "user", idempotencyKey,
				).
				First(&existing).Error
			if err == nil && existing.ID != uuid.Nil {
				userMsg = &existing

				// Assistant placeholder is always seq+1.
				var existingAsst types.ChatMessage
				_ = txx.WithContext(dbc.Ctx).
					Model(&types.ChatMessage{}).
					Where("thread_id = ? AND user_id = ? AND seq = ? AND role = ? AND deleted_at IS NULL",
						threadID, rd.UserID, existing.Seq+1, "assistant",
					).
					First(&existingAsst).Error
				if existingAsst.ID != uuid.Nil {
					asstMsg = &existingAsst
				}

				turn, _ := s.turns.GetByUserMessageID(inner, rd.UserID, threadID, existing.ID)
				if turn != nil && turn.JobID != nil && s.jobRuns != nil {
					rows, _ := s.jobRuns.GetByIDs(inner, []uuid.UUID{*turn.JobID})
					if len(rows) > 0 && rows[0] != nil {
						job = rows[0]
					}
				}

				return nil
			}
			if err != nil && err != gorm.ErrRecordNotFound {
				return err
			}
		}

		// Re-check inside the thread lock to avoid races between concurrent requests.
		has, err := s.jobRuns.HasRunnableForEntity(inner, rd.UserID, "chat_thread", threadID, "chat_respond")
		if err != nil {
			return err
		}
		if has {
			return fmt.Errorf("thread is busy")
		}

		now := time.Now().UTC()

		// If this thread is attached to a paused learning_build (typically waiting for path_intake),
		// allow the user to *chat* about the decision without resuming the build immediately.
		//
		// We only auto-resume when the user's message looks like an actual intake answer (e.g. "#1", "#2",
		// "two tracks", "single path", "make reasonable assumptions", etc). Otherwise, we enqueue chat_respond
		// and keep the build paused.
		shouldResumePausedBuild := false
		pausedStage := ""
		if th.JobID != nil && *th.JobID != uuid.Nil && s.jobRuns != nil {
			if rows, err := s.jobRuns.GetByIDs(inner, []uuid.UUID{*th.JobID}); err == nil && len(rows) > 0 && rows[0] != nil {
				buildJob := rows[0]
				if buildJob.OwnerUserID == rd.UserID &&
					strings.EqualFold(strings.TrimSpace(buildJob.JobType), "learning_build") &&
					strings.EqualFold(strings.TrimSpace(buildJob.Status), "waiting_user") {
					pausedStage = pausedStageFromJobStage(buildJob.Stage)
					// Gate intake specifically; other waiting stages are resumed on any reply (legacy behavior).
					if pausedStage == "" || pausedStage == "path_intake" {
						// If the paused intake is waiting on an explicit structure choice, only resume on structure-selection messages.
						// This avoids resuming/stopping repeatedly when the user is discussing or answering non-structure details.
						if pausedIntakeRequiresStructureChoice(inner, buildJob) {
							if wf, werr := activeBlockingWorkflowForPausedIntake(inner, threadID, rd.UserID); werr == nil && wf != nil {
								_, ok := matchWorkflowV1Action(wf, content)
								shouldResumePausedBuild = ok
							} else {
								// Fallback (e.g., older prompts without workflow metadata).
								shouldResumePausedBuild = isLikelyStructureSelectionMessage(content)
							}
						} else {
							shouldResumePausedBuild = isLikelyIntakeAnswerMessage(content)
						}
					} else {
						shouldResumePausedBuild = true
					}
				}
			}
		}

		if shouldResumePausedBuild {
			if resumedJob, _ := s.maybeResumePausedPathBuild(inner, rd.UserID, threadID); resumedJob != nil {
				seqUser := th.NextSeq + 1
				userMsg = &types.ChatMessage{
					ID:             uuid.New(),
					ThreadID:       threadID,
					UserID:         rd.UserID,
					Seq:            seqUser,
					Role:           "user",
					Status:         "sent",
					Content:        content,
					Metadata:       datatypes.JSON([]byte(`{}`)),
					IdempotencyKey: idempotencyKey,
					CreatedAt:      now,
					UpdatedAt:      now,
				}
				if _, err := s.messages.Create(inner, []*types.ChatMessage{userMsg}); err != nil {
					return err
				}
				createdNew = true

				if err := s.threads.UpdateFields(inner, threadID, map[string]interface{}{
					"next_seq":        seqUser,
					"last_message_at": now,
					"last_viewed_at":  now,
					"updated_at":      now,
				}); err != nil {
					return err
				}

				job = resumedJob
				if job != nil && job.ID != uuid.Nil {
					dispatchJobIDs = append(dispatchJobIDs, job.ID)
				}
				asstMsg = nil
				return nil
			}
		}

		// Structure commands (non-blocking "soft proceed" overrides):
		// If the thread is tied to an in-flight (or recently completed) path build, allow the user to
		// override the structure via compact tokens ("1"/"2") or explicit commands ("undo split", "restore split").
		//
		// This is intentionally deterministic (no LLM call) so it is fast, cheap, and reliable.
		if handled, resp, cmdJob, cmdDispatch, cmdCancel, cmdRestart, err := s.maybeHandlePathStructureCommand(inner, th, rd.UserID, content); err != nil {
			return err
		} else if handled {
			seqUser := th.NextSeq + 1
			seqAsst := seqUser + 1
			now := time.Now().UTC()

			userMsg = &types.ChatMessage{
				ID:             uuid.New(),
				ThreadID:       threadID,
				UserID:         rd.UserID,
				Seq:            seqUser,
				Role:           "user",
				Status:         "sent",
				Content:        content,
				Metadata:       datatypes.JSON([]byte(`{}`)),
				IdempotencyKey: idempotencyKey,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			asstMetaJSON, _ := json.Marshal(map[string]any{
				"kind": "path_structure_command",
			})
			asstMsg = &types.ChatMessage{
				ID:        uuid.New(),
				ThreadID:  threadID,
				UserID:    rd.UserID,
				Seq:       seqAsst,
				Role:      "assistant",
				Status:    "sent",
				Content:   strings.TrimSpace(resp),
				Metadata:  datatypes.JSON(asstMetaJSON),
				CreatedAt: now,
				UpdatedAt: now,
			}
			if _, err := s.messages.Create(inner, []*types.ChatMessage{userMsg, asstMsg}); err != nil {
				return err
			}
			createdNew = true

			if err := s.threads.UpdateFields(inner, threadID, map[string]interface{}{
				"next_seq":        seqAsst,
				"last_message_at": now,
				"last_viewed_at":  now,
				"updated_at":      now,
			}); err != nil {
				return err
			}

			job = cmdJob
			if len(cmdDispatch) > 0 {
				dispatchJobIDs = append(dispatchJobIDs, cmdDispatch...)
			}
			if len(cmdCancel) > 0 {
				cancelJobIDs = append(cancelJobIDs, cmdCancel...)
			}
			if len(cmdRestart) > 0 {
				restartJobIDs = append(restartJobIDs, cmdRestart...)
			}
			return nil
		}

		turnID := uuid.New()
		seqUser := th.NextSeq + 1
		seqAsst := seqUser + 1

		userMsg = &types.ChatMessage{
			ID:             uuid.New(),
			ThreadID:       threadID,
			UserID:         rd.UserID,
			Seq:            seqUser,
			Role:           "user",
			Status:         "sent",
			Content:        content,
			Metadata:       datatypes.JSON([]byte(`{}`)),
			IdempotencyKey: idempotencyKey,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		asstMsg = &types.ChatMessage{
			ID:        uuid.New(),
			ThreadID:  threadID,
			UserID:    rd.UserID,
			Seq:       seqAsst,
			Role:      "assistant",
			Status:    "streaming",
			Content:   "",
			Metadata:  datatypes.JSON([]byte(`{}`)),
			CreatedAt: now,
			UpdatedAt: now,
		}

		if _, err := s.messages.Create(inner, []*types.ChatMessage{userMsg, asstMsg}); err != nil {
			return err
		}
		createdNew = true

		// Advance thread seq + timestamps.
		if err := s.threads.UpdateFields(inner, threadID, map[string]interface{}{
			"next_seq":        seqAsst,
			"last_message_at": now,
			"last_viewed_at":  now,
			"updated_at":      now,
		}); err != nil {
			return err
		}

		// Enqueue chat_respond job (worker does LLM + retrieval/memory maintenance).
		payload := map[string]any{
			"thread_id":            threadID.String(),
			"user_message_id":      userMsg.ID.String(),
			"assistant_message_id": asstMsg.ID.String(),
			"turn_id":              turnID.String(),
		}
		entityID := threadID
		enqueued, err := s.jobs.Enqueue(inner, rd.UserID, "chat_respond", "chat_thread", &entityID, payload)
		if err != nil {
			return err
		}
		job = enqueued
		if job != nil && job.ID != uuid.Nil {
			dispatchJobIDs = append(dispatchJobIDs, job.ID)
		}

		jobID := job.ID
		turn := &types.ChatTurn{
			ID:                 turnID,
			UserID:             rd.UserID,
			ThreadID:           threadID,
			UserMessageID:      userMsg.ID,
			AssistantMessageID: asstMsg.ID,
			JobID:              &jobID,
			Status:             "queued",
			Attempt:            0,
			RetrievalTrace:     datatypes.JSON([]byte(`{}`)),
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		if err := s.turns.Create(inner, turn); err != nil {
			return err
		}

		// Persist job_id into assistant metadata for client convenience (non-authoritative).
		meta := map[string]any{
			"job_id":  job.ID.String(),
			"turn_id": turnID.String(),
		}
		if b, err := json.Marshal(meta); err == nil {
			_ = s.messages.UpdateFields(inner, asstMsg.ID, map[string]interface{}{
				"metadata":   datatypes.JSON(b),
				"updated_at": now,
			})
			asstMsg.Metadata = datatypes.JSON(b)
		}

		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}

	// Notify after commit.
	if createdNew && s.notify != nil {
		meta := map[string]any{}
		if asstMsg != nil && len(asstMsg.Metadata) > 0 {
			_ = json.Unmarshal(asstMsg.Metadata, &meta)
		}
		if userMsg != nil {
			s.notify.MessageCreated(rd.UserID, threadID, userMsg, meta)
		}
		if asstMsg != nil {
			s.notify.MessageCreated(rd.UserID, threadID, asstMsg, meta)
		}
	}

	// Best-effort: apply any job control actions after commit (cancel/restart), then dispatch new work.
	if s.jobs != nil {
		seen := map[uuid.UUID]bool{}
		for _, jid := range cancelJobIDs {
			if jid == uuid.Nil || seen[jid] {
				continue
			}
			seen[jid] = true
			if _, err := s.jobs.CancelForRequestUser(dbctx.Context{Ctx: dbc.Ctx}, jid); err != nil && s.log != nil {
				s.log.Warn("Cancel job failed (continuing)", "job_id", jid.String(), "error", err)
			}
		}
		for _, jid := range restartJobIDs {
			if jid == uuid.Nil || seen[jid] {
				continue
			}
			seen[jid] = true
			if _, err := s.jobs.RestartForRequestUser(dbctx.Context{Ctx: dbc.Ctx}, jid); err != nil && s.log != nil {
				s.log.Warn("Restart job failed (continuing)", "job_id", jid.String(), "error", err)
			}
		}

		for _, jid := range dispatchJobIDs {
			if jid == uuid.Nil {
				continue
			}
			if err := s.jobs.Dispatch(dbctx.Context{Ctx: dbc.Ctx}, jid); err != nil {
				return userMsg, asstMsg, job, err
			}
		}
	}

	return userMsg, asstMsg, job, nil
}

type pausedBuildState struct {
	Stages map[string]struct {
		ChildJobID string `json:"child_job_id,omitempty"`
	} `json:"stages"`
}

func pausedStageFromJobStage(stage string) string {
	s := strings.ToLower(strings.TrimSpace(stage))
	if strings.HasPrefix(s, "waiting_user_") {
		return strings.TrimSpace(strings.TrimPrefix(s, "waiting_user_"))
	}
	return ""
}

func isLikelyIntakeAnswerMessage(content string) bool {
	s := strings.ToLower(strings.TrimSpace(content))
	if s == "" {
		return false
	}

	// Explicit "let the system decide" triggers.
	if strings.Contains(s, "make reasonable assumptions") {
		return true
	}
	if strings.HasPrefix(s, "/proceed") || strings.HasPrefix(s, "proceed") || strings.HasPrefix(s, "continue") || strings.HasPrefix(s, "go ahead") {
		return true
	}

	// Compact selection: "#1", "#2", "1", "2", "option 1", "option 2".
	trimmed := strings.TrimSpace(strings.TrimPrefix(s, "#"))
	if trimmed == "1" || trimmed == "2" {
		return true
	}
	if trimmed == "option 1" || trimmed == "option 2" || trimmed == "choice 1" || trimmed == "choice 2" {
		return true
	}
	// Common explicit overrides.
	if strings.Contains(s, "undo split") || strings.Contains(s, "undo the split") {
		return true
	}
	if strings.Contains(s, "restore split") || strings.Contains(s, "restore the split") {
		return true
	}

	// Allow track letters when the intake message presented "Track A / Track B".
	if trimmed == "a" || trimmed == "b" {
		return true
	}

	// If the user is asking a question, assume they want to discuss before resuming.
	// We only resume immediately on explicit selection tokens (handled above).
	if strings.Contains(s, "?") {
		return false
	}
	for _, prefix := range []string{
		"what ",
		"which ",
		"why ",
		"how ",
		"can you",
		"could you",
		"would you",
		"should ",
		"do you",
		"is it",
		"are you",
		"recommend",
		"help me decide",
	} {
		if strings.HasPrefix(s, prefix) {
			return false
		}
	}

	// Hedging without an explicit selection token usually means "let's talk it through" rather than resume.
	// (Explicit tokens like "1"/"2" are handled above.)
	if strings.Contains(s, "maybe") ||
		strings.Contains(s, "not sure") ||
		strings.Contains(s, "unsure") ||
		strings.Contains(s, "not certain") ||
		strings.Contains(s, "i'm unsure") ||
		strings.Contains(s, "i am unsure") {
		return false
	}

	// More natural selections.
	containsAll := func(hay string, needles ...string) bool {
		for _, n := range needles {
			if !strings.Contains(hay, n) {
				return false
			}
		}
		return true
	}
	containsAny := func(hay string, needles ...string) bool {
		for _, n := range needles {
			if strings.Contains(hay, n) {
				return true
			}
		}
		return false
	}

	// Structure selection signals.
	if containsAll(s, "single", "path") || containsAll(s, "combined", "path") {
		return true
	}
	if containsAny(s, "two tracks", "two subpaths", "separate tracks", "split into", "split this") && containsAny(s, "track", "subpath", "sub-path") {
		return true
	}
	// Explicit multi-track counts (e.g., "3 tracks", "three subpaths").
	for _, n := range []string{"2", "3", "4", "5", "6", "7", "8", "9", "10"} {
		if containsAny(s, n+" track", n+" tracks", n+" subpath", n+" subpaths", n+" sub-path", n+" sub-paths") {
			return true
		}
	}
	for _, n := range []string{"two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"} {
		if containsAny(s, n+" track", n+" tracks", n+" subpath", n+" subpaths", n+" sub-path", n+" sub-paths") {
			return true
		}
	}
	if containsAny(s, "program", "subpath", "sub-path") && containsAny(s, "track", "subpath", "sub-path") {
		return true
	}

	// Enumerated answers ("1) ... 2) ...").
	if strings.Contains(s, "1)") || strings.Contains(s, "2)") {
		return true
	}
	if strings.Contains(s, "1.") || strings.Contains(s, "2.") {
		return true
	}

	// Priority/level/deadline answers are also valid intake signals.
	if containsAny(s, "deadline", "no deadline", "asap", "this week", "this month", "intermediate", "beginner", "advanced") {
		return true
	}
	if containsAny(s, "go concurrency", "goroutines", "channels", "kubernetes networking", "services", "dns", "ingress") && containsAny(s, "priority", "focus", "i want", "i need") {
		return true
	}

	return false
}

func isLikelyStructureSelectionMessage(content string) bool {
	s := strings.ToLower(strings.TrimSpace(content))
	if s == "" {
		return false
	}

	containsAny := func(hay string, needles ...string) bool {
		for _, n := range needles {
			if strings.Contains(hay, n) {
				return true
			}
		}
		return false
	}

	// Short confirmations (used when the system asks to confirm a recommended structure).
	// Keep conservative: only match when it looks like an affirmation and not a question.
	{
		trim := strings.Trim(s, " \t\r\n,.;:!\"'")
		if trim != "" && !strings.Contains(trim, "?") {
			// Leading tokens that may be followed by extra details (e.g., "confirm. no deadline").
			// These appear when the intake prompt asks for an explicit reply token.
			hasLeadingToken := func(tok string) bool {
				tok = strings.TrimSpace(strings.ToLower(tok))
				if tok == "" {
					return false
				}
				if trim == tok {
					return true
				}
				if !strings.HasPrefix(trim, tok) {
					return false
				}
				// Word boundary: allow whitespace or punctuation immediately after the token.
				if len(trim) <= len(tok) {
					return true
				}
				next := trim[len(tok)]
				if next == ' ' || next == '\n' || next == '\t' {
					return true
				}
				switch next {
				case '.', ',', ';', ':', '!', ')', ']', '}':
					return true
				default:
					return false
				}
			}

			// Avoid resuming on "ok, but ..." or "ok what ..." type messages.
			for _, needle := range []string{
				"what ", "which ", "why ", "how ", "can you", "could you", "would you", "should ", "do you", "is it", "are you", "recommend", "help me",
			} {
				if strings.HasPrefix(trim, needle) || strings.Contains(trim, " "+needle) {
					goto skipShortConfirm
				}
			}

			// Explicit negative.
			if trim == "no" || strings.HasPrefix(trim, "no ") || strings.HasPrefix(trim, "nah") || strings.HasPrefix(trim, "nope") {
				goto skipShortConfirm
			}

			// Common reply tokens for hard outlier prompts.
			if hasLeadingToken("confirm") || hasLeadingToken("confirmed") {
				return true
			}

			switch trim {
			case "confirm", "confirmed", "yes", "y", "yeah", "yep", "ok", "okay", "sure", "sounds good", "looks good", "that works", "that's fine", "thats fine", "fine", "go ahead", "do it", "proceed", "continue":
				return true
			}
			// Common variants like "ok that's fine", "ok sounds good", "sure that works".
			for _, prefix := range []string{"ok", "okay", "sure", "yeah", "yep", "yes"} {
				if strings.HasPrefix(trim, prefix+" ") {
					rest := strings.TrimSpace(strings.TrimPrefix(trim, prefix))
					rest = strings.TrimLeft(rest, " \t\r\n,.;:!-")
					switch rest {
					case "confirm", "confirmed", "that's fine", "thats fine", "fine", "sounds good", "looks good", "that works", "works", "go ahead", "do it", "proceed", "continue":
						return true
					}
					if strings.HasPrefix(rest, "option 1") || strings.HasPrefix(rest, "option 2") || strings.HasPrefix(rest, "choice 1") || strings.HasPrefix(rest, "choice 2") {
						return true
					}
				}
			}

			// Accept "split/separate is fine/ok" as an explicit structural decision.
			// This helps when users confirm in plain language instead of using the exact token requested.
			if (strings.Contains(trim, "split") || strings.Contains(trim, "separate")) &&
				(containsAny(trim, " fine", " ok", " okay", " sure", " sounds good", " that works", " looks good", " go ahead", " proceed", " continue") ||
					strings.HasSuffix(trim, "fine") || strings.HasSuffix(trim, "ok") || strings.HasSuffix(trim, "okay")) {
				return true
			}
		}
	skipShortConfirm:
	}

	// Explicit "let the system decide" triggers.
	if strings.Contains(s, "make reasonable assumptions") {
		return true
	}
	if strings.Contains(s, "whatever you recommend") || strings.Contains(s, "you decide") || strings.Contains(s, "your call") || strings.Contains(s, "pick for me") {
		return true
	}
	if strings.HasPrefix(s, "/proceed") || strings.HasPrefix(s, "proceed") || strings.HasPrefix(s, "continue") || strings.HasPrefix(s, "go ahead") {
		return true
	}

	// Compact selection tokens.
	trimmed := strings.TrimSpace(strings.TrimPrefix(s, "#"))
	if trimmed == "1" || trimmed == "2" {
		return true
	}
	// Accept "1.", "2)", etc.
	if strings.HasPrefix(trimmed, "1") || strings.HasPrefix(trimmed, "2") {
		if len(trimmed) == 1 {
			return true
		}
		switch trimmed[1] {
		case '.', ')', ':', ',', ';', ' ', '\n', '\t':
			return true
		}
	}
	if strings.Contains(s, "option 1") || strings.Contains(s, "choice 1") || strings.Contains(s, "option 2") || strings.Contains(s, "choice 2") {
		return true
	}

	// Structure command parsing (covers "undo split", "restore split", "single path", "split into tracks", etc).
	if _, ok := parsePathStructureCommand(content); ok {
		return true
	}

	return false
}

func pausedIntakeRequiresStructureChoice(dbc dbctx.Context, buildJob *types.JobRun) bool {
	if dbc.Ctx == nil || dbc.Tx == nil || buildJob == nil || buildJob.ID == uuid.Nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(buildJob.JobType), "learning_build") {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(buildJob.Status), "waiting_user") {
		return false
	}

	pausedStage := pausedStageFromJobStage(buildJob.Stage)
	if pausedStage != "" && pausedStage != "path_intake" {
		return false
	}

	childJobID := uuid.Nil
	if len(buildJob.Result) > 0 && strings.TrimSpace(string(buildJob.Result)) != "" && strings.TrimSpace(string(buildJob.Result)) != "null" {
		var st pausedBuildState
		if err := json.Unmarshal(buildJob.Result, &st); err == nil && st.Stages != nil {
			if pausedStage != "" {
				if ss, ok := st.Stages[pausedStage]; ok {
					if id, err := uuid.Parse(strings.TrimSpace(ss.ChildJobID)); err == nil {
						childJobID = id
					}
				}
			}
			if childJobID == uuid.Nil {
				if ss, ok := st.Stages["path_intake"]; ok {
					if id, err := uuid.Parse(strings.TrimSpace(ss.ChildJobID)); err == nil {
						childJobID = id
					}
				}
			}
		}
	}

	// Fallback: locate the most recent waiting path_intake child job for this build's entity.
	if childJobID == uuid.Nil && buildJob.EntityID != nil && *buildJob.EntityID != uuid.Nil {
		var tmp types.JobRun
		_ = dbc.Tx.WithContext(dbc.Ctx).
			Model(&types.JobRun{}).
			Where("owner_user_id = ? AND job_type = ? AND entity_type = ? AND entity_id = ? AND status = ?",
				buildJob.OwnerUserID, "path_intake", buildJob.EntityType, *buildJob.EntityID, "waiting_user",
			).
			Order("created_at DESC").
			Limit(1).
			Find(&tmp).Error
		if tmp.ID != uuid.Nil {
			childJobID = tmp.ID
		}
	}

	if childJobID == uuid.Nil {
		return false
	}

	var child types.JobRun
	if err := dbc.Tx.WithContext(dbc.Ctx).
		Model(&types.JobRun{}).
		Where("id = ? AND owner_user_id = ? AND job_type = ? AND status = ?",
			childJobID, buildJob.OwnerUserID, "path_intake", "waiting_user",
		).
		First(&child).Error; err != nil {
		return false
	}

	if len(child.Result) == 0 || strings.TrimSpace(string(child.Result)) == "" || strings.TrimSpace(string(child.Result)) == "null" {
		return false
	}
	var res map[string]any
	if err := json.Unmarshal(child.Result, &res); err != nil || res == nil {
		return false
	}

	intake, ok := res["intake"].(map[string]any)
	if !ok || intake == nil {
		return false
	}

	ma, _ := intake["material_alignment"].(map[string]any)
	mode := strings.ToLower(strings.TrimSpace(fmt.Sprint(ma["mode"])))
	tracks, _ := intake["tracks"].([]any)
	multiGoal := mode == "multi_goal" || len(tracks) > 1
	if !multiGoal {
		return false
	}

	ps, _ := intake["path_structure"].(map[string]any)
	selected := strings.ToLower(strings.TrimSpace(fmt.Sprint(ps["selected_mode"])))
	return selected == "" || selected == "unspecified"
}

type pathStructureCommand struct {
	Mode string // "single_path" | "program_with_subpaths"
	// Token is true when the command is ambiguous without context (e.g. "1"/"2").
	Token bool
}

func parsePathStructureCommand(content string) (pathStructureCommand, bool) {
	out := pathStructureCommand{}
	s := strings.ToLower(strings.TrimSpace(content))
	if s == "" {
		return out, false
	}

	trimmed := strings.TrimSpace(strings.TrimPrefix(s, "#"))
	switch trimmed {
	case "1", "option 1", "choice 1":
		out.Mode = "single_path"
		out.Token = true
		return out, true
	case "2", "option 2", "choice 2":
		out.Mode = "program_with_subpaths"
		out.Token = true
		return out, true
	}

	if strings.Contains(s, "undo split") || strings.Contains(s, "undo the split") || strings.Contains(s, "keep together") {
		out.Mode = "single_path"
		return out, true
	}
	if strings.Contains(s, "restore split") || strings.Contains(s, "restore the split") || strings.Contains(s, "keep the split") {
		out.Mode = "program_with_subpaths"
		return out, true
	}

	// Natural language structure selection (keep conservative to avoid false positives).
	if strings.Contains(s, "single path") || strings.Contains(s, "one path") || strings.Contains(s, "combined path") {
		out.Mode = "single_path"
		return out, true
	}
	if strings.Contains(s, "program with subpaths") || strings.Contains(s, "separate tracks") || strings.Contains(s, "split into") && (strings.Contains(s, "track") || strings.Contains(s, "subpath") || strings.Contains(s, "sub-path")) {
		out.Mode = "program_with_subpaths"
		return out, true
	}

	return out, false
}

func jsonMapFromRaw(raw datatypes.JSON) map[string]any {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err == nil && m != nil {
		return m
	}
	return map[string]any{}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func uuidFromAny(v any) uuid.UUID {
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" {
		return uuid.Nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}

func setPathIntakeSelectedMode(pathMeta map[string]any, selectedMode string) bool {
	if pathMeta == nil {
		return false
	}
	intake, ok := pathMeta["intake"].(map[string]any)
	if !ok || intake == nil {
		return false
	}
	ps, ok := intake["path_structure"].(map[string]any)
	if !ok || ps == nil {
		return false
	}
	prev := strings.ToLower(strings.TrimSpace(fmt.Sprint(ps["selected_mode"])))
	next := strings.ToLower(strings.TrimSpace(selectedMode))
	if next == "" {
		return false
	}
	if prev == next {
		return false
	}
	ps["selected_mode"] = next
	// If the user explicitly chose a mode, it is no longer "unspecified".
	if strings.TrimSpace(next) != "" && strings.ToLower(strings.TrimSpace(fmt.Sprint(ps["selected_mode"]))) != "unspecified" {
		intake["needs_clarification"] = false
	}
	pathMeta["intake"] = intake
	return true
}

func (s *chatService) maybeHandlePathStructureCommand(
	dbc dbctx.Context,
	th *types.ChatThread,
	userID uuid.UUID,
	content string,
) (handled bool, response string, primaryJob *types.JobRun, dispatchIDs []uuid.UUID, cancelIDs []uuid.UUID, restartIDs []uuid.UUID, err error) {
	cmd, ok := parsePathStructureCommand(content)
	if !ok || cmd.Mode == "" {
		return false, "", nil, nil, nil, nil, nil
	}
	if s == nil || s.db == nil || s.jobs == nil || s.jobRuns == nil || s.paths == nil || s.threads == nil {
		return false, "", nil, nil, nil, nil, nil
	}
	if dbc.Ctx == nil || dbc.Tx == nil {
		return false, "", nil, nil, nil, nil, nil
	}
	if th == nil || th.ID == uuid.Nil || th.UserID != userID {
		return false, "", nil, nil, nil, nil, nil
	}
	if th.PathID == nil || *th.PathID == uuid.Nil {
		return false, "", nil, nil, nil, nil, nil
	}

	threadID := th.ID

	// Only apply structure overrides on path-build threads.
	threadMeta := jsonMapFromRaw(th.Metadata)
	if kind := strings.ToLower(strings.TrimSpace(fmt.Sprint(threadMeta["kind"]))); kind != "" && kind != "path_build" {
		return false, "", nil, nil, nil, nil, nil
	}

	// Identify the target path/material set from the latest intake prompt, if present.
	promptPathID := uuid.Nil
	promptSetID := uuid.Nil
	promptFound := false
	{
		var msg types.ChatMessage
		q := dbc.Tx.WithContext(dbc.Ctx).
			Model(&types.ChatMessage{}).
			Where("thread_id = ? AND user_id = ? AND deleted_at IS NULL", threadID, userID).
			Where("metadata->>'kind' IN ?", []string{"path_intake_questions", "path_intake_review"}).
			Order("seq DESC").
			Limit(1)
		if q.First(&msg).Error == nil && msg.ID != uuid.Nil {
			meta := jsonMapFromRaw(msg.Metadata)
			promptPathID = uuidFromAny(meta["path_id"])
			promptSetID = uuidFromAny(meta["material_set_id"])
			if promptPathID != uuid.Nil {
				promptFound = true
			}
		}
	}

	// Load the active thread path (source of truth for "current structure").
	activePathID := *th.PathID
	var active types.Path
	if err := dbc.Tx.WithContext(dbc.Ctx).
		Model(&types.Path{}).
		Where("id = ? AND user_id = ?", activePathID, userID).
		First(&active).Error; err != nil {
		// If we can't find a path to operate on, do not treat this as a command.
		return false, "", nil, nil, nil, nil, nil
	}

	activeMeta := jsonMapFromRaw(active.Metadata)
	activeSourceProgramID := uuidFromAny(activeMeta["structure_source_program_path_id"])
	if activeSourceProgramID == uuid.Nil {
		if v, ok := activeMeta["structure_variant"].(map[string]any); ok && v != nil {
			activeSourceProgramID = uuidFromAny(v["source_program_path_id"])
		}
	}

	// For token-only selections ("1"/"2"), require a recent intake prompt to avoid false positives.
	// (Explicit phrases like "undo split" are allowed without a prompt.)
	if cmd.Token && !promptFound && activeSourceProgramID == uuid.Nil && !strings.EqualFold(strings.TrimSpace(active.Kind), "program") {
		return false, "", nil, nil, nil, nil, nil
	}

	// No-op confirmations based on the active structure.
	if cmd.Mode == "single_path" && activeSourceProgramID != uuid.Nil {
		return true, "Already generating a single combined path. If you want to restore the split tracks, reply `2` or say “restore split”.", nil, nil, nil, nil, nil
	}
	if cmd.Mode == "program_with_subpaths" && strings.EqualFold(strings.TrimSpace(active.Kind), "program") && activeSourceProgramID == uuid.Nil {
		return true, "Already split into subpaths. If you want to revert to one combined path, reply `1` or say “undo split”.", nil, nil, nil, nil, nil
	}

	// Resolve which path we should operate on for this command.
	targetPathID := uuid.Nil
	switch cmd.Mode {
	case "program_with_subpaths":
		// If the user is currently in a combined-override view, restore from that.
		if activeSourceProgramID != uuid.Nil {
			targetPathID = activePathID
		}
	case "single_path":
		// If the active path is a program container, undo from it.
		if strings.EqualFold(strings.TrimSpace(active.Kind), "program") {
			targetPathID = activePathID
		}
	}
	if targetPathID == uuid.Nil {
		if promptPathID != uuid.Nil {
			targetPathID = promptPathID
		} else {
			targetPathID = activePathID
		}
	}

	// Load the target path (may differ from the active path).
	target := active
	if targetPathID != activePathID {
		var tmp types.Path
		if err := dbc.Tx.WithContext(dbc.Ctx).
			Model(&types.Path{}).
			Where("id = ? AND user_id = ?", targetPathID, userID).
			First(&tmp).Error; err == nil && tmp.ID != uuid.Nil {
			target = tmp
		}
	}

	sourceSetID := uuid.Nil
	if target.MaterialSetID != nil && *target.MaterialSetID != uuid.Nil {
		sourceSetID = *target.MaterialSetID
	} else if promptSetID != uuid.Nil {
		sourceSetID = promptSetID
	}
	if sourceSetID == uuid.Nil {
		return false, "", nil, nil, nil, nil, nil
	}

	targetMeta := jsonMapFromRaw(target.Metadata)
	sourceProgramID := uuidFromAny(targetMeta["structure_source_program_path_id"])
	if sourceProgramID == uuid.Nil {
		// Nested location (if stored as an object).
		if v, ok := targetMeta["structure_variant"].(map[string]any); ok && v != nil {
			sourceProgramID = uuidFromAny(v["source_program_path_id"])
		}
	}

	// Helpers.
	updateThreadPath := func(newPathID uuid.UUID, newJobID *uuid.UUID) error {
		threadMetaNext := threadMeta
		if threadMetaNext == nil {
			threadMetaNext = map[string]any{}
		}
		threadMetaNext["path_id"] = newPathID.String()
		threadMetaNext["material_set_id"] = sourceSetID.String()
		b, _ := json.Marshal(threadMetaNext)
		updates := map[string]interface{}{
			"path_id":    &newPathID,
			"metadata":   datatypes.JSON(b),
			"updated_at": time.Now().UTC(),
		}
		if newJobID != nil && *newJobID != uuid.Nil {
			updates["job_id"] = *newJobID
		}
		return s.threads.UpdateFields(dbc, threadID, updates)
	}

	archivePathTree := func(rootID uuid.UUID) ([]uuid.UUID, error) {
		type row struct {
			JobID *uuid.UUID `gorm:"column:job_id"`
		}
		var rows []row
		if err := dbc.Tx.WithContext(dbc.Ctx).
			Table("path").
			Select("job_id").
			Where("user_id = ? AND root_path_id = ? AND deleted_at IS NULL", userID, rootID).
			Find(&rows).Error; err != nil {
			return nil, err
		}
		ids := make([]uuid.UUID, 0, len(rows))
		seen := map[uuid.UUID]bool{}
		for _, r := range rows {
			if r.JobID == nil || *r.JobID == uuid.Nil {
				continue
			}
			if seen[*r.JobID] {
				continue
			}
			seen[*r.JobID] = true
			ids = append(ids, *r.JobID)
		}

		if err := dbc.Tx.WithContext(dbc.Ctx).
			Model(&types.Path{}).
			Where("user_id = ? AND root_path_id = ? AND deleted_at IS NULL", userID, rootID).
			Updates(map[string]interface{}{
				"status":     "archived",
				"updated_at": time.Now().UTC(),
			}).Error; err != nil {
			return ids, err
		}
		return ids, nil
	}

	unarchivePathTree := func(rootID uuid.UUID) error {
		now := time.Now().UTC()
		// Unarchive everything as draft, then mark the root container as ready if it is a program.
		if err := dbc.Tx.WithContext(dbc.Ctx).
			Model(&types.Path{}).
			Where("user_id = ? AND root_path_id = ? AND deleted_at IS NULL", userID, rootID).
			Updates(map[string]interface{}{
				"status":     "draft",
				"updated_at": now,
			}).Error; err != nil {
			return err
		}
		// If the root is a program container, it should render as ready.
		_ = dbc.Tx.WithContext(dbc.Ctx).
			Model(&types.Path{}).
			Where("id = ? AND user_id = ? AND deleted_at IS NULL", rootID, userID).
			Updates(map[string]interface{}{
				"status":     "ready",
				"updated_at": now,
			}).Error
		return nil
	}

	upsertUserLibraryIndexPath := func(materialSetID uuid.UUID, pathID uuid.UUID) error {
		now := time.Now().UTC()
		row := &types.UserLibraryIndex{
			ID:                uuid.New(),
			UserID:            userID,
			MaterialSetID:     materialSetID,
			PathID:            &pathID,
			Tags:              datatypes.JSON([]byte(`[]`)),
			ConceptClusterIDs: datatypes.JSON([]byte(`[]`)),
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		return dbc.Tx.WithContext(dbc.Ctx).
			Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "user_id"}, {Name: "material_set_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"path_id", "updated_at"}),
			}).
			Create(row).Error
	}

	// ----- Apply command -----
	switch cmd.Mode {
	case "single_path":
		// If we're already on a combined-override path, treat as a no-op confirmation.
		if sourceProgramID != uuid.Nil && target.ID == targetPathID {
			return true, "Already generating a single combined path. If you want to restore the split tracks, reply `2` or say “restore split”.", nil, nil, nil, nil, nil
		}

		// If this path is already a plain single path with no children, just lock the intake selection.
		children, _ := s.paths.ListByParentID(dbc, userID, target.ID)
		if !strings.EqualFold(strings.TrimSpace(target.Kind), "program") && len(children) == 0 {
			if changed := setPathIntakeSelectedMode(targetMeta, "single_path"); changed {
				_ = dbc.Tx.WithContext(dbc.Ctx).
					Model(&types.Path{}).
					Where("id = ? AND user_id = ?", target.ID, userID).
					Update("metadata", datatypes.JSON(mustJSON(targetMeta))).Error
			}
			return true, "Got it — keeping this as one combined path.", nil, nil, nil, nil, nil
		}

		// Undo split: archive the program tree and start a combined build as a stable "variant" path.
		programID := target.ID
		cancel, err := archivePathTree(programID)
		if err != nil {
			return true, "", nil, nil, nil, nil, err
		}
		cancelIDs = append(cancelIDs, cancel...)

		combinedID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("combined_path|"+programID.String()))
		now := time.Now().UTC()

		combinedMeta := map[string]any{}
		// Copy user-facing intake blobs if present (keeps generation aligned with the original analysis).
		for _, k := range []string{"intake", "intake_md", "intake_material_filter", "intake_updated_at"} {
			if v, ok := targetMeta[k]; ok {
				combinedMeta[k] = v
			}
		}
		// Critical: prevent path_structure_dispatch from re-splitting this combined override.
		_ = setPathIntakeSelectedMode(combinedMeta, "single_path")
		combinedMeta["intake_locked"] = true
		combinedMeta["structure_source_program_path_id"] = programID.String()
		combinedMeta["structure_variant"] = map[string]any{
			"kind":                   "combined_override",
			"source_program_path_id": programID.String(),
			"source_material_set_id": sourceSetID.String(),
			"created_at":             now.Format(time.RFC3339Nano),
		}

		// Create or update the combined path row.
		if err := dbc.Tx.WithContext(dbc.Ctx).
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&types.Path{
				ID:            combinedID,
				UserID:        &userID,
				MaterialSetID: &sourceSetID,
				ParentPathID:  nil,
				RootPathID:    &combinedID,
				Depth:         0,
				SortIndex:     0,
				Kind:          "path",
				Title:         strings.TrimSpace(target.Title),
				Description:   strings.TrimSpace(target.Description),
				Status:        "draft",
				JobID:         nil,
				Metadata:      datatypes.JSON(mustJSON(combinedMeta)),
				CreatedAt:     now,
				UpdatedAt:     now,
			}).Error; err != nil {
			return true, "", nil, nil, nil, nil, err
		}
		// Ensure the combined path reflects latest metadata/title.
		_ = s.paths.UpdateFields(dbc, combinedID, map[string]interface{}{
			"title":       strings.TrimSpace(target.Title),
			"description": strings.TrimSpace(target.Description),
			"status":      "draft",
			"metadata":    datatypes.JSON(mustJSON(combinedMeta)),
		})

		// Remap (user, source material_set) -> combined path for future calls.
		if err := upsertUserLibraryIndexPath(sourceSetID, combinedID); err != nil {
			return true, "", nil, nil, nil, nil, err
		}

		// Start a new combined build (idempotent: avoid enqueue if already runnable).
		if has, _ := s.jobRuns.HasRunnableForEntity(dbc, userID, "path", combinedID, "learning_build"); !has {
			payload := map[string]any{
				"material_set_id": sourceSetID.String(),
				"path_id":         combinedID.String(),
				"thread_id":       threadID.String(),
			}
			entityID := combinedID
			enq, err := s.jobs.Enqueue(dbc, userID, "learning_build", "path", &entityID, payload)
			if err != nil {
				return true, "", nil, nil, nil, nil, err
			}
			primaryJob = enq
			if enq != nil && enq.ID != uuid.Nil {
				dispatchIDs = append(dispatchIDs, enq.ID)
				_ = s.paths.UpdateFields(dbc, combinedID, map[string]interface{}{"job_id": enq.ID})
				_ = updateThreadPath(combinedID, &enq.ID)
			} else {
				_ = updateThreadPath(combinedID, nil)
			}
		} else {
			_ = updateThreadPath(combinedID, nil)
		}

		return true, "Got it — switching to one combined path. I paused the split tracks and started a combined build (you can say “restore split” any time).", primaryJob, dispatchIDs, cancelIDs, restartIDs, nil

	case "program_with_subpaths":
		// Restore split from a combined override.
		if sourceProgramID != uuid.Nil {
			programID := sourceProgramID
			// Unarchive the program tree.
			if err := unarchivePathTree(programID); err != nil {
				return true, "", nil, nil, nil, nil, err
			}

			// Archive the combined path and cancel its build job (best-effort).
			if target.JobID != nil && *target.JobID != uuid.Nil {
				cancelIDs = append(cancelIDs, *target.JobID)
			}
			_ = s.paths.UpdateFields(dbc, target.ID, map[string]interface{}{
				"status": "archived",
			})

			// Remap source set back to the program container.
			if err := upsertUserLibraryIndexPath(sourceSetID, programID); err != nil {
				return true, "", nil, nil, nil, nil, err
			}

			// Move thread context back to the program container.
			_ = updateThreadPath(programID, nil)

			// Ensure subpath builds are runnable again (restart canceled/failed; enqueue if missing).
			children, _ := s.paths.ListByParentID(dbc, userID, programID)
			jobIDs := make([]uuid.UUID, 0, len(children))
			for _, p := range children {
				if p == nil || p.ID == uuid.Nil {
					continue
				}
				if p.JobID != nil && *p.JobID != uuid.Nil {
					jobIDs = append(jobIDs, *p.JobID)
				}
			}
			statusByID := map[uuid.UUID]string{}
			if len(jobIDs) > 0 {
				type jr struct {
					ID     uuid.UUID `gorm:"column:id"`
					Status string    `gorm:"column:status"`
				}
				var rows []jr
				_ = dbc.Tx.WithContext(dbc.Ctx).
					Table("job_run").
					Select("id, status").
					Where("id IN ?", jobIDs).
					Find(&rows).Error
				for _, r := range rows {
					statusByID[r.ID] = strings.ToLower(strings.TrimSpace(r.Status))
				}
			}
			for _, p := range children {
				if p == nil || p.ID == uuid.Nil {
					continue
				}
				// Skip archived children (we only restore direct subpaths).
				if strings.EqualFold(strings.TrimSpace(p.Status), "archived") {
					continue
				}
				if p.JobID != nil && *p.JobID != uuid.Nil {
					st := statusByID[*p.JobID]
					if st == "canceled" || st == "failed" {
						restartIDs = append(restartIDs, *p.JobID)
						continue
					}
					if st == "queued" || st == "running" || st == "waiting_user" {
						continue
					}
				}

				// Enqueue a new build if nothing runnable is attached.
				if has, _ := s.jobRuns.HasRunnableForEntity(dbc, userID, "path", p.ID, "learning_build"); has {
					continue
				}
				if p.MaterialSetID == nil || *p.MaterialSetID == uuid.Nil {
					continue
				}
				payload := map[string]any{
					"material_set_id": p.MaterialSetID.String(),
					"path_id":         p.ID.String(),
					"thread_id":       threadID.String(),
				}
				entityID := p.ID
				enq, err := s.jobs.Enqueue(dbc, userID, "learning_build", "path", &entityID, payload)
				if err != nil {
					return true, "", nil, nil, nil, nil, err
				}
				if enq != nil && enq.ID != uuid.Nil {
					dispatchIDs = append(dispatchIDs, enq.ID)
					_ = s.paths.UpdateFields(dbc, p.ID, map[string]interface{}{"job_id": enq.ID})
					if primaryJob == nil {
						primaryJob = enq
					}
				}
			}

			return true, "Got it — restoring the split tracks. I paused the combined build and resumed the program subpaths.", primaryJob, dispatchIDs, cancelIDs, restartIDs, nil
		}

		// If the path is already a program container, treat as confirmation.
		if strings.EqualFold(strings.TrimSpace(target.Kind), "program") {
			return true, "Already split into subpaths. If you want to revert to one combined path, reply `1` or say “undo split”.", nil, nil, nil, nil, nil
		}

		// Request split: set selected_mode and enqueue a dispatch job to create subpaths.
		if changed := setPathIntakeSelectedMode(targetMeta, "program_with_subpaths"); changed {
			_ = s.paths.UpdateFields(dbc, target.ID, map[string]interface{}{
				"metadata": datatypes.JSON(mustJSON(targetMeta)),
			})
		}
		if has, _ := s.jobRuns.HasRunnableForEntity(dbc, userID, "path", target.ID, "path_structure_dispatch"); !has {
			payload := map[string]any{
				"material_set_id": sourceSetID.String(),
				"path_id":         target.ID.String(),
				"thread_id":       threadID.String(),
			}
			entityID := target.ID
			enq, err := s.jobs.Enqueue(dbc, userID, "path_structure_dispatch", "path", &entityID, payload)
			if err != nil {
				return true, "", nil, nil, nil, nil, err
			}
			if enq != nil && enq.ID != uuid.Nil {
				dispatchIDs = append(dispatchIDs, enq.ID)
				primaryJob = enq
			}
		}
		return true, "Got it — splitting into subpaths now. I’m creating tracks and starting one build per track.", primaryJob, dispatchIDs, cancelIDs, restartIDs, nil
	}

	return false, "", nil, nil, nil, nil, nil
}

func (s *chatService) maybeResumePausedPathBuild(dbc dbctx.Context, userID uuid.UUID, threadID uuid.UUID) (*types.JobRun, error) {
	if s == nil || s.jobRuns == nil || s.threads == nil || s.db == nil {
		return nil, nil
	}
	if userID == uuid.Nil || threadID == uuid.Nil {
		return nil, nil
	}

	transaction := dbc.Tx
	if transaction == nil {
		transaction = s.db
	}

	threads, err := s.threads.GetByIDs(dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}, []uuid.UUID{threadID})
	if err != nil {
		return nil, err
	}
	if len(threads) == 0 || threads[0] == nil || threads[0].UserID != userID {
		return nil, nil
	}
	th := threads[0]
	if th.JobID == nil || *th.JobID == uuid.Nil {
		return nil, nil
	}

	rows, err := s.jobRuns.GetByIDs(dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}, []uuid.UUID{*th.JobID})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0] == nil {
		return nil, nil
	}
	buildJob := rows[0]
	if !strings.EqualFold(strings.TrimSpace(buildJob.JobType), "learning_build") {
		return nil, nil
	}
	if !strings.EqualFold(strings.TrimSpace(buildJob.Status), "waiting_user") {
		return nil, nil
	}

	pausedStage := pausedStageFromJobStage(buildJob.Stage)

	childJobID := uuid.Nil
	if len(buildJob.Result) > 0 && strings.TrimSpace(string(buildJob.Result)) != "" && strings.TrimSpace(string(buildJob.Result)) != "null" {
		var st pausedBuildState
		if err := json.Unmarshal(buildJob.Result, &st); err == nil && st.Stages != nil {
			// Prefer the stage that actually paused the parent job (e.g., waiting_user_path_intake, waiting_user_web_resources_seed).
			if pausedStage != "" {
				if ss, ok := st.Stages[pausedStage]; ok {
					if id, err := uuid.Parse(strings.TrimSpace(ss.ChildJobID)); err == nil {
						childJobID = id
					}
				}
			}
			// Back-compat: older builds only paused on intake.
			if childJobID == uuid.Nil {
				if ss, ok := st.Stages["path_intake"]; ok {
					if id, err := uuid.Parse(strings.TrimSpace(ss.ChildJobID)); err == nil {
						childJobID = id
					}
				}
			}
		}
	}
	if childJobID == uuid.Nil && buildJob.EntityID != nil && *buildJob.EntityID != uuid.Nil {
		// Fallback: find a waiting child job for this material_set entity.
		// Prefer the paused stage name, otherwise fall back to path_intake (legacy).
		targetJobType := pausedStage
		if targetJobType == "" {
			targetJobType = "path_intake"
		}

		var tmp types.JobRun
		_ = transaction.WithContext(dbc.Ctx).
			Model(&types.JobRun{}).
			Where("owner_user_id = ? AND job_type = ? AND entity_type = ? AND entity_id = ? AND status = ?",
				userID, targetJobType, buildJob.EntityType, *buildJob.EntityID, "waiting_user",
			).
			Order("created_at DESC").
			Limit(1).
			Find(&tmp).Error
		if tmp.ID != uuid.Nil {
			childJobID = tmp.ID
		}
	}
	// If we can't identify a paused child stage, do nothing (avoid resuming unrelated waiting jobs).
	if childJobID == uuid.Nil && pausedStage == "" {
		return nil, nil
	}

	now := time.Now().UTC()
	// Resume child first (if present), then resume the parent orchestrator.
	if childJobID != uuid.Nil {
		_ = transaction.WithContext(dbc.Ctx).
			Model(&types.JobRun{}).
			Where("id = ? AND status = ?", childJobID, "waiting_user").
			Updates(map[string]interface{}{
				"status":        "queued",
				"stage":         "queued",
				"message":       "Queued",
				"locked_at":     nil,
				"updated_at":    now,
				"heartbeat_at":  now,
				"error":         "",
				"last_error_at": nil,
			}).Error
	}

	if err := transaction.WithContext(dbc.Ctx).
		Model(&types.JobRun{}).
		Where("id = ? AND status = ?", buildJob.ID, "waiting_user").
		Updates(map[string]interface{}{
			"status":        "queued",
			"stage":         "queued",
			"message":       "Queued",
			"locked_at":     nil,
			"updated_at":    now,
			"heartbeat_at":  now,
			"error":         "",
			"last_error_at": nil,
		}).Error; err != nil {
		return nil, err
	}

	buildJob.Status = "queued"
	buildJob.Stage = "queued"
	buildJob.Message = "Queued"
	buildJob.Error = ""
	buildJob.LockedAt = nil
	buildJob.HeartbeatAt = &now
	buildJob.UpdatedAt = now
	return buildJob, nil
}

func (s *chatService) RebuildThread(dbc dbctx.Context, threadID uuid.UUID) (*types.JobRun, error) {
	rd := ctxutil.GetRequestData(dbc.Ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("not authenticated")
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("missing thread id")
	}
	if s.jobs == nil || s.jobRuns == nil || s.threads == nil {
		return nil, fmt.Errorf("chat service not fully wired")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = s.db
	}
	repoCtx := dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}

	threads, err := s.threads.GetByIDs(repoCtx, []uuid.UUID{threadID})
	if err != nil {
		return nil, err
	}
	if len(threads) == 0 || threads[0] == nil || threads[0].UserID != rd.UserID {
		return nil, fmt.Errorf("thread not found")
	}

	// Avoid rebuilding while a response is in-flight; let it retry later.
	if has, _ := s.jobRuns.HasRunnableForEntity(repoCtx, rd.UserID, "chat_thread", threadID, "chat_respond"); has {
		return nil, fmt.Errorf("thread is busy")
	}

	// Debounce rebuild jobs.
	if has, _ := s.jobRuns.HasRunnableForEntity(repoCtx, rd.UserID, "chat_thread", threadID, "chat_rebuild"); has {
		if latest, _ := s.jobRuns.GetLatestByEntity(repoCtx, rd.UserID, "chat_thread", threadID, "chat_rebuild"); latest != nil {
			return latest, nil
		}
		return nil, fmt.Errorf("rebuild already queued")
	}

	payload := map[string]any{"thread_id": threadID.String()}
	entityID := threadID
	return s.jobs.Enqueue(repoCtx, rd.UserID, "chat_rebuild", "chat_thread", &entityID, payload)
}

func (s *chatService) DeleteThread(dbc dbctx.Context, threadID uuid.UUID) (*types.JobRun, error) {
	rd := ctxutil.GetRequestData(dbc.Ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("not authenticated")
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("missing thread id")
	}
	if s.jobs == nil || s.jobRuns == nil || s.db == nil {
		return nil, fmt.Errorf("chat service not fully wired")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = s.db
	}
	repoCtx := dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}

	// Avoid deleting while a response is in-flight.
	if has, _ := s.jobRuns.HasRunnableForEntity(repoCtx, rd.UserID, "chat_thread", threadID, "chat_respond"); has {
		return nil, fmt.Errorf("thread is busy")
	}
	// Debounce purge jobs.
	if has, _ := s.jobRuns.HasRunnableForEntity(repoCtx, rd.UserID, "chat_thread", threadID, "chat_purge"); has {
		if latest, _ := s.jobRuns.GetLatestByEntity(repoCtx, rd.UserID, "chat_thread", threadID, "chat_purge"); latest != nil {
			return latest, nil
		}
		return nil, fmt.Errorf("purge already queued")
	}

	var job *types.JobRun
	err := transaction.WithContext(dbc.Ctx).Transaction(func(txx *gorm.DB) error {
		inner := dbctx.Context{Ctx: dbc.Ctx, Tx: txx}
		// Ensure thread exists + belongs to user.
		var thread types.ChatThread
		if err := txx.WithContext(dbc.Ctx).
			Model(&types.ChatThread{}).
			Where("id = ? AND user_id = ?", threadID, rd.UserID).
			First(&thread).Error; err != nil {
			return fmt.Errorf("thread not found")
		}

		// Soft-delete messages + thread (canonical).
		if err := txx.WithContext(dbc.Ctx).
			Where("thread_id = ? AND user_id = ?", threadID, rd.UserID).
			Delete(&types.ChatMessage{}).Error; err != nil {
			return err
		}
		if err := txx.WithContext(dbc.Ctx).
			Where("id = ? AND user_id = ?", threadID, rd.UserID).
			Delete(&types.ChatThread{}).Error; err != nil {
			return err
		}

		// Enqueue purge to remove derived artifacts + vector cache.
		payload := map[string]any{"thread_id": threadID.String()}
		entityID := threadID
		enq, err := s.jobs.Enqueue(inner, rd.UserID, "chat_purge", "chat_thread", &entityID, payload)
		if err != nil {
			return err
		}
		job = enq
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Dispatch the Temporal workflow only after the DB transaction commits.
	if job != nil && job.ID != uuid.Nil && s.jobs != nil {
		if err := s.jobs.Dispatch(dbctx.Context{Ctx: dbc.Ctx}, job.ID); err != nil {
			return job, err
		}
	}
	return job, nil
}

func (s *chatService) UpdateMessage(dbc dbctx.Context, threadID uuid.UUID, messageID uuid.UUID, content string) (*types.JobRun, error) {
	rd := ctxutil.GetRequestData(dbc.Ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("not authenticated")
	}
	if threadID == uuid.Nil || messageID == uuid.Nil {
		return nil, fmt.Errorf("missing ids")
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("missing content")
	}
	if len(content) > 20000 {
		return nil, fmt.Errorf("message too large")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = s.db
	}
	repoCtx := dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}

	// Only allow editing user messages.
	if err := transaction.WithContext(dbc.Ctx).
		Model(&types.ChatMessage{}).
		Where("id = ? AND thread_id = ? AND user_id = ? AND role = ?", messageID, threadID, rd.UserID, "user").
		Updates(map[string]interface{}{
			"content":    content,
			"updated_at": time.Now().UTC(),
		}).Error; err != nil {
		return nil, err
	}

	// Rebuild projections to ensure all derived artifacts are consistent.
	return s.RebuildThread(repoCtx, threadID)
}

func (s *chatService) DeleteMessage(dbc dbctx.Context, threadID uuid.UUID, messageID uuid.UUID) (*types.JobRun, error) {
	rd := ctxutil.GetRequestData(dbc.Ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("not authenticated")
	}
	if threadID == uuid.Nil || messageID == uuid.Nil {
		return nil, fmt.Errorf("missing ids")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = s.db
	}
	repoCtx := dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}

	// Only allow deleting user messages.
	if err := transaction.WithContext(dbc.Ctx).
		Where("id = ? AND thread_id = ? AND user_id = ? AND role = ?", messageID, threadID, rd.UserID, "user").
		Delete(&types.ChatMessage{}).Error; err != nil {
		return nil, err
	}

	return s.RebuildThread(repoCtx, threadID)
}

func (s *chatService) GetTurn(dbc dbctx.Context, turnID uuid.UUID) (*types.ChatTurn, error) {
	rd := ctxutil.GetRequestData(dbc.Ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("not authenticated")
	}
	if turnID == uuid.Nil {
		return nil, fmt.Errorf("missing turn id")
	}
	if s.turns == nil {
		return nil, fmt.Errorf("chat turns repo not wired")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = s.db
	}
	repoCtx := dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}
	return s.turns.GetByID(repoCtx, rd.UserID, turnID)
}

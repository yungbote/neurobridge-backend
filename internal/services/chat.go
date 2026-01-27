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
	waitingJobTypes := []string{"path_intake", "path_grouping_refine", "waitpoint_stage"}
	questions := []*types.ChatMessage{}
	if err := transaction.WithContext(dbc.Ctx).
		Table("chat_message AS m").
		Select("m.*").
		Joins("JOIN job_run AS j ON j.id::text = m.metadata->>'job_id'").
		Where("m.user_id = ? AND m.deleted_at IS NULL", rd.UserID).
		Where("m.metadata->>'kind' = ?", "path_intake_questions").
		Where("j.owner_user_id = ? AND j.job_type IN ? AND j.status = ?", rd.UserID, waitingJobTypes, "waiting_user").
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

			// Best-effort: return the thread's linked build job for client context.
			if job == nil && s.jobRuns != nil && s.threads != nil {
				if thRows, err := s.threads.GetByIDs(repoCtx, []uuid.UUID{threadID}); err == nil && len(thRows) > 0 && thRows[0] != nil && thRows[0].UserID == rd.UserID {
					if thRows[0].JobID != nil && *thRows[0].JobID != uuid.Nil {
						if jr, err := s.jobRuns.GetByIDs(repoCtx, []uuid.UUID{*thRows[0].JobID}); err == nil && len(jr) > 0 && jr[0] != nil {
							job = jr[0]
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

		// ──────────────────────────────────────────────────────────────────────────────
		// WAITPOINT INTEGRATION: If this thread is attached to a paused learning_build,
		// route the message through waitpoint_interpret instead of using old resume logic.
		// The interpreter classifies the message and decides whether to:
		// - continue chatting (enqueue chat_respond)
		// - ask for clarification (post an assistant message)
		// - confirm and resume (apply selection + resume jobs)
		// ──────────────────────────────────────────────────────────────────────────────
		if th.JobID != nil && *th.JobID != uuid.Nil && s.jobRuns != nil {
			if rows, err := s.jobRuns.GetByIDs(inner, []uuid.UUID{*th.JobID}); err == nil && len(rows) > 0 && rows[0] != nil {
				buildJob := rows[0]
				buildIsPaused := strings.EqualFold(strings.TrimSpace(buildJob.Status), "waiting_user")
				if !buildIsPaused {
					buildIsPaused = s.hasPausedWaitpointChild(inner, buildJob)
				}
				if buildJob.OwnerUserID == rd.UserID &&
					strings.EqualFold(strings.TrimSpace(buildJob.JobType), "learning_build") &&
					buildIsPaused {
					// Thread has a paused build - route through waitpoint_interpret.
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

					// Enqueue waitpoint_interpret to classify and handle the message.
					payload := map[string]any{
						"thread_id":       threadID.String(),
						"user_message_id": userMsg.ID.String(),
						"build_job_id":    buildJob.ID.String(),
					}
					entityID := threadID
					enqueued, err := s.jobs.Enqueue(inner, rd.UserID, "waitpoint_interpret", "chat_thread", &entityID, payload)
					if err != nil {
						return err
					}
					job = enqueued
					if job != nil && job.ID != uuid.Nil {
						dispatchJobIDs = append(dispatchJobIDs, job.ID)
					}
					asstMsg = nil
					return nil
				}
			}
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

// ──────────────────────────────────────────────────────────────────────────────
// VAULTED: Old heuristic-based resume logic has been removed.
// All pause/resume decisions now flow through the waitpoint_interpret pipeline,
// which uses an LLM classifier to determine user intent.
// ──────────────────────────────────────────────────────────────────────────────

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

// ──────────────────────────────────────────────────────────────────────────────
// VAULTED: maybeHandlePathStructureCommand and maybeResumePausedPathBuild
// have been removed. All pause/resume decisions now flow through the
// waitpoint_interpret pipeline, which uses an LLM classifier.
// ──────────────────────────────────────────────────────────────────────────────

// pathStructureCommand is a stub type for backwards compatibility.
// The actual structure command logic is now handled by waitpoint_interpret.
type pathStructureCommand struct {
	Mode  string
	Token bool
}

// parsePathStructureCommand is a stub that always returns false.
// Structure commands are now handled by the waitpoint_interpret pipeline.
func parsePathStructureCommand(content string) (pathStructureCommand, bool) {
	return pathStructureCommand{}, false
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

type orchestratorStateProbe struct {
	Stages map[string]orchestratorStageProbe `json:"stages,omitempty"`
}

type orchestratorStageProbe struct {
	ChildJobID   string `json:"child_job_id,omitempty"`
	ChildJobType string `json:"child_job_type,omitempty"`
}

func (s *chatService) hasPausedWaitpointChild(dbc dbctx.Context, build *types.JobRun) bool {
	if s == nil || s.jobRuns == nil || build == nil || build.ID == uuid.Nil {
		return false
	}
	candidates := waitpointChildCandidates(build.Result)
	if len(candidates) == 0 {
		return false
	}
	ids := make([]uuid.UUID, 0, len(candidates))
	for id := range candidates {
		if id != uuid.Nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return false
	}
	rows, err := s.jobRuns.GetByIDs(dbc, ids)
	if err != nil {
		return false
	}
	for _, row := range rows {
		if row == nil || row.ID == uuid.Nil {
			continue
		}
		if row.OwnerUserID != build.OwnerUserID {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(row.Status), "waiting_user") {
			return true
		}
	}
	return false
}

func waitpointChildCandidates(raw datatypes.JSON) map[uuid.UUID]string {
	out := map[uuid.UUID]string{}
	if len(raw) == 0 {
		return out
	}
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return out
	}
	var probe orchestratorStateProbe
	if err := json.Unmarshal(raw, &probe); err != nil || probe.Stages == nil {
		return out
	}
	for stageName, ss := range probe.Stages {
		if !isWaitpointStageName(stageName, ss) {
			continue
		}
		id, err := uuid.Parse(strings.TrimSpace(ss.ChildJobID))
		if err != nil || id == uuid.Nil {
			continue
		}
		out[id] = stageName
	}
	return out
}

func isWaitpointStageName(stageName string, ss orchestratorStageProbe) bool {
	name := strings.ToLower(strings.TrimSpace(stageName))
	if strings.HasSuffix(name, "_waitpoint") || strings.Contains(name, "waitpoint") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(ss.ChildJobType), "waitpoint_stage")
}

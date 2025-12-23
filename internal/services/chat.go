package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type ChatService interface {
	CreateThread(ctx context.Context, tx *gorm.DB, title string, pathID *uuid.UUID, jobID *uuid.UUID) (*types.ChatThread, error)
	ListThreads(ctx context.Context, tx *gorm.DB, limit int) ([]*types.ChatThread, error)
	GetThread(ctx context.Context, tx *gorm.DB, threadID uuid.UUID, limit int) (*types.ChatThread, []*types.ChatMessage, error)
	ListMessages(ctx context.Context, tx *gorm.DB, threadID uuid.UUID, limit int, beforeSeq *int64) ([]*types.ChatMessage, error)

	// SendMessage persists a user message, creates an assistant placeholder message, and enqueues a "chat_respond" job.
	SendMessage(ctx context.Context, tx *gorm.DB, threadID uuid.UUID, content string, idempotencyKey string) (*types.ChatMessage, *types.ChatMessage, *types.JobRun, error)

	// RebuildThread enqueues a deterministic rebuild of derived chat artifacts (docs/summaries/graph/memory).
	RebuildThread(ctx context.Context, tx *gorm.DB, threadID uuid.UUID) (*types.JobRun, error)

	// DeleteThread soft-deletes the thread/messages and enqueues a purge of derived artifacts + vectors.
	DeleteThread(ctx context.Context, tx *gorm.DB, threadID uuid.UUID) (*types.JobRun, error)

	// UpdateMessage updates a message (user-only) and enqueues a rebuild to keep projections consistent.
	UpdateMessage(ctx context.Context, tx *gorm.DB, threadID uuid.UUID, messageID uuid.UUID, content string) (*types.JobRun, error)

	// DeleteMessage soft-deletes a message (user-only) and enqueues a rebuild to keep projections consistent.
	DeleteMessage(ctx context.Context, tx *gorm.DB, threadID uuid.UUID, messageID uuid.UUID) (*types.JobRun, error)

	// GetTurn fetches a single turn (includes retrieval_trace) for debugging.
	GetTurn(ctx context.Context, tx *gorm.DB, turnID uuid.UUID) (*types.ChatTurn, error)
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

func (s *chatService) CreateThread(ctx context.Context, tx *gorm.DB, title string, pathID *uuid.UUID, jobID *uuid.UUID) (*types.ChatThread, error) {
	rd := ctxutil.GetRequestData(ctx)
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

	transaction := tx
	if transaction == nil {
		transaction = s.db
	}

	if pathID != nil && *pathID != uuid.Nil && s.paths != nil {
		p, err := s.paths.GetByID(ctx, transaction, *pathID)
		if err != nil || p == nil || p.UserID == nil || *p.UserID != rd.UserID {
			return nil, fmt.Errorf("path not found")
		}
	}

	// Optional: link to an existing job (e.g., a path build job).
	if jobID != nil && *jobID != uuid.Nil && s.jobRuns != nil {
		rows, err := s.jobRuns.GetByIDs(ctx, transaction, []uuid.UUID{*jobID})
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

	created, err := s.threads.Create(ctx, transaction, []*types.ChatThread{thread})
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

func (s *chatService) ListThreads(ctx context.Context, tx *gorm.DB, limit int) ([]*types.ChatThread, error) {
	rd := ctxutil.GetRequestData(ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("not authenticated")
	}
	if s.threads == nil {
		return nil, fmt.Errorf("chat threads repo not wired")
	}
	transaction := tx
	if transaction == nil {
		transaction = s.db
	}
	return s.threads.ListByUser(ctx, transaction, rd.UserID, limit)
}

func (s *chatService) GetThread(ctx context.Context, tx *gorm.DB, threadID uuid.UUID, limit int) (*types.ChatThread, []*types.ChatMessage, error) {
	rd := ctxutil.GetRequestData(ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, nil, fmt.Errorf("not authenticated")
	}
	if threadID == uuid.Nil {
		return nil, nil, fmt.Errorf("missing thread id")
	}
	if s.threads == nil || s.messages == nil {
		return nil, nil, fmt.Errorf("chat repos not wired")
	}
	transaction := tx
	if transaction == nil {
		transaction = s.db
	}

	rows, err := s.threads.GetByIDs(ctx, transaction, []uuid.UUID{threadID})
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 || rows[0] == nil || rows[0].UserID != rd.UserID {
		return nil, nil, fmt.Errorf("thread not found")
	}

	msgs, err := s.messages.ListByThread(ctx, transaction, threadID, limit)
	if err != nil {
		return nil, nil, err
	}
	return rows[0], msgs, nil
}

func (s *chatService) ListMessages(ctx context.Context, tx *gorm.DB, threadID uuid.UUID, limit int, beforeSeq *int64) ([]*types.ChatMessage, error) {
	rd := ctxutil.GetRequestData(ctx)
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
	transaction := tx
	if transaction == nil {
		transaction = s.db
	}

	rows, err := s.threads.GetByIDs(ctx, transaction, []uuid.UUID{threadID})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0] == nil || rows[0].UserID != rd.UserID {
		return nil, fmt.Errorf("thread not found")
	}

	// Cursor-based paging by seq (monotonic).
	var msgs []*types.ChatMessage
	q := transaction.WithContext(ctx).
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

func (s *chatService) SendMessage(ctx context.Context, tx *gorm.DB, threadID uuid.UUID, content string, idempotencyKey string) (*types.ChatMessage, *types.ChatMessage, *types.JobRun, error) {
	rd := ctxutil.GetRequestData(ctx)
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

	transaction := tx
	if transaction == nil {
		transaction = s.db
	}

	var (
		userMsg    *types.ChatMessage
		asstMsg    *types.ChatMessage
		job        *types.JobRun
		createdNew bool
	)

	// Fast-path idempotency (no lock): let clients safely retry while a response is still running.
	if idempotencyKey != "" {
		var existing types.ChatMessage
		err := transaction.WithContext(ctx).
			Model(&types.ChatMessage{}).
			Where("thread_id = ? AND user_id = ? AND role = ? AND idempotency_key = ? AND deleted_at IS NULL",
				threadID, rd.UserID, "user", idempotencyKey,
			).
			First(&existing).Error
		if err == nil && existing.ID != uuid.Nil {
			userMsg = &existing

			var existingAsst types.ChatMessage
			_ = transaction.WithContext(ctx).
				Model(&types.ChatMessage{}).
				Where("thread_id = ? AND user_id = ? AND seq = ? AND role = ? AND deleted_at IS NULL",
					threadID, rd.UserID, existing.Seq+1, "assistant",
				).
				First(&existingAsst).Error
			if existingAsst.ID != uuid.Nil {
				asstMsg = &existingAsst
			}

			turn, _ := s.turns.GetByUserMessageID(ctx, transaction, rd.UserID, threadID, existing.ID)
			if turn != nil && turn.JobID != nil && s.jobRuns != nil {
				rows, _ := s.jobRuns.GetByIDs(ctx, transaction, []uuid.UUID{*turn.JobID})
				if len(rows) > 0 && rows[0] != nil {
					job = rows[0]
				}
			}
			return userMsg, asstMsg, job, nil
		}
		if err != nil && err != gorm.ErrRecordNotFound {
			return nil, nil, nil, err
		}
	}

	// For now, enforce a single runnable chat_respond per thread (keeps OpenAI conversation ordering sane).
	has, err := s.jobRuns.HasRunnableForEntity(ctx, transaction, rd.UserID, "chat_thread", threadID, "chat_respond")
	if err != nil {
		return nil, nil, nil, err
	}
	if has {
		return nil, nil, nil, fmt.Errorf("thread is busy")
	}

	err = transaction.WithContext(ctx).Transaction(func(txx *gorm.DB) error {
		// Lock thread for concurrency-safe sequencing.
		th, err := s.threads.LockByID(ctx, txx, threadID)
		if err != nil {
			return err
		}
		if th == nil || th.ID == uuid.Nil || th.UserID != rd.UserID {
			return fmt.Errorf("thread not found")
		}

		// Idempotency: if the client retries the same message, return the existing turn.
		if idempotencyKey != "" {
			var existing types.ChatMessage
			err := txx.WithContext(ctx).
				Model(&types.ChatMessage{}).
				Where("thread_id = ? AND user_id = ? AND role = ? AND idempotency_key = ? AND deleted_at IS NULL",
					threadID, rd.UserID, "user", idempotencyKey,
				).
				First(&existing).Error
			if err == nil && existing.ID != uuid.Nil {
				userMsg = &existing

				// Assistant placeholder is always seq+1.
				var existingAsst types.ChatMessage
				_ = txx.WithContext(ctx).
					Model(&types.ChatMessage{}).
					Where("thread_id = ? AND user_id = ? AND seq = ? AND role = ? AND deleted_at IS NULL",
						threadID, rd.UserID, existing.Seq+1, "assistant",
					).
					First(&existingAsst).Error
				if existingAsst.ID != uuid.Nil {
					asstMsg = &existingAsst
				}

				turn, _ := s.turns.GetByUserMessageID(ctx, txx, rd.UserID, threadID, existing.ID)
				if turn != nil && turn.JobID != nil && s.jobRuns != nil {
					rows, _ := s.jobRuns.GetByIDs(ctx, txx, []uuid.UUID{*turn.JobID})
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
		has, err := s.jobRuns.HasRunnableForEntity(ctx, txx, rd.UserID, "chat_thread", threadID, "chat_respond")
		if err != nil {
			return err
		}
		if has {
			return fmt.Errorf("thread is busy")
		}

		now := time.Now().UTC()

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

		if _, err := s.messages.Create(ctx, txx, []*types.ChatMessage{userMsg, asstMsg}); err != nil {
			return err
		}
		createdNew = true

		// Advance thread seq + timestamps.
		if err := s.threads.UpdateFields(ctx, txx, threadID, map[string]interface{}{
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
		enqueued, err := s.jobs.Enqueue(ctx, txx, rd.UserID, "chat_respond", "chat_thread", &entityID, payload)
		if err != nil {
			return err
		}
		job = enqueued

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
		if err := s.turns.Create(ctx, txx, turn); err != nil {
			return err
		}

		// Persist job_id into assistant metadata for client convenience (non-authoritative).
		meta := map[string]any{
			"job_id":  job.ID.String(),
			"turn_id": turnID.String(),
		}
		if b, err := json.Marshal(meta); err == nil {
			_ = s.messages.UpdateFields(ctx, txx, asstMsg.ID, map[string]interface{}{
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
		s.notify.MessageCreated(rd.UserID, threadID, userMsg, meta)
		s.notify.MessageCreated(rd.UserID, threadID, asstMsg, meta)
	}

	return userMsg, asstMsg, job, nil
}

func (s *chatService) RebuildThread(ctx context.Context, tx *gorm.DB, threadID uuid.UUID) (*types.JobRun, error) {
	rd := ctxutil.GetRequestData(ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("not authenticated")
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("missing thread id")
	}
	if s.jobs == nil || s.jobRuns == nil || s.threads == nil {
		return nil, fmt.Errorf("chat service not fully wired")
	}
	transaction := tx
	if transaction == nil {
		transaction = s.db
	}

	threads, err := s.threads.GetByIDs(ctx, transaction, []uuid.UUID{threadID})
	if err != nil {
		return nil, err
	}
	if len(threads) == 0 || threads[0] == nil || threads[0].UserID != rd.UserID {
		return nil, fmt.Errorf("thread not found")
	}

	// Avoid rebuilding while a response is in-flight; let it retry later.
	if has, _ := s.jobRuns.HasRunnableForEntity(ctx, transaction, rd.UserID, "chat_thread", threadID, "chat_respond"); has {
		return nil, fmt.Errorf("thread is busy")
	}

	// Debounce rebuild jobs.
	if has, _ := s.jobRuns.HasRunnableForEntity(ctx, transaction, rd.UserID, "chat_thread", threadID, "chat_rebuild"); has {
		if latest, _ := s.jobRuns.GetLatestByEntity(ctx, transaction, rd.UserID, "chat_thread", threadID, "chat_rebuild"); latest != nil {
			return latest, nil
		}
		return nil, fmt.Errorf("rebuild already queued")
	}

	payload := map[string]any{"thread_id": threadID.String()}
	entityID := threadID
	return s.jobs.Enqueue(ctx, transaction, rd.UserID, "chat_rebuild", "chat_thread", &entityID, payload)
}

func (s *chatService) DeleteThread(ctx context.Context, tx *gorm.DB, threadID uuid.UUID) (*types.JobRun, error) {
	rd := ctxutil.GetRequestData(ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("not authenticated")
	}
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("missing thread id")
	}
	if s.jobs == nil || s.jobRuns == nil || s.db == nil {
		return nil, fmt.Errorf("chat service not fully wired")
	}
	transaction := tx
	if transaction == nil {
		transaction = s.db
	}

	// Avoid deleting while a response is in-flight.
	if has, _ := s.jobRuns.HasRunnableForEntity(ctx, transaction, rd.UserID, "chat_thread", threadID, "chat_respond"); has {
		return nil, fmt.Errorf("thread is busy")
	}
	// Debounce purge jobs.
	if has, _ := s.jobRuns.HasRunnableForEntity(ctx, transaction, rd.UserID, "chat_thread", threadID, "chat_purge"); has {
		if latest, _ := s.jobRuns.GetLatestByEntity(ctx, transaction, rd.UserID, "chat_thread", threadID, "chat_purge"); latest != nil {
			return latest, nil
		}
		return nil, fmt.Errorf("purge already queued")
	}

	var job *types.JobRun
	err := transaction.WithContext(ctx).Transaction(func(txx *gorm.DB) error {
		// Ensure thread exists + belongs to user.
		var thread types.ChatThread
		if err := txx.WithContext(ctx).
			Model(&types.ChatThread{}).
			Where("id = ? AND user_id = ?", threadID, rd.UserID).
			First(&thread).Error; err != nil {
			return fmt.Errorf("thread not found")
		}

		// Soft-delete messages + thread (canonical).
		if err := txx.WithContext(ctx).
			Where("thread_id = ? AND user_id = ?", threadID, rd.UserID).
			Delete(&types.ChatMessage{}).Error; err != nil {
			return err
		}
		if err := txx.WithContext(ctx).
			Where("id = ? AND user_id = ?", threadID, rd.UserID).
			Delete(&types.ChatThread{}).Error; err != nil {
			return err
		}

		// Enqueue purge to remove derived artifacts + vector cache.
		payload := map[string]any{"thread_id": threadID.String()}
		entityID := threadID
		enq, err := s.jobs.Enqueue(ctx, txx, rd.UserID, "chat_purge", "chat_thread", &entityID, payload)
		if err != nil {
			return err
		}
		job = enq
		return nil
	})
	if err != nil {
		return nil, err
	}
	return job, nil
}

func (s *chatService) UpdateMessage(ctx context.Context, tx *gorm.DB, threadID uuid.UUID, messageID uuid.UUID, content string) (*types.JobRun, error) {
	rd := ctxutil.GetRequestData(ctx)
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
	transaction := tx
	if transaction == nil {
		transaction = s.db
	}

	// Only allow editing user messages.
	if err := transaction.WithContext(ctx).
		Model(&types.ChatMessage{}).
		Where("id = ? AND thread_id = ? AND user_id = ? AND role = ?", messageID, threadID, rd.UserID, "user").
		Updates(map[string]interface{}{
			"content":    content,
			"updated_at": time.Now().UTC(),
		}).Error; err != nil {
		return nil, err
	}

	// Rebuild projections to ensure all derived artifacts are consistent.
	return s.RebuildThread(ctx, transaction, threadID)
}

func (s *chatService) DeleteMessage(ctx context.Context, tx *gorm.DB, threadID uuid.UUID, messageID uuid.UUID) (*types.JobRun, error) {
	rd := ctxutil.GetRequestData(ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("not authenticated")
	}
	if threadID == uuid.Nil || messageID == uuid.Nil {
		return nil, fmt.Errorf("missing ids")
	}
	transaction := tx
	if transaction == nil {
		transaction = s.db
	}

	// Only allow deleting user messages.
	if err := transaction.WithContext(ctx).
		Where("id = ? AND thread_id = ? AND user_id = ? AND role = ?", messageID, threadID, rd.UserID, "user").
		Delete(&types.ChatMessage{}).Error; err != nil {
		return nil, err
	}

	return s.RebuildThread(ctx, transaction, threadID)
}

func (s *chatService) GetTurn(ctx context.Context, tx *gorm.DB, turnID uuid.UUID) (*types.ChatTurn, error) {
	rd := ctxutil.GetRequestData(ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("not authenticated")
	}
	if turnID == uuid.Nil {
		return nil, fmt.Errorf("missing turn id")
	}
	if s.turns == nil {
		return nil, fmt.Errorf("chat turns repo not wired")
	}
	transaction := tx
	if transaction == nil {
		transaction = s.db
	}
	return s.turns.GetByID(ctx, transaction, rd.UserID, turnID)
}

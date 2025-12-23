package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/clients/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type RespondDeps struct {
	DB *gorm.DB

	Log *logger.Logger
	AI  openai.Client
	Vec pc.VectorStore

	Threads   repos.ChatThreadRepo
	Messages  repos.ChatMessageRepo
	State     repos.ChatThreadStateRepo
	Summaries repos.ChatSummaryNodeRepo
	Docs      repos.ChatDocRepo
	Turns     repos.ChatTurnRepo

	JobRuns repos.JobRunRepo
	Jobs    services.JobService

	Notify services.ChatNotifier
}

type RespondInput struct {
	UserID uuid.UUID

	ThreadID           uuid.UUID
	UserMessageID      uuid.UUID
	AssistantMessageID uuid.UUID
	TurnID             uuid.UUID
	JobID              uuid.UUID
	Attempt            int
}

type RespondOutput struct {
	AssistantText string `json:"assistant_text"`
}

func Respond(ctx context.Context, deps RespondDeps, in RespondInput) (RespondOutput, error) {
	out := RespondOutput{}
	if deps.DB == nil || deps.Log == nil || deps.AI == nil || deps.Threads == nil || deps.Messages == nil || deps.State == nil || deps.Summaries == nil || deps.Docs == nil || deps.Turns == nil {
		return out, fmt.Errorf("chat respond: missing deps")
	}
	if in.UserID == uuid.Nil || in.ThreadID == uuid.Nil || in.UserMessageID == uuid.Nil || in.AssistantMessageID == uuid.Nil || in.TurnID == uuid.Nil {
		return out, fmt.Errorf("chat respond: missing ids")
	}

	threads, err := deps.Threads.GetByIDs(ctx, deps.DB, []uuid.UUID{in.ThreadID})
	if err != nil {
		return out, err
	}
	if len(threads) == 0 || threads[0] == nil || threads[0].UserID != in.UserID {
		return out, fmt.Errorf("thread not found")
	}
	thread := threads[0]

	// Mark turn as running.
	now := time.Now().UTC()
	_ = deps.Turns.UpdateFields(ctx, deps.DB, in.UserID, in.TurnID, map[string]interface{}{
		"status":     "running",
		"attempt":    in.Attempt,
		"job_id":     in.JobID,
		"started_at": &now,
	})

	// If this is a retry, reset the assistant placeholder so clients can safely restart streaming.
	if in.Attempt > 0 {
		_ = deps.Messages.UpdateFields(ctx, deps.DB, in.AssistantMessageID, map[string]interface{}{
			"content":    "",
			"status":     MessageStatusStreaming,
			"updated_at": time.Now().UTC(),
		})
		if deps.Notify != nil {
			var asst types.ChatMessage
			_ = deps.DB.WithContext(ctx).
				Model(&types.ChatMessage{}).
				Where("id = ? AND thread_id = ? AND user_id = ?", in.AssistantMessageID, in.ThreadID, in.UserID).
				First(&asst).Error
			if asst.ID != uuid.Nil {
				deps.Notify.MessageCreated(in.UserID, in.ThreadID, &asst, map[string]any{
					"turn_id": in.TurnID.String(),
					"attempt": in.Attempt,
					"job_id":  in.JobID.String(),
				})
			}
		}
	}

	// Load the user message content (canonical).
	var userMsg types.ChatMessage
	if err := deps.DB.WithContext(ctx).
		Model(&types.ChatMessage{}).
		Where("id = ? AND thread_id = ? AND user_id = ?", in.UserMessageID, in.ThreadID, in.UserID).
		First(&userMsg).Error; err != nil {
		return out, err
	}
	userText := strings.TrimSpace(userMsg.Content)
	if userText == "" {
		return out, fmt.Errorf("empty user message")
	}

	// Ensure thread state + OpenAI conversation.
	state, err := deps.State.GetOrCreate(ctx, deps.DB, in.ThreadID)
	if err != nil {
		return out, err
	}
	conversationID := ""
	if state.OpenAIConversationID != nil && strings.TrimSpace(*state.OpenAIConversationID) != "" {
		conversationID = strings.TrimSpace(*state.OpenAIConversationID)
	}
	if conversationID == "" {
		// Conversations are an optimization; proceed without them if creation fails.
		if cid, err := deps.AI.CreateConversation(ctx); err == nil && strings.TrimSpace(cid) != "" {
			conversationID = strings.TrimSpace(cid)
			_ = deps.State.UpdateFields(ctx, deps.DB, in.ThreadID, map[string]interface{}{
				"openai_conversation_id": conversationID,
			})
			state.OpenAIConversationID = &conversationID
		}
	}

	plan, err := BuildContextPlan(ctx, ContextPlanDeps{
		DB:        deps.DB,
		AI:        deps.AI,
		Vec:       deps.Vec,
		Docs:      deps.Docs,
		Messages:  deps.Messages,
		Summaries: deps.Summaries,
	}, ContextPlanInput{
		UserID:   in.UserID,
		Thread:   thread,
		State:    state,
		UserText: userText,
	})
	if err != nil {
		return out, err
	}

	// Persist the retrieval decision trace early for debuggability (even if streaming fails later).
	if len(plan.Trace) > 0 {
		if b, err := json.Marshal(plan.Trace); err == nil {
			_ = deps.Turns.UpdateFields(ctx, deps.DB, in.UserID, in.TurnID, map[string]interface{}{
				"retrieval_trace": datatypes.JSON(b),
			})
		}
	}

	// Stream response into assistant message.
	var (
		full          strings.Builder
		pending       strings.Builder
		lastFlushAt   = time.Now()
		lastNotifyAt  = time.Now()
		lastFlushSize = 0
		pendingBytes  = 0
		deltaSeq      int64 // SSE delta chunk seq (not raw model delta count)
	)

	flushDB := func() {
		// Throttle DB writes.
		if time.Since(lastFlushAt) < 750*time.Millisecond && (full.Len()-lastFlushSize) < 256 {
			return
		}
		txt := full.String()
		lastFlushAt = time.Now()
		lastFlushSize = len(txt)
		_ = deps.Messages.UpdateFields(ctx, deps.DB, in.AssistantMessageID, map[string]interface{}{
			"content":    txt,
			"status":     MessageStatusStreaming,
			"updated_at": time.Now().UTC(),
		})
	}

	flushNotify := func() {
		if deps.Notify == nil {
			pending.Reset()
			pendingBytes = 0
			return
		}
		chunk := pending.String()
		if chunk == "" {
			return
		}
		pending.Reset()
		pendingBytes = 0
		deltaSeq++
		deps.Notify.MessageDelta(in.UserID, in.ThreadID, in.AssistantMessageID, chunk, map[string]any{
			"turn_id":     in.TurnID.String(),
			"attempt":     in.Attempt,
			"delta_seq":   deltaSeq,
			"content_len": full.Len(),
		})
		lastNotifyAt = time.Now()
	}

	onDelta := func(delta string) {
		if delta == "" {
			return
		}
		full.WriteString(delta)
		pending.WriteString(delta)
		pendingBytes += len(delta)

		// Throttle SSE emits to avoid blowing up hub buffers (backpressure).
		if time.Since(lastNotifyAt) >= 150*time.Millisecond || pendingBytes >= 512 {
			flushNotify()
		}
		flushDB()
	}

	var text string
	if strings.TrimSpace(conversationID) != "" {
		text, err = deps.AI.StreamTextInConversation(ctx, conversationID, plan.Instructions, plan.UserPayload, onDelta)
	} else {
		text, err = deps.AI.StreamText(ctx, plan.Instructions, plan.UserPayload, onDelta)
	}
	if err != nil {
		flushNotify()
		_ = deps.Messages.UpdateFields(ctx, deps.DB, in.AssistantMessageID, map[string]interface{}{
			"status":     MessageStatusError,
			"updated_at": time.Now().UTC(),
		})
		if deps.Notify != nil {
			deps.Notify.MessageError(in.UserID, in.ThreadID, in.AssistantMessageID, err.Error(), map[string]any{
				"turn_id": in.TurnID.String(),
				"attempt": in.Attempt,
			})
		}
		doneAt := time.Now().UTC()
		_ = deps.Turns.UpdateFields(ctx, deps.DB, in.UserID, in.TurnID, map[string]interface{}{
			"status":       "error",
			"completed_at": &doneAt,
		})
		return out, err
	}
	flushNotify()
	// Ensure we have the full text (stream function also returns full).
	if strings.TrimSpace(text) == "" {
		text = full.String()
	}
	text = strings.TrimSpace(text)
	out.AssistantText = text

	// Persist final message content + status.
	if err := deps.Messages.UpdateFields(ctx, deps.DB, in.AssistantMessageID, map[string]interface{}{
		"content":    text,
		"status":     MessageStatusDone,
		"updated_at": time.Now().UTC(),
	}); err != nil {
		return out, err
	}

	// Fetch assistant message for done event payload.
	var asst types.ChatMessage
	_ = deps.DB.WithContext(ctx).Model(&types.ChatMessage{}).Where("id = ?", in.AssistantMessageID).First(&asst).Error
	if deps.Notify != nil {
		deps.Notify.MessageDone(in.UserID, in.ThreadID, &asst, map[string]any{
			"turn_id": in.TurnID.String(),
			"attempt": in.Attempt,
		})
	}

	doneAt := time.Now().UTC()
	_ = deps.Turns.UpdateFields(ctx, deps.DB, in.UserID, in.TurnID, map[string]interface{}{
		"status":                 "done",
		"completed_at":           &doneAt,
		"openai_conversation_id": state.OpenAIConversationID,
	})

	// Enqueue maintenance job (debounced per thread).
	if deps.Jobs != nil && deps.JobRuns != nil {
		has, _ := deps.JobRuns.HasRunnableForEntity(ctx, deps.DB, in.UserID, "chat_thread", in.ThreadID, "chat_maintain")
		if !has {
			payload := map[string]any{
				"thread_id": in.ThreadID.String(),
			}
			entityID := in.ThreadID
			_, _ = deps.Jobs.Enqueue(ctx, deps.DB, in.UserID, "chat_maintain", "chat_thread", &entityID, payload)
		}
	}

	return out, nil
}

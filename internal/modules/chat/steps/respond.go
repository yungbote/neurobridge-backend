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

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
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
	Path      repos.PathRepo
	PathNodes repos.PathNodeRepo
	NodeDocs  repos.LearningNodeDocRepo
	Concepts  repos.ConceptRepo
	Edges     repos.ConceptEdgeRepo
	Mastery   repos.UserConceptStateRepo
	Models    repos.UserConceptModelRepo
	Miscon    repos.UserMisconceptionInstanceRepo

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

	dbc := dbctx.Context{Ctx: ctx, Tx: deps.DB}
	threads, err := deps.Threads.GetByIDs(dbc, []uuid.UUID{in.ThreadID})
	if err != nil {
		return out, err
	}
	if len(threads) == 0 || threads[0] == nil || threads[0].UserID != in.UserID {
		return out, fmt.Errorf("thread not found")
	}
	thread := threads[0]

	// Mark turn as running.
	now := time.Now().UTC()
	_ = deps.Turns.UpdateFields(dbc, in.UserID, in.TurnID, map[string]interface{}{
		"status":     "running",
		"attempt":    in.Attempt,
		"job_id":     in.JobID,
		"started_at": &now,
	})

	// If this is a retry, reset the assistant placeholder so clients can safely restart streaming.
	if in.Attempt > 0 {
		_ = deps.Messages.UpdateFields(dbc, in.AssistantMessageID, map[string]interface{}{
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
	state, err := deps.State.GetOrCreate(dbc, in.ThreadID)
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
			_ = deps.State.UpdateFields(dbc, in.ThreadID, map[string]interface{}{
				"openai_conversation_id": conversationID,
			})
			state.OpenAIConversationID = &conversationID
		}
	}

	// Gather recent messages for routing / fast response context.
	recent := ""
	if deps.Messages != nil {
		if history, err := deps.Messages.ListRecent(dbc, in.ThreadID, 12); err == nil && len(history) > 0 {
			recent = formatRecent(history, 6)
		}
	}

	route := chatRouteDecision{Route: "product"}
	if !threadHasActiveWaitpoint(ctx, deps, thread, in.UserID) {
		if r, err := routeChatMessage(ctx, deps, thread, userText, recent); err == nil {
			route = r
		}
	}

	var (
		instructions    string
		userPayload     string
		trace           map[string]any
		aiClient        openai.Client
		useConversation bool
		evidenceSources []EvidenceSource
		selectedEvidence []EvidenceSource
		evidenceText    string
		evidenceBudget  int
	)
	trace = map[string]any{}
	aiClient = deps.AI
	useConversation = strings.TrimSpace(conversationID) != ""

	switch strings.ToLower(strings.TrimSpace(route.Route)) {
	case "smalltalk":
		instructions, userPayload = promptFastChat(recent, userText)
		if fastModel := resolveChatFastModel(); fastModel != "" {
			aiClient = openai.WithModel(deps.AI, fastModel)
			trace["model"] = fastModel
		}
		trace["route"] = "smalltalk"
		trace["context_messages"] = 6
		useConversation = false
	case "tool":
		toolRes, err := executeChatToolCalls(ctx, deps, thread, route.ToolCalls)
		if err != nil {
			return out, err
		}
		// Persist tool metadata into assistant message and finish without streaming.
		meta := map[string]any{
			"kind":       "tool_call",
			"tool_calls": route.ToolCalls,
		}
		for k, v := range toolRes.Metadata {
			meta[k] = v
		}
		metaJSON, _ := json.Marshal(meta)
		_ = deps.Messages.UpdateFields(dbc, in.AssistantMessageID, map[string]interface{}{
			"content":    toolRes.Text,
			"status":     MessageStatusDone,
			"metadata":   datatypes.JSON(metaJSON),
			"updated_at": time.Now().UTC(),
		})
		if deps.Notify != nil {
			var asst types.ChatMessage
			_ = deps.DB.WithContext(ctx).Model(&types.ChatMessage{}).Where("id = ?", in.AssistantMessageID).First(&asst).Error
			if asst.ID != uuid.Nil {
				deps.Notify.MessageDone(in.UserID, in.ThreadID, &asst, map[string]any{
					"turn_id": in.TurnID.String(),
					"attempt": in.Attempt,
				})
			}
		}
		trace["route"] = "tool"
		trace["tool_meta"] = toolRes.Metadata
		if b, err := json.Marshal(trace); err == nil {
			_ = deps.Turns.UpdateFields(dbc, in.UserID, in.TurnID, map[string]interface{}{
				"retrieval_trace": datatypes.JSON(b),
			})
		}
		doneAt := time.Now().UTC()
		_ = deps.Turns.UpdateFields(dbc, in.UserID, in.TurnID, map[string]interface{}{
			"status":       "done",
			"completed_at": &doneAt,
		})
		out.AssistantText = strings.TrimSpace(toolRes.Text)
		if deps.Jobs != nil && deps.JobRuns != nil {
			has, _ := deps.JobRuns.HasRunnableForEntity(dbc, in.UserID, "chat_thread", in.ThreadID, "chat_maintain")
			if !has {
				payload := map[string]any{
					"thread_id": in.ThreadID.String(),
				}
				entityID := in.ThreadID
				_, _ = deps.Jobs.Enqueue(dbc, in.UserID, "chat_maintain", "chat_thread", &entityID, payload)
			}
		}
		return out, nil
	default:
		plan, err := BuildContextPlan(ctx, ContextPlanDeps{
			DB:        deps.DB,
			AI:        deps.AI,
			Vec:       deps.Vec,
			Docs:      deps.Docs,
			Messages:  deps.Messages,
			Summaries: deps.Summaries,
			Path:      deps.Path,
			PathNodes: deps.PathNodes,
			NodeDocs:  deps.NodeDocs,
			Concepts:  deps.Concepts,
			Edges:     deps.Edges,
			Mastery:   deps.Mastery,
			Models:    deps.Models,
			Miscon:    deps.Miscon,
		}, ContextPlanInput{
			UserID:   in.UserID,
			Thread:   thread,
			State:    state,
			UserText: userText,
			UserMsg:  &userMsg,
		})
		if err != nil {
			return out, err
		}
		instructions = plan.Instructions
		userPayload = plan.UserPayload
		trace = plan.Trace
		if trace == nil {
			trace = map[string]any{}
		}
		trace["route"] = "product"
		evidenceSources = plan.EvidenceSources
		evidenceBudget = plan.EvidenceTokenBudget
		if len(evidenceSources) > 0 {
			selected, etrace := selectEvidenceSources(ctx, deps.AI, userText, evidenceSources)
			if len(etrace) > 0 {
				trace["evidence_select"] = etrace
			}
			if len(selected) == 0 {
				selected = evidenceSources
				trace["evidence_select_fallback"] = true
			}
			selectedEvidence = selected
			if wantsVerbatimQuote(userText) && wantsMaterialQuotes(userText) {
				matSources := filterQuoteSources(evidenceSources, "materials")
				if len(matSources) > 0 {
					seen := map[string]bool{}
					merged := make([]EvidenceSource, 0, len(selectedEvidence)+len(matSources))
					for _, s := range selectedEvidence {
						if strings.TrimSpace(s.ID) != "" {
							seen[s.ID] = true
						}
						merged = append(merged, s)
					}
					for _, s := range matSources {
						if strings.TrimSpace(s.ID) == "" || seen[s.ID] {
							continue
						}
						merged = append(merged, s)
					}
					selectedEvidence = merged
					trace["evidence_select_materials_forced"] = true
				}
			}
			evidenceText = renderEvidenceSources(selectedEvidence, plan.EvidenceTokenBudget)
			if strings.TrimSpace(evidenceText) != "" {
				instructions = strings.TrimSpace(instructions) + "\n\n## Evidence Sources (use for factual claims)\n" + evidenceText + "\n\nWhen stating facts or quoting, add citation markers like [[source:ID]]."
				trace["evidence_sources"] = len(selected)
			}
		}
	}

	// Guard: if user asks for verbatim slide/file quotes but we have no source excerpts, reply deterministically.
	if wantsMaterialQuotes(userText) && len(filterQuoteSources(selectedEvidence, "materials")) == 0 {
		reply := strings.TrimSpace(strings.Join([]string{
			"I don’t have any slide text indexed for this path, so I can’t provide citations or verbatim quotes from the file.",
			"If you want slide‑level quotes, re‑ingest the file (or upload a PDF) so the slide text can be extracted and indexed.",
			"I can still list the source file(s) or summarize based on the path materials summary if that’s helpful.",
		}, "\n"))
		now := time.Now().UTC()
		meta := map[string]any{"material_quote_missing": true}
		metaJSON, _ := json.Marshal(meta)
		_ = deps.Messages.UpdateFields(dbc, in.AssistantMessageID, map[string]interface{}{
			"content":    reply,
			"status":     MessageStatusDone,
			"metadata":   datatypes.JSON(metaJSON),
			"updated_at": now,
		})
		if deps.Notify != nil {
			var asst types.ChatMessage
			_ = deps.DB.WithContext(ctx).Model(&types.ChatMessage{}).Where("id = ?", in.AssistantMessageID).First(&asst).Error
			if asst.ID != uuid.Nil {
				deps.Notify.MessageDone(in.UserID, in.ThreadID, &asst, map[string]any{
					"turn_id": in.TurnID.String(),
					"attempt": in.Attempt,
				})
			}
		}
		doneAt := time.Now().UTC()
		_ = deps.Turns.UpdateFields(dbc, in.UserID, in.TurnID, map[string]interface{}{
			"status":       "done",
			"completed_at": &doneAt,
		})
		out.AssistantText = reply
		return out, nil
	}

	// Persist the retrieval/route trace early for debuggability (even if streaming fails later).
	if len(trace) > 0 {
		if b, err := json.Marshal(trace); err == nil {
			_ = deps.Turns.UpdateFields(dbc, in.UserID, in.TurnID, map[string]interface{}{
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
		_ = deps.Messages.UpdateFields(dbc, in.AssistantMessageID, map[string]interface{}{
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
	if useConversation && strings.TrimSpace(conversationID) != "" {
		text, err = aiClient.StreamTextInConversation(ctx, conversationID, instructions, userPayload, onDelta)
	} else {
		text, err = aiClient.StreamText(ctx, instructions, userPayload, onDelta)
	}
	if err != nil {
		flushNotify()
		_ = deps.Messages.UpdateFields(dbc, in.AssistantMessageID, map[string]interface{}{
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
		_ = deps.Turns.UpdateFields(dbc, in.UserID, in.TurnID, map[string]interface{}{
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

	// Post-process citations and verify quotes against evidence.
	citations := []EvidenceCitation{}
	quoteVerified := true
	if len(selectedEvidence) > 0 && strings.TrimSpace(evidenceText) != "" {
		ids, cleaned := parseCitationMarkers(text)
		if len(ids) > 0 {
			citations = buildCitations(ids, selectedEvidence)
			if len(citations) > 0 {
				text = applyCitationReplacements(cleaned, citations, selectedEvidence)
			} else {
				text = stripCitationMarkers(cleaned)
			}
		} else {
			text = cleaned
		}

		quoteIntent := wantsVerbatimQuote(userText)
		quotePreference := "any"
		if wantsMaterialQuotes(userText) {
			quotePreference = "materials"
		}
		quoteSources := selectedEvidence
		quoteEvidenceText := evidenceText
		if quoteIntent {
			quoteSources = filterQuoteSources(selectedEvidence, quotePreference)
			quoteEvidenceText = renderEvidenceSources(quoteSources, evidenceBudget)
			if strings.TrimSpace(quoteEvidenceText) == "" {
				quoteEvidenceText = "No verbatim evidence available."
			}
		}

		quotes := extractQuotedStrings(text)
		forceRepair := false
		if quoteIntent && len(quoteSources) == 0 && len(quotes) > 0 {
			forceRepair = true
		}

		if !forceRepair {
			if ok, _ := verifyQuotesInEvidence(quotes, quoteSources); !ok {
				quoteVerified = false
				forceRepair = true
			}
		} else {
			quoteVerified = false
		}
		if forceRepair {
			if fixed, ferr := repairQuotedAnswer(ctx, aiClient, text, quoteEvidenceText); ferr == nil && strings.TrimSpace(fixed) != "" {
				ids2, cleaned2 := parseCitationMarkers(fixed)
				if len(ids2) > 0 {
					citations = buildCitations(ids2, selectedEvidence)
					if len(citations) > 0 {
						text = applyCitationReplacements(cleaned2, citations, selectedEvidence)
					} else {
						text = stripCitationMarkers(cleaned2)
					}
				} else {
					text = cleaned2
				}
				quotes2 := extractQuotedStrings(text)
				if ok2, _ := verifyQuotesInEvidence(quotes2, quoteSources); ok2 {
					quoteVerified = true
				}
			}
		}
		out.AssistantText = text
	}

	// Persist final message content + status.
	meta := map[string]any{}
	if len(citations) > 0 {
		meta["citations"] = citations
	}
	if len(selectedEvidence) > 0 {
		ids := make([]string, 0, len(selectedEvidence))
		for _, s := range selectedEvidence {
			if strings.TrimSpace(s.ID) != "" {
				ids = append(ids, s.ID)
			}
		}
		meta["evidence_ids"] = ids
	}
	meta["quote_verified"] = quoteVerified
	metaJSON, _ := json.Marshal(meta)
	if err := deps.Messages.UpdateFields(dbc, in.AssistantMessageID, map[string]interface{}{
		"content":    text,
		"status":     MessageStatusDone,
		"metadata":   datatypes.JSON(metaJSON),
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
	_ = deps.Turns.UpdateFields(dbc, in.UserID, in.TurnID, map[string]interface{}{
		"status":                 "done",
		"completed_at":           &doneAt,
		"openai_conversation_id": state.OpenAIConversationID,
	})

	// Enqueue maintenance job (debounced per thread).
	if deps.Jobs != nil && deps.JobRuns != nil {
		has, _ := deps.JobRuns.HasRunnableForEntity(dbc, in.UserID, "chat_thread", in.ThreadID, "chat_maintain")
		if !has {
			payload := map[string]any{
				"thread_id": in.ThreadID.String(),
			}
			entityID := in.ThreadID
			_, _ = deps.Jobs.Enqueue(dbc, in.UserID, "chat_maintain", "chat_thread", &entityID, payload)
		}
	}

	return out, nil
}

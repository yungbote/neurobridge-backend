package chat_waitpoint_interpret

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	"github.com/yungbote/neurobridge-backend/internal/observability"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/waitpoint"
	waitcfg "github.com/yungbote/neurobridge-backend/internal/waitpoint/configs"
)

type proposalPayload struct {
	PathNodeID      string          `json:"path_node_id"`
	PathID          string          `json:"path_id"`
	DocID           string          `json:"doc_id"`
	BlockID         string          `json:"block_id"`
	BlockType       string          `json:"block_type"`
	Action          string          `json:"action"`
	CitationPolicy  string          `json:"citation_policy"`
	Instruction     string          `json:"instruction"`
	Selection       map[string]any  `json:"selection"`
	BeforeBlockText string          `json:"before_block_text"`
	AfterBlockText  string          `json:"after_block_text"`
	BeforeBlock     json.RawMessage `json:"before_block"`
	AfterBlock      json.RawMessage `json:"after_block"`
}

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	if p == nil || p.db == nil || p.log == nil || p.ai == nil ||
		p.threads == nil || p.messages == nil || p.turns == nil || p.jobRuns == nil || p.jobs == nil {
		jc.Fail("validate", fmt.Errorf("chat_waitpoint_interpret: missing dependencies"))
		return nil
	}

	threadID, ok := jc.PayloadUUID("thread_id")
	if !ok || threadID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing thread_id"))
		return nil
	}

	userMsgID, ok := jc.PayloadUUID("user_message_id")
	if !ok || userMsgID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing user_message_id"))
		return nil
	}

	userID := jc.Job.OwnerUserID
	if userID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing owner_user_id"))
		return nil
	}

	jc.Progress("load", 2, "Interpreting your response")

	// Load thread
	thRows, err := p.threads.GetByIDs(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, []uuid.UUID{threadID})
	if err != nil || len(thRows) == 0 || thRows[0] == nil || thRows[0].UserID != userID {
		jc.Succeed("done", map[string]any{"mode": "thread_not_found"})
		return nil
	}
	th := thRows[0]

	// Load user message
	var userMsg types.ChatMessage
	if err := p.db.WithContext(jc.Ctx).
		Model(&types.ChatMessage{}).
		Where("id = ? AND thread_id = ? AND user_id = ? AND role = ? AND deleted_at IS NULL", userMsgID, threadID, userID, "user").
		First(&userMsg).Error; err != nil {
		jc.Succeed("done", map[string]any{"mode": "user_message_not_found"})
		return nil
	}

	waitpointJobID := uuid.Nil
	if id, ok := jc.PayloadUUID("waitpoint_job_id"); ok && id != uuid.Nil {
		waitpointJobID = id
	}
	if waitpointJobID == uuid.Nil {
		waitpointJobID = pendingWaitpointJobID(th.Metadata)
	}
	if waitpointJobID == uuid.Nil {
		jc.Succeed("done", map[string]any{"mode": "no_pending_waitpoint"})
		return nil
	}

	jRows, err := p.jobRuns.GetByIDs(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, []uuid.UUID{waitpointJobID})
	if err != nil || len(jRows) == 0 || jRows[0] == nil {
		jc.Succeed("done", map[string]any{"mode": "waitpoint_job_not_found"})
		return nil
	}
	waitJob := jRows[0]
	if waitJob.OwnerUserID != userID {
		jc.Succeed("done", map[string]any{"mode": "waitpoint_owner_mismatch"})
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(waitJob.Status), "waiting_user") {
		jc.Succeed("done", map[string]any{"mode": "waitpoint_not_waiting"})
		return nil
	}

	env := parseWaitpointEnvelope(waitJob.Result)
	if env == nil || strings.TrimSpace(env.Waitpoint.Kind) == "" {
		jc.Succeed("done", map[string]any{"mode": "no_waitpoint_envelope"})
		return nil
	}

	if env.State.LastUserMessageID == userMsg.ID.String() ||
		(env.State.LastUserSeqHandled > 0 && userMsg.Seq <= env.State.LastUserSeqHandled) {
		jc.Succeed("done", map[string]any{"mode": "already_handled"})
		return nil
	}

	msgs, _ := p.messages.ListByThread(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, threadID, 200)

	reg := waitpoint.NewRegistry()
	_ = reg.Register(waitcfg.YAMLIntentConfig())

	interp := waitpoint.NewInterpreter(reg)
	ic := &waitpoint.InterpreterContext{
		Ctx:         jc.Ctx,
		UserID:      userID,
		ThreadID:    threadID,
		Thread:      th,
		UserMessage: &userMsg,
		ParentJob:   nil,
		ChildJob:    waitJob,
		Envelope:    env,
		Messages:    msgs,
		AI:          p.ai,
	}

	decision, cr, ierr := interp.Run(ic)
	if ierr != nil {
		jc.Fail("interpret", ierr)
		return nil
	}

	if p.traces != nil {
		if trace := buildWaitpointDecisionTrace(
			"chat_waitpoint",
			userID,
			threadID,
			&userMsg,
			waitJob,
			env,
			decision,
			cr,
			p.Type(),
		); trace != nil {
			if metrics := observability.Current(); metrics != nil {
				metrics.IncTraceAttempted("decision")
			}
			if _, err := p.traces.Create(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, []*types.DecisionTrace{trace}); err != nil {
				if metrics := observability.Current(); metrics != nil {
					metrics.IncTraceFailed("decision")
				}
				if p.log != nil {
					p.log.Debug("decision trace create failed", "error", err.Error())
				}
			} else {
				if metrics := observability.Current(); metrics != nil {
					metrics.IncTraceWritten("decision")
				}
			}
		}
	}

	env.State.Version = 1
	env.State.LastUserMessageID = userMsg.ID.String()
	env.State.LastUserSeqHandled = userMsg.Seq
	env.State.LastCase = string(cr.Case)
	env.State.LastConfidence = cr.Confidence
	env.State.Attempts++

	switch decision.Kind {
	case waitpoint.DecisionContinueChat:
		_ = p.persistEnvelope(jc, waitJob.ID, env)
		if decision.EnqueueChatRespond {
			_ = p.enqueueChatRespondForMessage(jc, threadID, userMsg.ID)
		}
		jc.Succeed("done", map[string]any{"case": cr.Case, "decision": decision.Kind})
		return nil
	case waitpoint.DecisionAskClarify:
		_ = p.persistEnvelope(jc, waitJob.ID, env)
		if strings.TrimSpace(decision.AssistantMessage) != "" {
			_ = p.appendAssistantMessage(jc, threadID, userID, decision.AssistantMessage, map[string]any{
				"kind":           "waitpoint_clarification",
				"waitpoint_kind": env.Waitpoint.Kind,
				"child_job_id":   waitJob.ID.String(),
			})
		}
		jc.Succeed("done", map[string]any{"case": cr.Case, "decision": decision.Kind})
		return nil
	case waitpoint.DecisionConfirmResume:
		_ = p.persistEnvelope(jc, waitJob.ID, env)
		commitType := ""
		if decision.Selection != nil {
			commitType = strings.ToLower(strings.TrimSpace(stringFromAny(decision.Selection["commit_type"])))
		}
		if commitType == "" {
			commitType = "confirm"
		}

		prop := proposalFromEnvelope(env)
		if strings.TrimSpace(prop.PathNodeID) == "" || strings.TrimSpace(prop.BlockID) == "" {
			commitType = "deny"
		}

		switch commitType {
		case "deny":
			_ = p.enqueueNodeDocEditApply(jc, userID, waitJob.ID, "deny")
			_ = p.clearThreadPendingWaitpoint(jc, threadID)
			if strings.TrimSpace(decision.ConfirmMessage) != "" {
				_ = p.appendAssistantMessage(jc, threadID, userID, decision.ConfirmMessage, map[string]any{
					"kind":           "waitpoint_confirm",
					"waitpoint_kind": env.Waitpoint.Kind,
					"child_job_id":   waitJob.ID.String(),
				})
			}
			jc.Succeed("done", map[string]any{"case": cr.Case, "decision": decision.Kind, "commit": commitType})
			return nil
		case "change", "refine":
			refineText := strings.TrimSpace(userMsg.Content)
			if refineText == "" {
				refineText = "Refine the previous edit."
			}
			if strings.TrimSpace(prop.AfterBlockText) != "" {
				refineText = strings.TrimSpace(strings.Join([]string{
					"Refine the draft below using the user's instructions.",
					"",
					"USER_INSTRUCTIONS:",
					refineText,
					"",
					"PREVIOUS_DRAFT:",
					prop.AfterBlockText,
				}, "\n"))
			}
			_ = p.markProposalResolved(jc, waitJob.ID, "superseded")
			_ = p.clearThreadPendingWaitpoint(jc, threadID)
			_ = p.enqueueNodeDocEdit(jc, userID, threadID, prop, refineText, waitJob.ID)
			if strings.TrimSpace(decision.ConfirmMessage) != "" {
				_ = p.appendAssistantMessage(jc, threadID, userID, decision.ConfirmMessage, map[string]any{
					"kind":           "waitpoint_confirm",
					"waitpoint_kind": env.Waitpoint.Kind,
					"child_job_id":   waitJob.ID.String(),
				})
			}
			jc.Succeed("done", map[string]any{"case": cr.Case, "decision": decision.Kind, "commit": commitType})
			return nil
		default:
			_ = p.enqueueNodeDocEditApply(jc, userID, waitJob.ID, "confirm")
			_ = p.clearThreadPendingWaitpoint(jc, threadID)
			if strings.TrimSpace(decision.ConfirmMessage) != "" {
				_ = p.appendAssistantMessage(jc, threadID, userID, decision.ConfirmMessage, map[string]any{
					"kind":           "waitpoint_confirm",
					"waitpoint_kind": env.Waitpoint.Kind,
					"child_job_id":   waitJob.ID.String(),
				})
			}
			jc.Succeed("done", map[string]any{"case": cr.Case, "decision": decision.Kind, "commit": commitType})
			return nil
		}
	default:
		_ = p.persistEnvelope(jc, waitJob.ID, env)
		_ = p.enqueueChatRespondForMessage(jc, threadID, userMsg.ID)
		jc.Succeed("done", map[string]any{"case": cr.Case, "decision": decision.Kind})
		return nil
	}
}

func pendingWaitpointJobID(raw datatypes.JSON) uuid.UUID {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return uuid.Nil
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil || meta == nil {
		return uuid.Nil
	}
	id := strings.TrimSpace(stringFromAny(meta["pending_waitpoint_job_id"]))
	if id == "" {
		return uuid.Nil
	}
	uid, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil
	}
	return uid
}

func proposalFromEnvelope(env *jobrt.WaitpointEnvelope) proposalPayload {
	prop := proposalPayload{}
	if env == nil || env.Data == nil {
		return prop
	}
	raw := env.Data["proposal"]
	b, _ := json.Marshal(raw)
	_ = json.Unmarshal(b, &prop)
	return prop
}

func (p *Pipeline) enqueueNodeDocEdit(jc *jobrt.Context, userID uuid.UUID, threadID uuid.UUID, prop proposalPayload, instruction string, parentJobID uuid.UUID) error {
	if p.jobs == nil || jc == nil || jc.Job == nil {
		return nil
	}
	pathNodeID, err := uuid.Parse(strings.TrimSpace(prop.PathNodeID))
	if err != nil || pathNodeID == uuid.Nil {
		return fmt.Errorf("invalid path_node_id")
	}
	payload := map[string]any{
		"thread_id":       threadID.String(),
		"path_node_id":    pathNodeID.String(),
		"block_id":        strings.TrimSpace(prop.BlockID),
		"action":          defaultString(strings.TrimSpace(prop.Action), "rewrite"),
		"citation_policy": defaultString(strings.TrimSpace(prop.CitationPolicy), "reuse_only"),
		"instruction":     strings.TrimSpace(instruction),
		"selection":       prop.Selection,
		"refine_from_job": parentJobID.String(),
	}
	entityID := pathNodeID
	_, err = p.jobs.Enqueue(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, userID, "node_doc_edit", "path_node", &entityID, payload)
	return err
}

func (p *Pipeline) enqueueNodeDocEditApply(jc *jobrt.Context, userID uuid.UUID, proposalJobID uuid.UUID, decision string) error {
	if p.jobs == nil || proposalJobID == uuid.Nil {
		return nil
	}
	payload := map[string]any{
		"proposal_job_id": proposalJobID.String(),
		"decision":        decision,
	}
	entityID := proposalJobID
	_, err := p.jobs.Enqueue(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, userID, "node_doc_edit_apply", "job_run", &entityID, payload)
	return err
}

func (p *Pipeline) clearThreadPendingWaitpoint(jc *jobrt.Context, threadID uuid.UUID) error {
	if p.threads == nil || threadID == uuid.Nil {
		return nil
	}
	var meta map[string]any
	if err := p.db.WithContext(jc.Ctx).Model(&types.ChatThread{}).Select("metadata").Where("id = ?", threadID).Scan(&meta).Error; err != nil {
		return err
	}
	if meta == nil {
		return nil
	}
	delete(meta, "pending_waitpoint_job_id")
	delete(meta, "pending_waitpoint_kind")
	delete(meta, "pending_waitpoint_proposal")
	metaJSON, _ := json.Marshal(meta)
	return p.threads.UpdateFields(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, threadID, map[string]interface{}{"metadata": datatypes.JSON(metaJSON)})
}

func (p *Pipeline) markProposalResolved(jc *jobrt.Context, jobID uuid.UUID, status string) error {
	if p.jobRuns == nil || jobID == uuid.Nil {
		return nil
	}
	return p.jobRuns.UpdateFields(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, jobID, map[string]interface{}{
		"status":     "succeeded",
		"stage":      "resolved",
		"message":    fmt.Sprintf("node_doc_edit %s", status),
		"updated_at": time.Now().UTC(),
	})
}

func (p *Pipeline) persistEnvelope(jc *jobrt.Context, jobID uuid.UUID, env *jobrt.WaitpointEnvelope) error {
	if p.db == nil || jobID == uuid.Nil || env == nil {
		return nil
	}
	b, _ := json.Marshal(env)
	now := time.Now().UTC()
	return p.db.WithContext(jc.Ctx).
		Model(&types.JobRun{}).
		Where("id = ?", jobID).
		Updates(map[string]any{
			"result":     datatypes.JSON(b),
			"updated_at": now,
		}).Error
}

func (p *Pipeline) appendAssistantMessage(
	jc *jobrt.Context,
	threadID uuid.UUID,
	userID uuid.UUID,
	content string,
	meta map[string]any,
) error {
	if p.db == nil || p.threads == nil || p.messages == nil ||
		threadID == uuid.Nil || userID == uuid.Nil {
		return nil
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	dbc := dbctx.Context{Ctx: jc.Ctx, Tx: p.db}
	thRows, err := p.threads.GetByIDs(dbc, []uuid.UUID{threadID})
	if err != nil || len(thRows) == 0 || thRows[0] == nil || thRows[0].UserID != userID {
		return fmt.Errorf("thread not found")
	}
	th := thRows[0]

	now := time.Now().UTC()
	seq := th.NextSeq + 1
	metaJSON, _ := json.Marshal(meta)
	msg := &types.ChatMessage{
		ID:        uuid.New(),
		ThreadID:  threadID,
		UserID:    userID,
		Seq:       seq,
		Role:      "assistant",
		Status:    "sent",
		Content:   content,
		Metadata:  datatypes.JSON(metaJSON),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := p.messages.Create(dbc, []*types.ChatMessage{msg}); err != nil {
		return err
	}
	if err := p.threads.UpdateFields(dbc, threadID, map[string]interface{}{
		"next_seq":        seq,
		"last_message_at": now,
		"updated_at":      now,
	}); err != nil {
		return err
	}
	if p.notify != nil {
		p.notify.MessageCreated(userID, threadID, msg, meta)
	}
	return nil
}

// enqueueChatRespondForMessage enqueues chat_respond after waitpoint classification.
func (p *Pipeline) enqueueChatRespondForMessage(
	jc *jobrt.Context,
	threadID uuid.UUID,
	userMsgID uuid.UUID,
) error {
	if p.db == nil || p.jobs == nil || p.threads == nil || p.messages == nil || p.turns == nil {
		return nil
	}
	if jc == nil || jc.Job == nil || threadID == uuid.Nil || userMsgID == uuid.Nil {
		return nil
	}
	owner := jc.Job.OwnerUserID
	if owner == uuid.Nil {
		return nil
	}

	if p.jobRuns != nil {
		dbc := dbctx.Context{Ctx: jc.Ctx, Tx: p.db}
		has, _ := p.jobRuns.HasRunnableForEntity(dbc, owner, "chat_thread", threadID, "chat_respond")
		if has {
			return nil
		}
	}

	var userMsg types.ChatMessage
	if err := p.db.WithContext(jc.Ctx).
		Model(&types.ChatMessage{}).
		Where("id = ? AND thread_id = ? AND user_id = ?", userMsgID, threadID, owner).
		First(&userMsg).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}

	var (
		asstMsg *types.ChatMessage
		jobID   uuid.UUID
	)

	err := p.db.WithContext(jc.Ctx).Transaction(func(tx *gorm.DB) error {
		inner := dbctx.Context{Ctx: jc.Ctx, Tx: tx}
		th, err := p.threads.LockByID(inner, threadID)
		if err != nil {
			return err
		}
		if th == nil || th.ID == uuid.Nil || th.UserID != owner {
			return fmt.Errorf("thread not found")
		}

		// If newer messages arrived, let the most recent message drive the reply.
		if th.NextSeq >= userMsg.Seq+1 {
			return nil
		}

		now := time.Now().UTC()
		turnID := uuid.New()
		seqUser := userMsg.Seq
		seqAsst := seqUser + 1

		asstMsg = &types.ChatMessage{
			ID:        uuid.New(),
			ThreadID:  threadID,
			UserID:    owner,
			Seq:       seqAsst,
			Role:      "assistant",
			Status:    "streaming",
			Content:   "",
			Metadata:  datatypes.JSON([]byte(`{}`)),
			CreatedAt: now,
			UpdatedAt: now,
		}
		if _, err := p.messages.Create(inner, []*types.ChatMessage{asstMsg}); err != nil {
			return err
		}
		if err := p.threads.UpdateFields(inner, threadID, map[string]interface{}{
			"next_seq":        seqAsst,
			"last_message_at": now,
			"updated_at":      now,
		}); err != nil {
			return err
		}

		payload := map[string]any{
			"thread_id":            threadID.String(),
			"user_message_id":      userMsg.ID.String(),
			"assistant_message_id": asstMsg.ID.String(),
			"turn_id":              turnID.String(),
		}
		entityID := threadID
		job, err := p.jobs.Enqueue(inner, owner, "chat_respond", "chat_thread", &entityID, payload)
		if err != nil {
			return err
		}
		if job == nil || job.ID == uuid.Nil {
			return fmt.Errorf("enqueue chat_respond failed")
		}
		jobID = job.ID

		if err := p.turns.Create(inner, &types.ChatTurn{
			ID:                 turnID,
			UserID:             owner,
			ThreadID:           threadID,
			UserMessageID:      userMsg.ID,
			AssistantMessageID: asstMsg.ID,
			JobID:              &jobID,
			Status:             "queued",
			Attempt:            0,
			RetrievalTrace:     datatypes.JSON([]byte(`{}`)),
			CreatedAt:          now,
			UpdatedAt:          now,
		}); err != nil {
			return err
		}

		meta := map[string]any{
			"job_id":  jobID.String(),
			"turn_id": turnID.String(),
		}
		if b, err := json.Marshal(meta); err == nil {
			_ = p.messages.UpdateFields(inner, asstMsg.ID, map[string]any{
				"metadata":   datatypes.JSON(b),
				"updated_at": now,
			})
			asstMsg.Metadata = datatypes.JSON(b)
		}

		return nil
	})
	if err != nil {
		return err
	}

	if jobID != uuid.Nil {
		_ = p.jobs.Dispatch(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, jobID)
	}

	if p.notify != nil && asstMsg != nil {
		p.notify.MessageCreated(owner, threadID, asstMsg, map[string]any{
			"job_id": jobID.String(),
		})
	}

	return nil
}

func parseWaitpointEnvelope(raw datatypes.JSON) *jobrt.WaitpointEnvelope {
	if len(raw) == 0 {
		return nil
	}
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return nil
	}
	var env jobrt.WaitpointEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil
	}
	return &env
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case []byte:
		return strings.TrimSpace(string(t))
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func defaultString(v string, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func buildWaitpointDecisionTrace(
	scope string,
	userID uuid.UUID,
	threadID uuid.UUID,
	userMsg *types.ChatMessage,
	waitJob *types.JobRun,
	env *jobrt.WaitpointEnvelope,
	decision waitpoint.Decision,
	cr waitpoint.ClassifierResult,
	pipeline string,
) *types.DecisionTrace {
	if userID == uuid.Nil || threadID == uuid.Nil || env == nil {
		return nil
	}
	now := time.Now().UTC()
	inputs := map[string]any{
		"pipeline":       pipeline,
		"scope":          scope,
		"thread_id":      threadID.String(),
		"waitpoint_kind": strings.TrimSpace(env.Waitpoint.Kind),
		"waitpoint_step": strings.TrimSpace(env.Waitpoint.Step),
	}
	if userMsg != nil {
		inputs["user_message_id"] = userMsg.ID.String()
		inputs["user_message_seq"] = userMsg.Seq
	}
	if waitJob != nil {
		inputs["waitpoint_job_id"] = waitJob.ID.String()
		inputs["waitpoint_job_type"] = waitJob.JobType
		inputs["waitpoint_job_stage"] = waitJob.Stage
	}
	if env.Data != nil {
		if v := env.Data["path_id"]; v != nil {
			inputs["path_id"] = strings.TrimSpace(fmt.Sprint(v))
		}
		if v := env.Data["path_node_id"]; v != nil {
			inputs["path_node_id"] = strings.TrimSpace(fmt.Sprint(v))
		}
		if v := env.Data["options"]; v != nil {
			inputs["options_present"] = true
		}
	}

	candidates := waitpointCandidates(env)

	chosen := map[string]any{
		"decision":   decision.Kind,
		"case":       cr.Case,
		"confidence": cr.Confidence,
	}
	if cr.Selected != "" {
		chosen["selected_mode"] = cr.Selected
	}
	if cr.CommitType != "" {
		chosen["commit_type"] = cr.CommitType
	}
	if cr.ConfirmedAction != "" {
		chosen["confirmed_action"] = cr.ConfirmedAction
	}
	if cr.Reason != "" {
		chosen["reason"] = cr.Reason
	}
	if decision.Selection != nil {
		chosen["selection"] = decision.Selection
	}
	if strings.TrimSpace(decision.AssistantMessage) != "" {
		chosen["assistant_message"] = decision.AssistantMessage
	}
	if strings.TrimSpace(decision.ConfirmMessage) != "" {
		chosen["confirm_message"] = decision.ConfirmMessage
	}
	chosen["enqueue_chat_respond"] = decision.EnqueueChatRespond

	pathID := uuid.Nil
	if env.Data != nil {
		pathID = parseUUIDFromAny(env.Data["path_id"])
	}
	var pathPtr *uuid.UUID
	if pathID != uuid.Nil {
		pathPtr = &pathID
	}

	trace := &types.DecisionTrace{
		ID:            uuid.New(),
		UserID:        userID,
		OccurredAt:    now,
		DecisionType:  "waitpoint_interpret",
		DecisionPhase: "runtime",
		DecisionMode:  "stochastic",
		PathID:        pathPtr,
		Inputs:        datatypes.JSON(mustJSON(inputs)),
		Candidates:    datatypes.JSON(mustJSON(candidates)),
		Chosen:        datatypes.JSON(mustJSON(chosen)),
	}
	return trace
}

func waitpointCandidates(env *jobrt.WaitpointEnvelope) []map[string]any {
	if env == nil {
		return nil
	}
	out := []map[string]any{}
	for _, act := range env.Waitpoint.Actions {
		if strings.TrimSpace(act.ID) == "" {
			continue
		}
		out = append(out, map[string]any{
			"kind":    "action",
			"id":      act.ID,
			"label":   act.Label,
			"token":   act.Token,
			"variant": act.Variant,
		})
	}
	if env.Data != nil {
		if raw := env.Data["options"]; raw != nil {
			switch t := raw.(type) {
			case []any:
				for _, opt := range t {
					out = append(out, map[string]any{
						"kind":  "option",
						"value": opt,
					})
				}
			case []map[string]any:
				for _, opt := range t {
					out = append(out, map[string]any{
						"kind":  "option",
						"value": opt,
					})
				}
			}
		}
	}
	return out
}

func parseUUIDFromAny(v any) uuid.UUID {
	switch t := v.(type) {
	case uuid.UUID:
		return t
	case string:
		if id, err := uuid.Parse(strings.TrimSpace(t)); err == nil {
			return id
		}
	case []byte:
		if id, err := uuid.Parse(strings.TrimSpace(string(t))); err == nil {
			return id
		}
	default:
		if s := strings.TrimSpace(fmt.Sprint(v)); s != "" {
			if id, err := uuid.Parse(s); err == nil {
				return id
			}
		}
	}
	return uuid.Nil
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

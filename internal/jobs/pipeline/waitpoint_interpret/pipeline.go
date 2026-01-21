package waitpoint_interpret

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
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/waitpoint"
	waitcfg "github.com/yungbote/neurobridge-backend/internal/waitpoint/configs"
)

type orchestratorProbe struct {
	Waiting *struct {
		Kind       string `json:"kind"`
		Stage      string `json:"stage"`
		ChildJobID string `json:"child_job_id"`
	} `json:"waiting,omitempty"`

	Stages map[string]struct {
		ChildJobID string `json:"child_job_id,omitempty"`
	} `json:"stages,omitempty"`
}

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	if p == nil || p.db == nil || p.log == nil || p.ai == nil ||
		p.threads == nil || p.messages == nil || p.turns == nil || p.jobRuns == nil || p.jobs == nil {
		jc.Fail("validate", fmt.Errorf("waitpoint_interpret: missing dependencies"))
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

	// ─────────────────────────────────────────────────────────────
	// Load thread
	// ─────────────────────────────────────────────────────────────

	thRows, err := p.threads.GetByIDs(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, []uuid.UUID{threadID})
	if err != nil || len(thRows) == 0 || thRows[0] == nil || thRows[0].UserID != userID {
		jc.Succeed("done", map[string]any{"mode": "thread_not_found"})
		return nil
	}
	th := thRows[0]
	if th.JobID == nil || *th.JobID == uuid.Nil {
		jc.Succeed("done", map[string]any{"mode": "no_thread_job"})
		return nil
	}

	// ─────────────────────────────────────────────────────────────
	// Load triggering user message
	// ─────────────────────────────────────────────────────────────

	var userMsg types.ChatMessage
	if err := p.db.WithContext(jc.Ctx).
		Model(&types.ChatMessage{}).
		Where(
			"id = ? AND thread_id = ? AND user_id = ? AND role = ? AND deleted_at IS NULL",
			userMsgID, threadID, userID, "user",
		).
		First(&userMsg).Error; err != nil {
		jc.Succeed("done", map[string]any{"mode": "user_message_not_found"})
		return nil
	}

	// ─────────────────────────────────────────────────────────────
	// Load parent learning_build job
	// ─────────────────────────────────────────────────────────────

	jRows, err := p.jobRuns.GetByIDs(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, []uuid.UUID{*th.JobID})
	if err != nil || len(jRows) == 0 || jRows[0] == nil {
		jc.Succeed("done", map[string]any{"mode": "build_job_not_found"})
		return nil
	}
	build := jRows[0]

	if build.OwnerUserID != userID ||
		!strings.EqualFold(strings.TrimSpace(build.JobType), "learning_build") {
		jc.Succeed("done", map[string]any{"mode": "not_learning_build"})
		return nil
	}

	if !strings.EqualFold(strings.TrimSpace(build.Status), "waiting_user") {
		jc.Succeed("done", map[string]any{"mode": "build_not_waiting"})
		return nil
	}

	// ─────────────────────────────────────────────────────────────
	// Find blocking child job from orchestrator state
	// ─────────────────────────────────────────────────────────────

	childJobID := uuid.Nil
	stageName := pausedStageFromJobStage(build.Stage)

	var probe orchestratorProbe
	if len(build.Result) > 0 && strings.TrimSpace(string(build.Result)) != "" &&
		strings.TrimSpace(string(build.Result)) != "null" {
		_ = json.Unmarshal(build.Result, &probe)
	}

	if probe.Waiting != nil &&
		strings.EqualFold(strings.TrimSpace(probe.Waiting.Kind), "child_waitpoint") {
		stageName = strings.TrimSpace(probe.Waiting.Stage)
		if id, e := uuid.Parse(strings.TrimSpace(probe.Waiting.ChildJobID)); e == nil {
			childJobID = id
		}
	}

	if childJobID == uuid.Nil && probe.Stages != nil {
		if stageName != "" {
			if ss, ok := probe.Stages[stageName]; ok {
				if id, e := uuid.Parse(strings.TrimSpace(ss.ChildJobID)); e == nil {
					childJobID = id
				}
			}
		}
	}

	if childJobID == uuid.Nil {
		jc.Succeed("done", map[string]any{"mode": "no_child_waitpoint"})
		return nil
	}

	// ─────────────────────────────────────────────────────────────
	// Load child job (paused stage)
	// ─────────────────────────────────────────────────────────────

	cRows, err := p.jobRuns.GetByIDs(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, []uuid.UUID{childJobID})
	if err != nil || len(cRows) == 0 || cRows[0] == nil {
		jc.Succeed("done", map[string]any{"mode": "child_job_not_found"})
		return nil
	}
	child := cRows[0]

	if !strings.EqualFold(strings.TrimSpace(child.Status), "waiting_user") {
		jc.Succeed("done", map[string]any{"mode": "child_not_waiting"})
		return nil
	}

	// ─────────────────────────────────────────────────────────────
	// Decode waitpoint envelope
	// ─────────────────────────────────────────────────────────────

	env := parseWaitpointEnvelope(child.Result)
	if env == nil || strings.TrimSpace(env.Waitpoint.Kind) == "" {
		jc.Succeed("done", map[string]any{"mode": "no_waitpoint_envelope"})
		return nil
	}

	// Idempotency
	if env.State.LastUserMessageID == userMsg.ID.String() ||
		(env.State.LastUserSeqHandled > 0 && userMsg.Seq <= env.State.LastUserSeqHandled) {
		jc.Succeed("done", map[string]any{"mode": "already_handled"})
		return nil
	}

	// ─────────────────────────────────────────────────────────────
	// Load recent messages for context
	// ─────────────────────────────────────────────────────────────

	msgs, _ := p.messages.ListByThread(
		dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB},
		threadID,
		200,
	)

	// ─────────────────────────────────────────────────────────────
	// Run waitpoint interpreter
	// ─────────────────────────────────────────────────────────────

	reg := waitpoint.NewRegistry()
	_ = reg.Register(waitcfg.PathIntakeStructureConfig())

	interp := waitpoint.NewInterpreter(reg)

	ic := &waitpoint.InterpreterContext{
		Ctx:         jc.Ctx,
		UserID:      userID,
		ThreadID:    threadID,
		Thread:      th,
		UserMessage: &userMsg,
		ParentJob:   build,
		ChildJob:    child,
		Envelope:    env,
		Messages:    msgs,
		AI:          p.ai,
	}

	decision, cr, ierr := interp.Run(ic)
	if ierr != nil {
		jc.Fail("interpret", ierr)
		return nil
	}

	// Update interpreter state
	env.State.Version = 1
	env.State.LastUserMessageID = userMsg.ID.String()
	env.State.LastUserSeqHandled = userMsg.Seq
	env.State.LastCase = string(cr.Case)
	env.State.LastConfidence = cr.Confidence
	env.State.Attempts++

	// ─────────────────────────────────────────────────────────────
	// Apply decision
	// ─────────────────────────────────────────────────────────────

	switch decision.Kind {

	case waitpoint.DecisionContinueChat:
		_ = p.persistChildEnvelope(jc, child.ID, env)
		if decision.EnqueueChatRespond {
			_ = p.enqueueChatRespondForMessage(jc, threadID, userMsg.ID)
		}
		jc.Succeed("done", map[string]any{"case": cr.Case, "decision": decision.Kind})
		return nil

	case waitpoint.DecisionAskClarify:
		if decision.Selection != nil {
			if pg, ok := decision.Selection["pending_guess"]; ok {
				env.State.Phase = "awaiting_confirmation"
				env.State.PendingGuess = strings.TrimSpace(fmt.Sprint(pg))
			}
		}
		_ = p.persistChildEnvelope(jc, child.ID, env)
		if strings.TrimSpace(decision.AssistantMessage) != "" {
			_ = p.appendAssistantMessage(
				jc,
				threadID,
				userID,
				decision.AssistantMessage,
				map[string]any{
					"kind":           "waitpoint_clarification",
					"waitpoint_kind": env.Waitpoint.Kind,
					"child_job_id":   child.ID.String(),
				},
			)
		}
		jc.Succeed("done", map[string]any{"case": cr.Case, "decision": decision.Kind})
		return nil

	case waitpoint.DecisionConfirmResume:
		if decision.Selection != nil {
			_ = p.applyPathIntakeSelection(jc, env, decision.Selection)
		}
		_ = p.persistChildEnvelope(jc, child.ID, env)
		if strings.TrimSpace(decision.ConfirmMessage) != "" {
			_ = p.appendAssistantMessage(
				jc,
				threadID,
				userID,
				decision.ConfirmMessage,
				map[string]any{
					"kind":           "waitpoint_confirm",
					"waitpoint_kind": env.Waitpoint.Kind,
					"child_job_id":   child.ID.String(),
				},
			)
		}
		if err := p.resumeJobs(jc, userID, build.ID, child.ID); err != nil {
			jc.Fail("resume", err)
			return nil
		}
		if p.jobs != nil {
			_ = p.jobs.Dispatch(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, build.ID)
		}
		jc.Succeed("done", map[string]any{"case": cr.Case, "decision": decision.Kind})
		return nil

	default:
		_ = p.persistChildEnvelope(jc, child.ID, env)
		_ = p.enqueueChatRespondForMessage(jc, threadID, userMsg.ID)
		jc.Succeed("done", map[string]any{"case": cr.Case, "decision": decision.Kind})
		return nil
	}
}

// ─────────────────────────────────────────────────────────────
// Helper functions
// ─────────────────────────────────────────────────────────────

// pausedStageFromJobStage extracts the paused stage name from job.Stage.
// Format: "waiting_user_{stage}" -> "{stage}"
func pausedStageFromJobStage(stage string) string {
	s := strings.ToLower(strings.TrimSpace(stage))
	if strings.HasPrefix(s, "waiting_user_") {
		return strings.TrimSpace(strings.TrimPrefix(s, "waiting_user_"))
	}
	return ""
}

// parseWaitpointEnvelope decodes the waitpoint envelope from job result JSON.
func parseWaitpointEnvelope(raw datatypes.JSON) *jobrt.WaitpointEnvelope {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" ||
		strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	var env jobrt.WaitpointEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil
	}
	return &env
}

// persistChildEnvelope updates the child job's result with the modified envelope.
func (p *Pipeline) persistChildEnvelope(
	jc *jobrt.Context,
	childJobID uuid.UUID,
	env *jobrt.WaitpointEnvelope,
) error {
	if p.db == nil || childJobID == uuid.Nil || env == nil {
		return nil
	}
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return p.db.WithContext(jc.Ctx).
		Model(&types.JobRun{}).
		Where("id = ?", childJobID).
		Updates(map[string]interface{}{
			"result":     datatypes.JSON(b),
			"updated_at": now,
		}).Error
}

// appendAssistantMessage creates a new assistant message in the thread.
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

	// Get thread to find next seq
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

	// Notify client
	if p.notify != nil {
		p.notify.MessageCreated(userID, threadID, msg, meta)
	}

	return nil
}

// resumeJobs transitions child and parent jobs from waiting_user to queued.
func (p *Pipeline) resumeJobs(
	jc *jobrt.Context,
	userID uuid.UUID,
	parentJobID uuid.UUID,
	childJobID uuid.UUID,
) error {
	if p.db == nil {
		return nil
	}
	now := time.Now().UTC()

	// Resume child job first
	if childJobID != uuid.Nil {
		if err := p.db.WithContext(jc.Ctx).
			Model(&types.JobRun{}).
			Where("id = ? AND status = ?", childJobID, "waiting_user").
			Updates(map[string]interface{}{
				"status":       "queued",
				"stage":        "queued",
				"message":      "Queued",
				"locked_at":    nil,
				"updated_at":   now,
				"heartbeat_at": now,
				"error":        "",
			}).Error; err != nil {
			return err
		}
	}

	// Resume parent job
	if parentJobID != uuid.Nil {
		if err := p.db.WithContext(jc.Ctx).
			Model(&types.JobRun{}).
			Where("id = ? AND status = ?", parentJobID, "waiting_user").
			Updates(map[string]interface{}{
				"status":       "queued",
				"stage":        "queued",
				"message":      "Queued",
				"locked_at":    nil,
				"updated_at":   now,
				"heartbeat_at": now,
				"error":        "",
			}).Error; err != nil {
			return err
		}
	}

	return nil
}

// applyPathIntakeSelection applies domain-based selection to path metadata.
func (p *Pipeline) applyPathIntakeSelection(
	jc *jobrt.Context,
	env *jobrt.WaitpointEnvelope,
	selection map[string]any,
) error {
	if p.path == nil || env == nil || env.Data == nil {
		return nil
	}

	pathIDStr, _ := env.Data["path_id"].(string)
	pathID, err := uuid.Parse(strings.TrimSpace(pathIDStr))
	if err != nil || pathID == uuid.Nil {
		return nil
	}

	dbc := dbctx.Context{Ctx: jc.Ctx, Tx: p.db}
	path, err := p.path.GetByID(dbc, pathID)
	if err != nil || path == nil {
		return err
	}

	// Parse existing metadata
	meta := map[string]any{}
	if len(path.Metadata) > 0 && string(path.Metadata) != "null" {
		_ = json.Unmarshal(path.Metadata, &meta)
	}

	intake, _ := meta["intake"].(map[string]any)
	if intake == nil {
		intake = map[string]any{}
	}

	// Apply selection
	if commitType, ok := selection["commit_type"].(string); ok {
		ct := strings.ToLower(strings.TrimSpace(commitType))
		intake["paths_confirmation_type"] = ct
		if ct == "confirm" {
			intake["paths_confirmed"] = true
		} else if ct == "change" {
			intake["paths_confirmed"] = false
		}
		intake["paths_confirmed_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if paths, ok := selection["paths"]; ok {
		intake["paths"] = paths
	}

	intake["needs_clarification"] = false
	meta["intake"] = intake
	meta["intake_updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)

	metaJSON, _ := json.Marshal(meta)
	return p.path.UpdateFields(dbc, pathID, map[string]interface{}{
		"metadata": datatypes.JSON(metaJSON),
	})
}

// enqueueChatRespondForMessage enqueues a chat_respond job for continued conversation.
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

	// Check if already has runnable chat_respond
	if p.jobRuns != nil {
		dbc := dbctx.Context{Ctx: jc.Ctx, Tx: p.db}
		has, _ := p.jobRuns.HasRunnableForEntity(
			dbc, owner, "chat_thread", threadID, "chat_respond",
		)
		if has {
			return nil // Already has one pending
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
		if userMsg.Seq < th.NextSeq {
			return nil
		}

		if existing, _ := p.turns.GetByUserMessageID(inner, owner, threadID, userMsgID); existing != nil && existing.ID != uuid.Nil {
			return nil
		}

		now := time.Now().UTC()
		seq := th.NextSeq + 1

		msg := &types.ChatMessage{
			ID:        uuid.New(),
			ThreadID:  threadID,
			UserID:    owner,
			Seq:       seq,
			Role:      "assistant",
			Status:    "streaming",
			Content:   "",
			Metadata:  datatypes.JSON([]byte(`{}`)),
			CreatedAt: now,
			UpdatedAt: now,
		}
		if _, err := p.messages.Create(inner, []*types.ChatMessage{msg}); err != nil {
			return err
		}

		if err := p.threads.UpdateFields(inner, threadID, map[string]interface{}{
			"next_seq":        seq,
			"last_message_at": now,
			"updated_at":      now,
		}); err != nil {
			return err
		}

		turnID := uuid.New()
		payload := map[string]any{
			"thread_id":            threadID.String(),
			"user_message_id":      userMsgID.String(),
			"assistant_message_id": msg.ID.String(),
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

		turn := &types.ChatTurn{
			ID:                 turnID,
			UserID:             owner,
			ThreadID:           threadID,
			UserMessageID:      userMsgID,
			AssistantMessageID: msg.ID,
			JobID:              &jobID,
			Status:             "queued",
			Attempt:            0,
			RetrievalTrace:     datatypes.JSON([]byte(`{}`)),
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		if err := p.turns.Create(inner, turn); err != nil {
			return err
		}

		meta := map[string]any{
			"job_id":  jobID.String(),
			"turn_id": turnID.String(),
		}
		if b, err := json.Marshal(meta); err == nil {
			_ = p.messages.UpdateFields(inner, msg.ID, map[string]interface{}{
				"metadata":   datatypes.JSON(b),
				"updated_at": now,
			})
			msg.Metadata = datatypes.JSON(b)
		}

		asstMsg = msg
		return nil
	})
	if err != nil {
		return err
	}

	if jobID != uuid.Nil {
		_ = p.jobs.Dispatch(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, jobID)
	}
	if asstMsg != nil && p.notify != nil {
		meta := map[string]any{}
		if len(asstMsg.Metadata) > 0 {
			_ = json.Unmarshal(asstMsg.Metadata, &meta)
		}
		p.notify.MessageCreated(owner, threadID, asstMsg, meta)
	}
	return nil
}



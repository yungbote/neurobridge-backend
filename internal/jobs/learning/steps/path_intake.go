package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type PathIntakeDeps struct {
	DB        *gorm.DB
	Log       *logger.Logger
	Files     repos.MaterialFileRepo
	Chunks    repos.MaterialChunkRepo
	Summaries repos.MaterialSetSummaryRepo
	Path      repos.PathRepo
	Prefs     repos.UserPersonalizationPrefsRepo
	Threads   repos.ChatThreadRepo
	Messages  repos.ChatMessageRepo
	AI        openai.Client
	Notify    services.ChatNotifier
	Bootstrap services.LearningBuildBootstrapService
}

type PathIntakeInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
	ThreadID      uuid.UUID
	JobID         uuid.UUID

	// WaitForUser controls whether we are allowed to pause for user answers.
	// In child-mode learning_build, this should be true; in inline/dev flows it can be false.
	WaitForUser bool
}

type PathIntakeOutput struct {
	PathID   uuid.UUID      `json:"path_id"`
	ThreadID uuid.UUID      `json:"thread_id"`
	Status   string         `json:"status"` // "succeeded" | "waiting_user"
	Intake   map[string]any `json:"intake,omitempty"`
	Meta     map[string]any `json:"meta,omitempty"`
	Now      string         `json:"now,omitempty"`
}

func PathIntake(ctx context.Context, deps PathIntakeDeps, in PathIntakeInput) (PathIntakeOutput, error) {
	out := PathIntakeOutput{Status: "succeeded"}
	if deps.DB == nil || deps.Log == nil || deps.Files == nil || deps.Summaries == nil || deps.Path == nil || deps.Bootstrap == nil || deps.AI == nil {
		return out, fmt.Errorf("path_intake: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("path_intake: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("path_intake: missing material_set_id")
	}

	pathID := in.PathID
	if pathID == uuid.Nil {
		pid, err := deps.Bootstrap.EnsurePath(dbctx.Context{Ctx: ctx}, in.OwnerUserID, in.MaterialSetID)
		if err != nil {
			return out, err
		}
		pathID = pid
	}
	out.PathID = pathID
	out.ThreadID = in.ThreadID
	out.Now = time.Now().UTC().Format(time.RFC3339Nano)

	files, err := deps.Files.GetByMaterialSetIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{in.MaterialSetID})
	if err != nil {
		return out, err
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i] == nil || files[j] == nil {
			return false
		}
		return files[i].OriginalName < files[j].OriginalName
	})

	// Optional: include a small amount of raw text context to improve intake quality.
	fileIDs := make([]uuid.UUID, 0, len(files))
	for _, f := range files {
		if f != nil && f.ID != uuid.Nil {
			fileIDs = append(fileIDs, f.ID)
		}
	}
	var chunks []*types.MaterialChunk
	if deps.Chunks != nil && len(fileIDs) > 0 {
		if rows, err := deps.Chunks.GetByMaterialFileIDs(dbctx.Context{Ctx: ctx}, fileIDs); err == nil {
			chunks = rows
		}
	}

	// Optional: include user-controlled personalization prefs (accessibility/pace/etc).
	var prefsAny any
	if deps.Prefs != nil && in.OwnerUserID != uuid.Nil {
		if row, err := deps.Prefs.GetByUserID(dbctx.Context{Ctx: ctx}, in.OwnerUserID); err == nil && row != nil && len(row.PrefsJSON) > 0 && string(row.PrefsJSON) != "null" {
			_ = json.Unmarshal(row.PrefsJSON, &prefsAny)
		}
	}

	var summary *types.MaterialSetSummary
	if rows, err := deps.Summaries.GetByMaterialSetIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{in.MaterialSetID}); err == nil && len(rows) > 0 {
		summary = rows[0]
	}

	// If we don't have a thread to converse in, do not block the build.
	if in.ThreadID == uuid.Nil || deps.Threads == nil || deps.Messages == nil {
		intake := buildFallbackIntake(files, summary, "", "")
		_ = writePathIntakeMeta(ctx, deps, pathID, intake, nil)
		out.Intake = intake
		return out, nil
	}

	messages, _ := deps.Messages.ListByThread(dbctx.Context{Ctx: ctx}, in.ThreadID, 300)
	qMsg := latestIntakeQuestionsMessage(messages)
	if qMsg != nil {
		answer := userAnswerAfter(messages, qMsg.Seq)
		if strings.TrimSpace(answer) == "" {
			if !in.WaitForUser {
				// Non-interactive mode: proceed with assumptions and do not re-ask.
				intake := buildFallbackIntake(files, summary, userContextBefore(messages, qMsg.Seq), "")
				_ = writePathIntakeMeta(ctx, deps, pathID, intake, nil)
				out.Intake = intake
				return out, nil
			}
			out.Status = "waiting_user"
			out.Meta = map[string]any{
				"reason":       "awaiting_user_answer",
				"question_seq": qMsg.Seq,
				"question_id":  qMsg.ID.String(),
			}
			return out, nil
		}

		intake, intakeMD, err := generateIntake(ctx, deps, files, chunks, summary, prefsAny, userContextBefore(messages, qMsg.Seq), answer, true)
		if err != nil {
			deps.Log.Warn("path_intake: generate (with answers) failed; proceeding with fallback", "error", err)
			intake = buildFallbackIntake(files, summary, userContextBefore(messages, qMsg.Seq), answer)
		}
		_ = writePathIntakeMeta(ctx, deps, pathID, intake, map[string]any{"intake_md": intakeMD})
		_ = maybeAppendIntakeAckMessage(ctx, deps, in.OwnerUserID, in.ThreadID, in.JobID, in.MaterialSetID, pathID, intake, intakeMD)
		out.Intake = intake
		return out, nil
	}

	userCtx := userContextBefore(messages, 1<<30)

	intake, intakeMD, err := generateIntake(ctx, deps, files, chunks, summary, prefsAny, userCtx, "", false)
	if err != nil {
		deps.Log.Warn("path_intake: generate failed; proceeding with fallback", "error", err)
		intake = buildFallbackIntake(files, summary, userCtx, "")
		_ = writePathIntakeMeta(ctx, deps, pathID, intake, nil)
		out.Intake = intake
		return out, nil
	}

	needs := boolFromAny(intake["needs_clarification"])
	questions := sliceAny(intake["clarifying_questions"])

	if needs && len(questions) > 0 && in.WaitForUser {
		content := formatIntakeQuestionsMD(intake, intakeMD)
		created, err := appendIntakeQuestionsMessage(ctx, deps, in.OwnerUserID, in.ThreadID, in.JobID, in.MaterialSetID, pathID, content)
		if err != nil {
			// If we can't ask, don't block.
			deps.Log.Warn("path_intake: failed to post clarifying questions; proceeding", "error", err)
		} else if created != nil {
			out.Status = "waiting_user"
			out.Meta = map[string]any{
				"reason":       "clarifying_questions_posted",
				"question_id":  created.ID.String(),
				"question_seq": created.Seq,
			}
			out.Intake = intake
			return out, nil
		}
	}

	// Non-blocking: store intake as-is and proceed.
	_ = writePathIntakeMeta(ctx, deps, pathID, intake, map[string]any{"intake_md": intakeMD})
	out.Intake = intake
	return out, nil
}

func boolFromAny(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s == "true" || s == "1" || s == "yes" || s == "y"
	default:
		s := strings.ToLower(strings.TrimSpace(stringFromAny(v)))
		return s == "true" || s == "1" || s == "yes" || s == "y"
	}
}

func sliceAny(v any) []any {
	if v == nil {
		return nil
	}
	if arr, ok := v.([]any); ok {
		return arr
	}
	return nil
}

func messageKind(m *types.ChatMessage) string {
	if m == nil || len(m.Metadata) == 0 || strings.TrimSpace(string(m.Metadata)) == "" || strings.TrimSpace(string(m.Metadata)) == "null" {
		return ""
	}
	var meta map[string]any
	if err := json.Unmarshal(m.Metadata, &meta); err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(stringFromAny(meta["kind"])))
}

func latestIntakeQuestionsMessage(msgs []*types.ChatMessage) *types.ChatMessage {
	var best *types.ChatMessage
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if messageKind(m) != "path_intake_questions" {
			continue
		}
		if best == nil || m.Seq > best.Seq {
			best = m
		}
	}
	return best
}

func userAnswerAfter(msgs []*types.ChatMessage, afterSeq int64) string {
	var parts []string
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if int64(m.Seq) <= afterSeq {
			continue
		}
		if strings.ToLower(strings.TrimSpace(m.Role)) != "user" {
			continue
		}
		txt := strings.TrimSpace(m.Content)
		if txt == "" {
			continue
		}
		parts = append(parts, txt)
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func userContextBefore(msgs []*types.ChatMessage, beforeSeq int64) string {
	var parts []string
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if int64(m.Seq) >= beforeSeq {
			continue
		}
		if strings.ToLower(strings.TrimSpace(m.Role)) != "user" {
			continue
		}
		txt := strings.TrimSpace(m.Content)
		if txt == "" {
			continue
		}
		parts = append(parts, txt)
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func generateIntake(
	ctx context.Context,
	deps PathIntakeDeps,
	files []*types.MaterialFile,
	chunks []*types.MaterialChunk,
	summary *types.MaterialSetSummary,
	prefsAny any,
	userContext string,
	userAnswers string,
	isFollowup bool,
) (map[string]any, string, error) {
	fileItems := make([]map[string]any, 0, len(files))
	for _, f := range files {
		if f == nil {
			continue
		}
		fileItems = append(fileItems, map[string]any{
			"file_id":        f.ID.String(),
			"original_name":  f.OriginalName,
			"mime_type":      f.MimeType,
			"size_bytes":     f.SizeBytes,
			"extracted_kind": f.ExtractedKind,
		})
	}
	filesJSON, _ := json.Marshal(map[string]any{"files": fileItems})

	sumMD := ""
	subject := ""
	level := ""
	var tags []string
	var conceptKeys []string
	if summary != nil {
		sumMD = strings.TrimSpace(summary.SummaryMD)
		subject = strings.TrimSpace(summary.Subject)
		level = strings.TrimSpace(summary.Level)
		_ = json.Unmarshal(summary.Tags, &tags)
		_ = json.Unmarshal(summary.ConceptKeys, &conceptKeys)
	}
	tags = dedupeStrings(tags)
	conceptKeys = dedupeStrings(conceptKeys)

	system := strings.TrimSpace(strings.Join([]string{
		"You are an expert learning designer and curriculum planner.",
		"Given a set of uploaded study materials and any user-provided context, infer what each file is trying to teach and the combined learning goal.",
		"Only ask clarifying questions when needed to build a high-quality learning path; keep questions minimal, actionable, and non-redundant.",
		"Prefer asking about goal, deadline, current level, and prioritization when unclear or divergent.",
		"Never mention policy or hidden reasoning. Output must match the JSON schema exactly.",
	}, "\n"))

	var user strings.Builder
	user.WriteString("USER_CONTEXT:\n")
	if strings.TrimSpace(userContext) == "" {
		user.WriteString("(none)\n")
	} else {
		user.WriteString(userContext)
		user.WriteString("\n")
	}

	if strings.TrimSpace(userAnswers) != "" {
		user.WriteString("\nUSER_ANSWERS:\n")
		user.WriteString(userAnswers)
		user.WriteString("\n")
	}

	user.WriteString("\nMATERIAL_SET_SUMMARY_MD:\n")
	if sumMD == "" {
		user.WriteString("(not available)\n")
	} else {
		user.WriteString(sumMD)
		user.WriteString("\n")
	}

	user.WriteString("\nSUMMARY_METADATA:\n")
	user.WriteString(fmt.Sprintf("- subject: %s\n", stringsOr(subject, "(unknown)")))
	user.WriteString(fmt.Sprintf("- level: %s\n", stringsOr(level, "(unknown)")))
	if len(tags) > 0 {
		user.WriteString(fmt.Sprintf("- tags: %s\n", strings.Join(tags, ", ")))
	}
	if len(conceptKeys) > 0 {
		user.WriteString(fmt.Sprintf("- concept_keys: %s\n", strings.Join(conceptKeys, ", ")))
	}

	user.WriteString("\nFILES_JSON:\n")
	user.WriteString(string(filesJSON))
	user.WriteString("\n")

	if prefsAny != nil {
		prefsJSON, _ := json.Marshal(prefsAny)
		if len(prefsJSON) > 0 && string(prefsJSON) != "null" {
			user.WriteString("\nUSER_PERSONALIZATION_PREFS_JSON:\n")
			user.WriteString(string(prefsJSON))
			user.WriteString("\n")
		}
	}

	excerpts := buildIntakeMaterialExcerpts(files, chunks)
	if strings.TrimSpace(excerpts) != "" {
		user.WriteString("\nMATERIAL_EXCERPTS (ground truth snippets; may be incomplete):\n")
		user.WriteString(excerpts)
		user.WriteString("\n")
	}

	if isFollowup {
		user.WriteString("\nNOTE: This is a follow-up pass after the user answered questions; do not ask more questions unless absolutely necessary.\n")
	}

	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"file_intents": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"file_id":          map[string]any{"type": "string"},
						"original_name":    map[string]any{"type": "string"},
						"aim":              map[string]any{"type": "string"},
						"topics":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"confidence":       map[string]any{"type": "number"},
						"uncertainty_note": map[string]any{"type": "string"},
					},
					"required": []string{"file_id", "original_name", "aim", "topics", "confidence", "uncertainty_note"},
				},
			},
			"combined_goal":        map[string]any{"type": "string"},
			"learning_intent": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"goal_kind": map[string]any{
						"type": "string",
						"enum": []any{"exam", "project", "course", "research", "work", "hobby", "other", "unknown"},
					},
					"deadline":          map[string]any{"type": "string"}, // may be empty
					"prior_knowledge":   map[string]any{"type": "string"}, // may be empty
					"priority_topics":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"deprioritize":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"constraints":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"success_criteria":  map[string]any{"type": "string"}, // may be empty
					"plan_notes":        map[string]any{"type": "string"},
					"confidence":        map[string]any{"type": "number"},
					"uncertainty_notes": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
				"required": []string{
					"goal_kind",
					"deadline",
					"prior_knowledge",
					"priority_topics",
					"deprioritize",
					"constraints",
					"success_criteria",
					"plan_notes",
					"confidence",
					"uncertainty_notes",
				},
				"additionalProperties": false,
			},
			"audience_level_guess": map[string]any{"type": "string"},
			"confidence":           map[string]any{"type": "number"},
			"uncertainty_reasons":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"needs_clarification":  map[string]any{"type": "boolean"},
			"clarifying_questions": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"id":       map[string]any{"type": "string"},
						"question": map[string]any{"type": "string"},
						"reason":   map[string]any{"type": "string"},
					},
					"required": []string{"id", "question", "reason"},
				},
			},
			"assumptions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"notes":       map[string]any{"type": "string"},
		},
		"required": []string{
			"file_intents",
			"combined_goal",
			"learning_intent",
			"audience_level_guess",
			"confidence",
			"uncertainty_reasons",
			"needs_clarification",
			"clarifying_questions",
			"assumptions",
			"notes",
		},
	}

	obj, err := deps.AI.GenerateJSON(ctx, system, user.String(), "path_intake", schema)
	if err != nil {
		return nil, "", err
	}

	intakeMD := formatIntakeSummaryMD(obj)
	_ = isFollowup // reserved for future logic; keeps signature stable
	return obj, intakeMD, nil
}

func formatIntakeSummaryMD(intake map[string]any) string {
	if intake == nil {
		return ""
	}
	goal := strings.TrimSpace(stringFromAny(intake["combined_goal"]))
	level := strings.TrimSpace(stringFromAny(intake["audience_level_guess"]))
	intent := mapFromAny(intake["learning_intent"])
	assumptions := stringSliceFromAny(intake["assumptions"])

	lines := make([]string, 0, 8)
	if goal != "" {
		lines = append(lines, "**Goal**: "+goal)
	}
	if intent != nil {
		goalKind := strings.TrimSpace(stringFromAny(intent["goal_kind"]))
		if goalKind != "" && goalKind != "unknown" {
			lines = append(lines, "**Use case**: "+goalKind)
		}
		deadline := strings.TrimSpace(stringFromAny(intent["deadline"]))
		if deadline != "" {
			lines = append(lines, "**Deadline**: "+deadline)
		}
		priorities := dedupeStrings(stringSliceFromAny(intent["priority_topics"]))
		if len(priorities) > 0 {
			p := priorities
			if len(p) > 6 {
				p = p[:6]
			}
			lines = append(lines, "**Focus**: "+strings.Join(p, " • "))
		}
		constraints := dedupeStrings(stringSliceFromAny(intent["constraints"]))
		if len(constraints) > 0 {
			c := constraints
			if len(c) > 3 {
				c = c[:3]
			}
			lines = append(lines, "**Constraints**: "+strings.Join(c, " • "))
		}
	}
	if level != "" && level != "(unknown)" {
		lines = append(lines, "**Level**: "+level)
	}
	if len(assumptions) > 0 {
		a := assumptions
		if len(a) > 4 {
			a = a[:4]
		}
		lines = append(lines, "**Assumptions**: "+strings.Join(a, " • "))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func formatIntakeQuestionsMD(intake map[string]any, intakeMD string) string {
	if intake == nil {
		return "I need a bit more context to generate the best learning path. What’s your goal with these materials?"
	}
	var b strings.Builder
	b.WriteString("I reviewed your upload and I want to make sure I generate the right path.\n\n")
	if strings.TrimSpace(intakeMD) != "" {
		b.WriteString("**My current read**\n")
		b.WriteString(intakeMD)
		b.WriteString("\n\n")
	}

	qs := sliceAny(intake["clarifying_questions"])
	if len(qs) == 0 {
		b.WriteString("**A quick question**\n")
		b.WriteString("What’s your goal with these materials, and is there a deadline?\n\n")
	} else {
		b.WriteString("**A few quick questions**\n")
		for i, q := range qs {
			m, ok := q.(map[string]any)
			if !ok {
				continue
			}
			text := strings.TrimSpace(stringFromAny(m["question"]))
			if text == "" {
				continue
			}
			b.WriteString(fmt.Sprintf("%d) %s\n", i+1, text))
		}
		b.WriteString("\n")
	}

	b.WriteString("Reply in one message. If you’re not sure, say “Make reasonable assumptions” and I’ll proceed.\n")
	return strings.TrimSpace(b.String())
}

func appendIntakeQuestionsMessage(
	ctx context.Context,
	deps PathIntakeDeps,
	owner uuid.UUID,
	threadID uuid.UUID,
	jobID uuid.UUID,
	materialSetID uuid.UUID,
	pathID uuid.UUID,
	content string,
) (*types.ChatMessage, error) {
	if deps.DB == nil || deps.Threads == nil || deps.Messages == nil {
		return nil, fmt.Errorf("missing chat deps")
	}
	if owner == uuid.Nil || threadID == uuid.Nil || jobID == uuid.Nil {
		return nil, fmt.Errorf("missing ids")
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("empty content")
	}

	var created *types.ChatMessage
	createdNew := false

	err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		inner := dbctx.Context{Ctx: ctx, Tx: tx}
		th, err := deps.Threads.LockByID(inner, threadID)
		if err != nil {
			return err
		}
		if th == nil || th.ID == uuid.Nil || th.UserID != owner {
			return fmt.Errorf("thread not found")
		}

		// Idempotency: one questions message per intake job.
		var existing types.ChatMessage
		e := tx.WithContext(ctx).
			Model(&types.ChatMessage{}).
			Where("thread_id = ? AND user_id = ? AND metadata->>'kind' = ? AND metadata->>'job_id' = ?", threadID, owner, "path_intake_questions", jobID.String()).
			First(&existing).Error
		if e == nil && existing.ID != uuid.Nil {
			created = &existing
			return nil
		}
		if e != nil && e != gorm.ErrRecordNotFound {
			return e
		}

		now := time.Now().UTC()
		meta := map[string]any{
			"kind":            "path_intake_questions",
			"job_id":          jobID.String(),
			"path_id":         pathID.String(),
			"material_set_id": materialSetID.String(),
		}
		metaJSON, _ := json.Marshal(meta)

		nextSeq := th.NextSeq + 1
		msg := &types.ChatMessage{
			ID:        uuid.New(),
			ThreadID:  threadID,
			UserID:    owner,
			Seq:       nextSeq,
			Role:      "assistant",
			Status:    "sent",
			Content:   content,
			Model:     "",
			Metadata:  datatypes.JSON(metaJSON),
			CreatedAt: now,
			UpdatedAt: now,
		}
		if _, err := deps.Messages.Create(inner, []*types.ChatMessage{msg}); err != nil {
			return err
		}
		if err := deps.Threads.UpdateFields(inner, threadID, map[string]interface{}{
			"next_seq":        nextSeq,
			"last_message_at": now,
			"updated_at":      now,
		}); err != nil {
			return err
		}

		created = msg
		createdNew = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	if createdNew && created != nil && deps.Notify != nil {
		deps.Notify.MessageCreated(owner, threadID, created, nil)
	}
	return created, nil
}

func maybeAppendIntakeAckMessage(
	ctx context.Context,
	deps PathIntakeDeps,
	owner uuid.UUID,
	threadID uuid.UUID,
	jobID uuid.UUID,
	materialSetID uuid.UUID,
	pathID uuid.UUID,
	intake map[string]any,
	intakeMD string,
) error {
	if deps.DB == nil || deps.Threads == nil || deps.Messages == nil {
		return nil
	}
	if owner == uuid.Nil || threadID == uuid.Nil || jobID == uuid.Nil {
		return nil
	}

	title := strings.TrimSpace(stringFromAny(intake["combined_goal"]))
	if title == "" {
		title = "Got it — generating your path now."
	}
	content := "Thanks — I’ll generate your learning path now."
	if strings.TrimSpace(intakeMD) != "" {
		content = strings.TrimSpace(strings.Join([]string{
			"Thanks — I’ll generate your learning path now.",
			"**Locked in**\n" + intakeMD,
		}, "\n\n"))
	}

	var created *types.ChatMessage
	createdNew := false

	err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		inner := dbctx.Context{Ctx: ctx, Tx: tx}
		th, err := deps.Threads.LockByID(inner, threadID)
		if err != nil {
			return err
		}
		if th == nil || th.ID == uuid.Nil || th.UserID != owner {
			return nil
		}

		// Idempotency: only one ack per intake job.
		var existing types.ChatMessage
		e := tx.WithContext(ctx).
			Model(&types.ChatMessage{}).
			Where("thread_id = ? AND user_id = ? AND metadata->>'kind' = ? AND metadata->>'job_id' = ?", threadID, owner, "path_intake_ack", jobID.String()).
			First(&existing).Error
		if e == nil && existing.ID != uuid.Nil {
			created = &existing
			return nil
		}
		if e != nil && e != gorm.ErrRecordNotFound {
			return e
		}

		now := time.Now().UTC()
		meta := map[string]any{
			"kind":            "path_intake_ack",
			"job_id":          jobID.String(),
			"path_id":         pathID.String(),
			"material_set_id": materialSetID.String(),
		}
		metaJSON, _ := json.Marshal(meta)

		nextSeq := th.NextSeq + 1
		msg := &types.ChatMessage{
			ID:        uuid.New(),
			ThreadID:  threadID,
			UserID:    owner,
			Seq:       nextSeq,
			Role:      "assistant",
			Status:    "sent",
			Content:   content,
			Model:     "",
			Metadata:  datatypes.JSON(metaJSON),
			CreatedAt: now,
			UpdatedAt: now,
		}
		if _, err := deps.Messages.Create(inner, []*types.ChatMessage{msg}); err != nil {
			return err
		}
		if err := deps.Threads.UpdateFields(inner, threadID, map[string]interface{}{
			"next_seq":        nextSeq,
			"last_message_at": now,
			"updated_at":      now,
		}); err != nil {
			return err
		}

		created = msg
		createdNew = true
		return nil
	})
	if err != nil {
		return err
	}
	if createdNew && created != nil && deps.Notify != nil {
		deps.Notify.MessageCreated(owner, threadID, created, nil)
	}
	_ = title
	return nil
}

func buildFallbackIntake(files []*types.MaterialFile, summary *types.MaterialSetSummary, userContext string, userAnswers string) map[string]any {
	fileIntents := make([]map[string]any, 0, len(files))
	for _, f := range files {
		if f == nil {
			continue
		}
		fileIntents = append(fileIntents, map[string]any{
			"file_id":          f.ID.String(),
			"original_name":    f.OriginalName,
			"aim":              "Unknown (fallback)",
			"topics":           []string{},
			"confidence":       0.0,
			"uncertainty_note": "Automatic inference unavailable; proceeding with best effort.",
		})
	}
	goal := ""
	if summary != nil {
		goal = strings.TrimSpace(summary.Subject)
	}
	if goal == "" {
		goal = "Learn the uploaded materials"
	}
	out := map[string]any{
		"file_intents":         fileIntents,
		"combined_goal":        goal,
		"learning_intent": map[string]any{
			"goal_kind":         "unknown",
			"deadline":          "",
			"prior_knowledge":   "",
			"priority_topics":   []string{},
			"deprioritize":      []string{},
			"constraints":       []string{},
			"success_criteria":  "",
			"plan_notes":        "",
			"confidence":        0.1,
			"uncertainty_notes": []string{"fallback_intake"},
		},
		"audience_level_guess": stringsOr(strings.TrimSpace(summaryLevel(summary)), "unknown"),
		"confidence":           0.2,
		"uncertainty_reasons":  []string{"fallback_intake"},
		"needs_clarification":  false,
		"clarifying_questions": []map[string]any{},
		"assumptions":          []string{"Proceeding without additional user context."},
		"notes":                "Fallback intake used due to missing/failed AI call.",
		"user_context":         strings.TrimSpace(userContext),
		"user_answers":         strings.TrimSpace(userAnswers),
	}
	return out
}

func mapFromAny(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func buildIntakeMaterialExcerpts(files []*types.MaterialFile, chunks []*types.MaterialChunk) string {
	if len(files) == 0 || len(chunks) == 0 {
		return ""
	}

	// Defaults tuned for "high signal, low token": a couple of snippets per file.
	perFile := envIntAllowZero("PATH_INTAKE_EXCERPTS_PER_FILE", 2)
	if perFile <= 0 {
		return ""
	}
	maxChars := envIntAllowZero("PATH_INTAKE_EXCERPT_MAX_CHARS", 520)
	if maxChars <= 0 {
		maxChars = 520
	}
	maxTotal := envIntAllowZero("PATH_INTAKE_EXCERPT_MAX_TOTAL_CHARS", 12_000)
	if maxTotal <= 0 {
		maxTotal = 12_000
	}

	byFile := map[uuid.UUID][]*types.MaterialChunk{}
	for _, ch := range chunks {
		if ch == nil || ch.MaterialFileID == uuid.Nil {
			continue
		}
		if isUnextractableChunk(ch) {
			continue
		}
		txt := strings.TrimSpace(ch.Text)
		if txt == "" {
			continue
		}
		byFile[ch.MaterialFileID] = append(byFile[ch.MaterialFileID], ch)
	}
	if len(byFile) == 0 {
		return ""
	}

	var b strings.Builder
	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		arr := byFile[f.ID]
		if len(arr) == 0 {
			continue
		}
		sort.Slice(arr, func(i, j int) bool { return arr[i].Index < arr[j].Index })

		n := len(arr)
		k := perFile
		if k > n {
			k = n
		}
		step := float64(n) / float64(k)

		header := fmt.Sprintf("FILE: %s [file_id=%s]\n", strings.TrimSpace(f.OriginalName), f.ID.String())
		if b.Len()+len(header) > maxTotal {
			break
		}
		b.WriteString(header)

		for i := 0; i < k; i++ {
			idx := int(float64(i) * step)
			if idx < 0 {
				idx = 0
			}
			if idx >= n {
				idx = n - 1
			}
			ch := arr[idx]
			txt := shorten(ch.Text, maxChars)
			if txt == "" {
				continue
			}
			line := fmt.Sprintf("- [chunk_id=%s] %s\n", ch.ID.String(), txt)
			if b.Len()+len(line) > maxTotal {
				break
			}
			b.WriteString(line)
		}
		b.WriteString("\n")
		if b.Len() >= maxTotal {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

func summaryLevel(summary *types.MaterialSetSummary) string {
	if summary == nil {
		return ""
	}
	return strings.TrimSpace(summary.Level)
}

func writePathIntakeMeta(ctx context.Context, deps PathIntakeDeps, pathID uuid.UUID, intake map[string]any, extra map[string]any) error {
	if deps.Path == nil || pathID == uuid.Nil {
		return nil
	}
	row, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, pathID)
	if err != nil {
		return err
	}
	meta := map[string]any{}
	if row != nil && len(row.Metadata) > 0 && string(row.Metadata) != "null" {
		_ = json.Unmarshal(row.Metadata, &meta)
	}
	meta["intake"] = intake
	meta["intake_updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	for k, v := range extra {
		meta[k] = v
	}
	return deps.Path.UpdateFields(dbctx.Context{Ctx: ctx}, pathID, map[string]interface{}{
		"metadata": datatypes.JSON(mustJSON(meta)),
	})
}

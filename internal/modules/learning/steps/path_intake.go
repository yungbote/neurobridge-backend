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

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type PathIntakeDeps struct {
	DB        *gorm.DB
	Log       *logger.Logger
	Files     repos.MaterialFileRepo
	FileSigs  repos.MaterialFileSignatureRepo
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
	// ConfirmExternally keeps paths unconfirmed and emits prompt content without pausing.
	ConfirmExternally bool
}

type PathIntakeOutput struct {
	PathID            uuid.UUID      `json:"path_id"`
	ThreadID          uuid.UUID      `json:"thread_id"`
	Status            string         `json:"status"` // "succeeded" | "waiting_user" | "needs_confirmation"
	Intake            map[string]any `json:"intake,omitempty"`
	Meta              map[string]any `json:"meta,omitempty"`
	Prompt            string         `json:"prompt,omitempty"`
	Workflow          any            `json:"workflow,omitempty"`
	NeedsConfirmation bool           `json:"needs_confirmation,omitempty"`
	Now               string         `json:"now,omitempty"`
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

	// If this path's intake is locked (e.g., derived paths from an earlier split), skip regeneration and reuse.
	if deps.Path != nil {
		if row, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, pathID); err == nil && row != nil && len(row.Metadata) > 0 && strings.TrimSpace(string(row.Metadata)) != "" && strings.TrimSpace(string(row.Metadata)) != "null" {
			var meta map[string]any
			if json.Unmarshal(row.Metadata, &meta) == nil && meta != nil {
				if boolFromAny(meta["intake_locked"]) {
					if intake, ok := meta["intake"].(map[string]any); ok && intake != nil {
						out.Intake = intake
					} else {
						out.Intake = map[string]any{}
					}
					out.Meta = map[string]any{"reason": "intake_locked"}
					return out, nil
				}
			}
		}
	}

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
	fileSigs := map[uuid.UUID]*types.MaterialFileSignature{}
	if deps.FileSigs != nil && len(fileIDs) > 0 {
		if rows, err := deps.FileSigs.GetByMaterialFileIDs(dbctx.Context{Ctx: ctx}, fileIDs); err == nil {
			for _, row := range rows {
				if row == nil || row.MaterialFileID == uuid.Nil {
					continue
				}
				fileSigs[row.MaterialFileID] = row
			}
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
		intake["paths_confirmed"] = true
		filter := buildIntakeMaterialFilter(files, intake)
		_ = writePathIntakeMeta(ctx, deps, pathID, intake, map[string]any{"intake_material_filter": filter})
		out.Intake = intake
		return out, nil
	}

	messages, _ := deps.Messages.ListByThread(dbctx.Context{Ctx: ctx}, in.ThreadID, 300)
	qMsg := latestIntakeQuestionsMessage(messages)
	if qMsg != nil {
		answer := userAnswerAfter(messages, qMsg.Seq)
		if strings.TrimSpace(answer) == "" {
			if !in.WaitForUser && !in.ConfirmExternally {
				// Non-interactive mode: proceed with assumptions and do not re-ask.
				intake := buildFallbackIntake(files, summary, userContextBefore(messages, qMsg.Seq), "")
				intake["paths_confirmed"] = true
				filter := buildIntakeMaterialFilter(files, intake)
				_ = writePathIntakeMeta(ctx, deps, pathID, intake, map[string]any{"intake_material_filter": filter})
				out.Intake = intake
				return out, nil
			}
			if in.ConfirmExternally {
				var existingIntake map[string]any
				if deps.Path != nil {
					if row, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, pathID); err == nil && row != nil && len(row.Metadata) > 0 {
						var meta map[string]any
						if json.Unmarshal(row.Metadata, &meta) == nil {
							existingIntake = mapFromAny(meta["intake"])
						}
					}
				}
				out.Intake = existingIntake
				out.Prompt = strings.TrimSpace(qMsg.Content)
				out.Workflow = buildIntakeWorkflowV1(existingIntake, true)
				out.NeedsConfirmation = true
				out.Status = "needs_confirmation"
				out.Meta = map[string]any{
					"reason":       "awaiting_user_answer",
					"question_seq": qMsg.Seq,
					"question_id":  qMsg.ID.String(),
				}
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

		var existingIntake map[string]any
		if deps.Path != nil {
			if row, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, pathID); err == nil && row != nil && len(row.Metadata) > 0 {
				var meta map[string]any
				if json.Unmarshal(row.Metadata, &meta) == nil {
					existingIntake = mapFromAny(meta["intake"])
				}
			}
		}

		assistantCtx := assistantContextSince(messages, qMsg.Seq)
		intake, intakeMD, err := generateIntake(ctx, deps, files, fileSigs, chunks, summary, prefsAny, userContextBefore(messages, qMsg.Seq), answer, assistantCtx, existingIntake, true)
		if err != nil {
			deps.Log.Warn("path_intake: generate (with answers) failed; proceeding with fallback", "error", err)
			intake = buildFallbackIntake(files, summary, userContextBefore(messages, qMsg.Seq), answer)
			intakeMD = formatIntakeSummaryMD(intake)
		}
		intake["paths_confirmed"] = true
		filter := buildIntakeMaterialFilter(files, intake)
		_ = writePathIntakeMeta(ctx, deps, pathID, intake, map[string]any{
			"intake_md":                 intakeMD,
			"intake_material_filter":    filter,
			"intake_confirmed_by_user":  true,
			"intake_confirmed_at":       time.Now().UTC().Format(time.RFC3339Nano),
			"intake_confirmed_via_chat": true,
		})
		_ = maybeAppendIntakeAckMessage(ctx, deps, in.OwnerUserID, in.ThreadID, in.JobID, in.MaterialSetID, pathID, intake, intakeMD)
		out.Intake = intake
		return out, nil
	}

	userCtx := userContextBefore(messages, 1<<30)

	intake, intakeMD, err := generateIntake(ctx, deps, files, fileSigs, chunks, summary, prefsAny, userCtx, "", "", nil, false)
	if err != nil {
		deps.Log.Warn("path_intake: generate failed; proceeding with fallback", "error", err)
		intake = buildFallbackIntake(files, summary, userCtx, "")
		intakeMD = formatIntakeSummaryMD(intake)
	}

	if in.WaitForUser || in.ConfirmExternally {
		intake["paths_confirmed"] = false
		filter := buildIntakeMaterialFilter(files, intake)
		_ = writePathIntakeMeta(ctx, deps, pathID, intake, map[string]any{"intake_md": intakeMD, "intake_material_filter": filter})
		content := formatIntakeQuestionsMD(intake, intakeMD)
		workflow := buildIntakeWorkflowV1(intake, true)
		out.Prompt = content
		out.Workflow = workflow
		out.NeedsConfirmation = true
		if in.WaitForUser {
			created, err := appendIntakeQuestionsMessage(ctx, deps, in.OwnerUserID, in.ThreadID, in.JobID, in.MaterialSetID, pathID, content, workflow)
			if err != nil {
				deps.Log.Warn("path_intake: failed to post intake questions; proceeding", "error", err)
			} else if created != nil {
				out.Status = "waiting_user"
				out.Meta = map[string]any{
					"reason":       "awaiting_path_confirmation",
					"question_id":  created.ID.String(),
					"question_seq": created.Seq,
				}
				out.Intake = intake
				return out, nil
			}
		}
		if in.ConfirmExternally {
			out.Status = "needs_confirmation"
			out.Intake = intake
			return out, nil
		}
	}

	intake["paths_confirmed"] = true
	filter := buildIntakeMaterialFilter(files, intake)
	_ = writePathIntakeMeta(ctx, deps, pathID, intake, map[string]any{"intake_md": intakeMD, "intake_material_filter": filter})
	_ = maybeAppendIntakeAckMessage(ctx, deps, in.OwnerUserID, in.ThreadID, in.JobID, in.MaterialSetID, pathID, intake, intakeMD)
	out.NeedsConfirmation = false
	out.Prompt = ""
	out.Workflow = nil
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

func assistantContextSince(msgs []*types.ChatMessage, startSeq int64) string {
	const maxMessages = 6
	var parts []string
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if int64(m.Seq) < startSeq {
			continue
		}
		if strings.ToLower(strings.TrimSpace(m.Role)) != "assistant" {
			continue
		}
		txt := strings.TrimSpace(m.Content)
		if txt == "" {
			continue
		}
		if len(txt) > 1200 {
			txt = txt[:1200] + "..."
		}
		parts = append(parts, txt)
		if len(parts) > maxMessages {
			parts = parts[len(parts)-maxMessages:]
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func generateIntake(
	ctx context.Context,
	deps PathIntakeDeps,
	files []*types.MaterialFile,
	fileSigs map[uuid.UUID]*types.MaterialFileSignature,
	chunks []*types.MaterialChunk,
	summary *types.MaterialSetSummary,
	prefsAny any,
	userContext string,
	userAnswers string,
	assistantContext string,
	existingIntake map[string]any,
	isFollowup bool,
) (map[string]any, string, error) {
	fileItems := make([]map[string]any, 0, len(files))
	for _, f := range files {
		if f == nil {
			continue
		}
		item := map[string]any{
			"file_id":        f.ID.String(),
			"original_name":  f.OriginalName,
			"mime_type":      f.MimeType,
			"size_bytes":     f.SizeBytes,
			"extracted_kind": f.ExtractedKind,
		}
		if aiType := strings.TrimSpace(f.AIType); aiType != "" {
			item["ai_type"] = aiType
		}
		if topics := parseAITopics(f.AITopics); len(topics) > 0 {
			item["ai_topics"] = topics
		}
		if sig := fileSigs[f.ID]; sig != nil {
			if topics := parseJSONStrings(sig.Topics); len(topics) > 0 {
				item["sig_topics"] = topics
			}
			if tags := parseJSONStrings(sig.DomainTags); len(tags) > 0 {
				item["sig_domain_tags"] = tags
			}
			if keys := parseJSONStrings(sig.ConceptKeys); len(keys) > 0 {
				item["sig_concept_keys"] = keys
			}
			if s := strings.TrimSpace(sig.Difficulty); s != "" {
				item["sig_difficulty"] = s
			}
			if s := strings.TrimSpace(sig.Language); s != "" {
				item["sig_language"] = s
			}
			if s := strings.TrimSpace(sig.SummaryMD); s != "" {
				item["sig_summary"] = shorten(s, 260)
			}
		}
		fileItems = append(fileItems, item)
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
		"If FILES_JSON includes sig_topics/sig_domain_tags/sig_concept_keys, prefer those over ai_topics when available.",
		"",
		"CRITICAL - Path grouping:",
		"Group the files into one or more coherent learning paths.",
		"Each path should represent a single coherent learning objective or domain.",
		"Prefer a single path whenever the materials can be bridged into a coherent learning sequence.",
		"Only split when the domains are clearly unrelated or cannot be reasonably bridged.",
		"Every file MUST appear in exactly one path (core or support).",
		"No file may appear in more than one path. No path may be empty.",
		"If a file doesn't fit with others, give it its own path instead of excluding it.",
		"If you propose multiple paths, each path's notes MUST explain why it is separate (one short sentence).",
		"If you cannot articulate a clear reason to split, keep them together as one path.",
		"Set confidence values in the 0.0-1.0 range (overall confidence + per-path confidence). Use lower confidence if the split is debatable.",
		"",
		"Use material_alignment.mode:",
		"- 'single_goal' when there is exactly one path.",
		"- 'multi_goal' when there are multiple paths.",
		"",
		"If EXISTING_PATHS_JSON is provided and the user does not ask to change the grouping,",
		"keep the existing paths and file assignments exactly.",
		"",
		"Do NOT drop files into exclude/noise unless they are truly unreadable/blank.",
		"Even then, assign them to a path (e.g., an 'Unclear/low-signal' path).",
		"",
		"Only ask clarifying questions when needed to build a high-quality learning path; keep questions minimal, actionable, and non-redundant.",
		"If USER_ANSWERS are short or numeric (e.g., '2'), use ASSISTANT_MESSAGES_SINCE_LAST_QUESTION to interpret what they refer to.",
		"Prefer asking about goal, deadline, current level, and prioritization when unclear.",
		"Only reference file_id values that appear in FILES_JSON.",
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

	if strings.TrimSpace(assistantContext) != "" {
		user.WriteString("\nASSISTANT_MESSAGES_SINCE_LAST_QUESTION:\n")
		user.WriteString(assistantContext)
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

	if existingIntake != nil {
		existingPaths := sliceAny(existingIntake["paths"])
		if len(existingPaths) > 0 {
			payload := map[string]any{
				"primary_path_id": strings.TrimSpace(stringFromAny(existingIntake["primary_path_id"])),
				"paths":           existingPaths,
			}
			if b, err := json.Marshal(payload); err == nil && len(b) > 0 {
				user.WriteString("\nEXISTING_PATHS_JSON:\n")
				user.WriteString(string(b))
				user.WriteString("\n")
			}
		}
	}

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
						"alignment": map[string]any{
							"type": "string",
							"enum": []any{"core", "support", "noise", "unclear"},
						},
						"include_in_primary_path": map[string]any{"type": "boolean"},
						"alignment_reason":        map[string]any{"type": "string"},
					},
					"required": []string{
						"file_id",
						"original_name",
						"aim",
						"topics",
						"confidence",
						"uncertainty_note",
						"alignment",
						"include_in_primary_path",
						"alignment_reason",
					},
				},
			},
			"material_alignment": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"mode": map[string]any{
						"type": "string",
						"enum": []any{"single_goal", "multi_goal", "unclear"},
					},
					"primary_goal":                     map[string]any{"type": "string"},
					"include_file_ids":                 map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"exclude_file_ids":                 map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"noise_file_ids":                   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"notes":                            map[string]any{"type": "string"},
					"recommended_next_step":            map[string]any{"type": "string"},
					"recommended_next_step_reason":     map[string]any{"type": "string"},
					"recommended_next_step_confidence": map[string]any{"type": "number"},
				},
				"required": []string{
					"mode",
					"primary_goal",
					"include_file_ids",
					"exclude_file_ids",
					"noise_file_ids",
					"notes",
					"recommended_next_step",
					"recommended_next_step_reason",
					"recommended_next_step_confidence",
				},
			},
			"paths": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"path_id":          map[string]any{"type": "string"},
						"title":            map[string]any{"type": "string"},
						"goal":             map[string]any{"type": "string"},
						"core_file_ids":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"support_file_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"confidence":       map[string]any{"type": "number"},
						"notes":            map[string]any{"type": "string"},
					},
					"required": []string{
						"path_id",
						"title",
						"goal",
						"core_file_ids",
						"support_file_ids",
						"confidence",
						"notes",
					},
				},
			},
			"primary_path_id": map[string]any{"type": "string"},
			"combined_goal":   map[string]any{"type": "string"},
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
			"material_alignment",
			"paths",
			"primary_path_id",
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

	normalizeIntakePaths(obj, files, fileSigs)
	softSplitSanityCheck(ctx, obj, files, fileSigs, deps.AI, isFollowup)
	intakeMD := formatIntakeSummaryMD(obj)
	_ = isFollowup // reserved for future logic; keeps signature stable
	return obj, intakeMD, nil
}

func parseAITopics(raw datatypes.JSON) []string {
	return parseJSONStrings(raw)
}

func parseJSONStrings(raw datatypes.JSON) []string {
	if len(raw) == 0 {
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err == nil {
		return dedupeStrings(out)
	}
	var anyArr []any
	if err := json.Unmarshal(raw, &anyArr); err == nil {
		return dedupeStrings(stringSliceFromAny(anyArr))
	}
	return nil
}

func normalizeIntakeFileIntents(intake map[string]any, files []*types.MaterialFile, fileSigs map[uuid.UUID]*types.MaterialFileSignature) {
	if intake == nil {
		return
	}
	if len(files) == 0 {
		return
	}

	type fileInfo struct {
		ID        string
		Name      string
		Topics    []string
		DomainTag []string
	}

	fileByID := map[string]fileInfo{}
	order := make([]string, 0, len(files))
	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		id := f.ID.String()
		if _, ok := fileByID[id]; ok {
			continue
		}
		name := strings.TrimSpace(f.OriginalName)
		topics := parseAITopics(f.AITopics)
		var dom []string
		if sig := fileSigs[f.ID]; sig != nil {
			if sigTopics := parseJSONStrings(sig.Topics); len(sigTopics) > 0 {
				topics = sigTopics
			}
			dom = parseJSONStrings(sig.DomainTags)
		}
		fileByID[id] = fileInfo{ID: id, Name: name, Topics: topics, DomainTag: dom}
		order = append(order, id)
	}
	if len(fileByID) == 0 {
		return
	}

	rawIntents := sliceAny(intake["file_intents"])
	out := make([]any, 0, len(fileByID))
	seen := map[string]bool{}
	for _, it := range rawIntents {
		m, ok := it.(map[string]any)
		if !ok || m == nil {
			continue
		}
		id := strings.TrimSpace(stringFromAny(m["file_id"]))
		if id == "" || seen[id] {
			continue
		}
		fi, ok := fileByID[id]
		if !ok {
			continue
		}
		seen[id] = true

		if strings.TrimSpace(stringFromAny(m["original_name"])) == "" {
			m["original_name"] = fi.Name
		}
		if strings.TrimSpace(stringFromAny(m["aim"])) == "" {
			m["aim"] = "Unknown"
		}
		if topics := stringSliceFromAny(m["topics"]); len(topics) > 0 {
			m["topics"] = topics
		} else if len(fi.Topics) > 0 {
			m["topics"] = fi.Topics
		} else {
			m["topics"] = []string{}
		}
		if _, ok := m["confidence"]; !ok {
			m["confidence"] = 0.0
		}
		if strings.TrimSpace(stringFromAny(m["uncertainty_note"])) == "" {
			m["uncertainty_note"] = "Added for completeness."
		}
		if strings.TrimSpace(stringFromAny(m["alignment"])) == "" {
			m["alignment"] = "unclear"
		}
		if _, ok := m["include_in_primary_path"]; !ok {
			m["include_in_primary_path"] = true
		}
		if strings.TrimSpace(stringFromAny(m["alignment_reason"])) == "" {
			m["alignment_reason"] = "Added for completeness; alignment unclear."
		}
		out = append(out, m)
	}

	for _, id := range order {
		if seen[id] {
			continue
		}
		fi := fileByID[id]
		topics := fi.Topics
		if len(topics) == 0 {
			topics = []string{}
		}
		out = append(out, map[string]any{
			"file_id":                 fi.ID,
			"original_name":           fi.Name,
			"aim":                     "Unknown (missing from intake)",
			"topics":                  topics,
			"confidence":              0.0,
			"uncertainty_note":        "Added missing file for completeness.",
			"alignment":               "unclear",
			"include_in_primary_path": true,
			"alignment_reason":        "Added for completeness; alignment unknown.",
		})
	}

	intake["file_intents"] = out
}

func normalizeIntakePaths(intake map[string]any, files []*types.MaterialFile, fileSigs map[uuid.UUID]*types.MaterialFileSignature) {
	if intake == nil {
		return
	}

	allIDs := make([]string, 0, len(files))
	valid := map[string]bool{}
	if len(files) > 0 {
		for _, f := range files {
			if f == nil || f.ID == uuid.Nil {
				continue
			}
			id := f.ID.String()
			if valid[id] {
				continue
			}
			valid[id] = true
			allIDs = append(allIDs, id)
		}
		allIDs = dedupeStrings(allIDs)
		normalizeIntakeFileIntents(intake, files, fileSigs)
	} else {
		fileIntents := sliceAny(intake["file_intents"])
		for _, it := range fileIntents {
			m, ok := it.(map[string]any)
			if !ok || m == nil {
				continue
			}
			id := strings.TrimSpace(stringFromAny(m["file_id"]))
			if id != "" {
				allIDs = append(allIDs, id)
			}
		}
		allIDs = dedupeStrings(allIDs)
		for _, id := range allIDs {
			valid[id] = true
		}
	}

	ma := mapFromAny(intake["material_alignment"])
	if ma == nil {
		ma = map[string]any{}
		intake["material_alignment"] = ma
	}

	defaultGoal := strings.TrimSpace(stringFromAny(intake["combined_goal"]))
	if defaultGoal == "" {
		defaultGoal = strings.TrimSpace(stringFromAny(ma["primary_goal"]))
	}
	if defaultGoal == "" {
		defaultGoal = "Learn the uploaded materials"
	}

	buildDefault := func(note string) {
		if len(allIDs) == 0 {
			return
		}
		intake["paths"] = []any{
			map[string]any{
				"path_id":          "path_1",
				"title":            "Primary path",
				"goal":             defaultGoal,
				"core_file_ids":    allIDs,
				"support_file_ids": []string{},
				"confidence":       floatFromAny(intake["confidence"], 0.25),
				"notes":            note,
			},
		}
		intake["primary_path_id"] = "path_1"
		ma["mode"] = "single_goal"
		if len(stringSliceFromAny(ma["include_file_ids"])) == 0 {
			ma["include_file_ids"] = allIDs
		}
	}

	rawPaths := sliceAny(intake["paths"])
	if len(rawPaths) == 0 {
		buildDefault("Paths were missing/empty; defaulted to a single path.")
		return
	}

	seen := map[string]bool{}
	assigned := map[string]bool{}
	out := make([]any, 0, len(rawPaths))
	autoN := 1
	for _, p := range rawPaths {
		m, ok := p.(map[string]any)
		if !ok || m == nil {
			continue
		}
		id := strings.TrimSpace(stringFromAny(m["path_id"]))
		if id == "" {
			id = fmt.Sprintf("path_%d", autoN)
			autoN++
			m["path_id"] = id
		}
		if seen[id] {
			base := id
			k := 2
			for seen[id] {
				id = fmt.Sprintf("%s_%d", base, k)
				k++
			}
			m["path_id"] = id
		}
		seen[id] = true

		core := dedupeStrings(stringSliceFromAny(m["core_file_ids"]))
		support := dedupeStrings(stringSliceFromAny(m["support_file_ids"]))
		filteredCore := make([]string, 0, len(core))
		for _, fid := range core {
			if fid == "" || !valid[fid] || assigned[fid] {
				continue
			}
			assigned[fid] = true
			filteredCore = append(filteredCore, fid)
		}
		filteredSupport := make([]string, 0, len(support))
		for _, fid := range support {
			if fid == "" || !valid[fid] || assigned[fid] {
				continue
			}
			assigned[fid] = true
			filteredSupport = append(filteredSupport, fid)
		}
		if len(filteredCore) == 0 && len(filteredSupport) == 0 {
			continue
		}
		if strings.TrimSpace(stringFromAny(m["title"])) == "" {
			m["title"] = fmt.Sprintf("Path %d", len(out)+1)
		}
		if strings.TrimSpace(stringFromAny(m["goal"])) == "" {
			m["goal"] = defaultGoal
		}
		m["core_file_ids"] = filteredCore
		m["support_file_ids"] = filteredSupport
		out = append(out, m)
	}

	unassigned := make([]string, 0, len(allIDs))
	for _, id := range allIDs {
		if !assigned[id] {
			unassigned = append(unassigned, id)
		}
	}
	if len(unassigned) > 0 {
		id := fmt.Sprintf("path_%d", len(out)+1)
		for seen[id] {
			id = fmt.Sprintf("%s_%d", id, 2)
		}
		out = append(out, map[string]any{
			"path_id":          id,
			"title":            "Additional materials",
			"goal":             "Review the remaining materials",
			"core_file_ids":    unassigned,
			"support_file_ids": []string{},
			"confidence":       floatFromAny(intake["confidence"], 0.2),
			"notes":            "Added to ensure every file is assigned to a path.",
		})
		seen[id] = true
	}

	if len(out) == 0 {
		buildDefault("Paths were empty after normalization; defaulted to a single path.")
		return
	}
	intake["paths"] = out

	primary := strings.TrimSpace(stringFromAny(intake["primary_path_id"]))
	if primary == "" || !seen[primary] {
		if m, ok := out[0].(map[string]any); ok && m != nil {
			intake["primary_path_id"] = strings.TrimSpace(stringFromAny(m["path_id"]))
		}
	}

	if len(out) > 1 {
		ma["mode"] = "multi_goal"
	} else {
		ma["mode"] = "single_goal"
	}
	if len(stringSliceFromAny(ma["include_file_ids"])) == 0 && len(allIDs) > 0 {
		ma["include_file_ids"] = allIDs
	}
}

func softSplitSanityCheck(ctx context.Context, intake map[string]any, files []*types.MaterialFile, fileSigs map[uuid.UUID]*types.MaterialFileSignature, ai openai.Client, isFollowup bool) {
	if intake == nil || isFollowup {
		return
	}
	paths := sliceAny(intake["paths"])
	if len(paths) <= 1 {
		return
	}

	fileIntents := sliceAny(intake["file_intents"])
	intentByID := map[string]map[string]any{}
	for _, it := range fileIntents {
		m, ok := it.(map[string]any)
		if !ok || m == nil {
			continue
		}
		id := strings.TrimSpace(stringFromAny(m["file_id"]))
		if id == "" {
			continue
		}
		intentByID[id] = m
	}

	pathTokens := make([]map[string]bool, 0, len(paths))
	pathEmbs := make([][]float32, 0, len(paths))
	pathConf := make([]float64, 0, len(paths))
	missingNotes := false

	for _, it := range paths {
		m, ok := it.(map[string]any)
		if !ok || m == nil {
			continue
		}
		tokenSet := map[string]bool{}
		addTokens(tokenSet, stringFromAny(m["title"]))
		addTokens(tokenSet, stringFromAny(m["goal"]))

		core := stringSliceFromAny(m["core_file_ids"])
		support := stringSliceFromAny(m["support_file_ids"])
		pathFileIDs := append([]string{}, core...)
		pathFileIDs = append(pathFileIDs, support...)
		for _, fid := range append(core, support...) {
			intent := intentByID[fid]
			if intent == nil {
				continue
			}
			addTokens(tokenSet, stringFromAny(intent["aim"]))
			for _, topic := range stringSliceFromAny(intent["topics"]) {
				addTokens(tokenSet, topic)
			}
			name := strings.TrimSpace(stringFromAny(intent["original_name"]))
			name = stripFileExtension(name)
			addTokens(tokenSet, name)
		}
		if len(tokenSet) == 0 {
			addTokens(tokenSet, stringFromAny(intake["combined_goal"]))
		}
		pathTokens = append(pathTokens, tokenSet)
		pathEmbs = append(pathEmbs, avgSignatureEmbedding(pathFileIDs, fileSigs))
		pathConf = append(pathConf, floatFromAny(m["confidence"], floatFromAny(intake["confidence"], 0.5)))
		note := strings.TrimSpace(stringFromAny(m["notes"]))
		if note == "" || len(tokenizeTerms(note)) < 2 {
			missingNotes = true
		}
	}

	if len(pathTokens) <= 1 {
		return
	}

	maxSim := 0.0
	for i := 0; i < len(pathTokens); i++ {
		for j := i + 1; j < len(pathTokens); j++ {
			if sim := jaccard(pathTokens[i], pathTokens[j]); sim > maxSim {
				maxSim = sim
			}
		}
	}
	maxEmbSim := 0.0
	for i := 0; i < len(pathEmbs); i++ {
		for j := i + 1; j < len(pathEmbs); j++ {
			if sim := cosineSim(pathEmbs[i], pathEmbs[j]); sim > maxEmbSim {
				maxEmbSim = sim
			}
		}
	}
	maxPairScore := 0.0
	if ai != nil && envBool("PATH_INTAKE_PAIR_SCORE", false) {
		if ps := scorePairSimilarity(ctx, files, fileSigs, intentByID, ai); ps > maxPairScore {
			maxPairScore = ps
		}
	}

	sepConfidence := floatFromAny(intake["confidence"], 0.5)
	for _, c := range pathConf {
		if c < sepConfidence {
			sepConfidence = c
		}
	}

	lowConfidence := sepConfidence < 0.55
	shouldClarify := (maxSim >= 0.35) || (maxSim >= 0.25 && lowConfidence) || (maxEmbSim >= 0.72) || (maxPairScore >= 0.7) || missingNotes
	if !shouldClarify {
		return
	}

	q := map[string]any{
		"id":       "structure_clarify",
		"question": "These paths seem closely related. Should they be kept together as one path, or kept separate?",
		"reason":   "High similarity and/or low separation confidence between proposed paths.",
	}

	qs := sliceAny(intake["clarifying_questions"])
	for _, it := range qs {
		m, ok := it.(map[string]any)
		if !ok || m == nil {
			continue
		}
		if strings.TrimSpace(stringFromAny(m["id"])) == "structure_clarify" {
			intake["needs_clarification"] = true
			return
		}
	}
	qs = append(qs, q)
	intake["clarifying_questions"] = qs
	intake["needs_clarification"] = true
}

func avgSignatureEmbedding(fileIDs []string, fileSigs map[uuid.UUID]*types.MaterialFileSignature) []float32 {
	if len(fileIDs) == 0 || len(fileSigs) == 0 {
		return nil
	}
	var sum []float32
	var count int
	for _, fid := range fileIDs {
		id, err := uuid.Parse(strings.TrimSpace(fid))
		if err != nil || id == uuid.Nil {
			continue
		}
		sig := fileSigs[id]
		if sig == nil {
			continue
		}
		emb, ok := decodeEmbedding(sig.SummaryEmbedding)
		if !ok || len(emb) == 0 {
			continue
		}
		if sum == nil {
			sum = make([]float32, len(emb))
		}
		if len(sum) != len(emb) {
			continue
		}
		for i := range emb {
			sum[i] += emb[i]
		}
		count++
	}
	if count == 0 {
		return nil
	}
	for i := range sum {
		sum[i] /= float32(count)
	}
	return sum
}

func scorePairSimilarity(ctx context.Context, files []*types.MaterialFile, fileSigs map[uuid.UUID]*types.MaterialFileSignature, intentByID map[string]map[string]any, ai openai.Client) float64 {
	if ai == nil || len(files) < 2 {
		return 0
	}
	maxFiles := envIntAllowZero("PATH_INTAKE_PAIR_SCORE_MAX_FILES", 8)
	if maxFiles > 0 && len(files) > maxFiles {
		return 0
	}

	type fileDesc struct {
		ID   string
		Desc string
	}
	descs := make([]fileDesc, 0, len(files))
	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		id := f.ID.String()
		intent := intentByID[id]
		var topics []string
		if intent != nil {
			topics = stringSliceFromAny(intent["topics"])
		}
		if len(topics) == 0 {
			if sig := fileSigs[f.ID]; sig != nil {
				topics = parseJSONStrings(sig.Topics)
			}
		}
		summary := ""
		if sig := fileSigs[f.ID]; sig != nil {
			summary = strings.TrimSpace(sig.SummaryMD)
		}
		descParts := []string{
			strings.TrimSpace(f.OriginalName),
		}
		if len(topics) > 0 {
			descParts = append(descParts, "topics: "+strings.Join(topics, ", "))
		}
		if summary != "" {
			descParts = append(descParts, "summary: "+shorten(summary, 180))
		}
		desc := strings.TrimSpace(strings.Join(descParts, " | "))
		if desc != "" {
			descs = append(descs, fileDesc{ID: id, Desc: desc})
		}
	}
	if len(descs) < 2 {
		return 0
	}

	maxPairs := envIntAllowZero("PATH_INTAKE_PAIR_SCORE_MAX_PAIRS", 40)
	if maxPairs <= 0 {
		maxPairs = 40
	}

	type pair struct {
		A string `json:"a_id"`
		B string `json:"b_id"`
	}
	pairs := make([]pair, 0, maxPairs)
	for i := 0; i < len(descs); i++ {
		for j := i + 1; j < len(descs); j++ {
			if len(pairs) >= maxPairs {
				break
			}
			pairs = append(pairs, pair{A: descs[i].ID, B: descs[j].ID})
		}
		if len(pairs) >= maxPairs {
			break
		}
	}
	if len(pairs) == 0 {
		return 0
	}

	descLines := make([]string, 0, len(descs))
	for _, d := range descs {
		descLines = append(descLines, fmt.Sprintf("- %s: %s", d.ID, d.Desc))
	}
	pairLines := make([]string, 0, len(pairs))
	for _, p := range pairs {
		pairLines = append(pairLines, fmt.Sprintf("- %s | %s", p.A, p.B))
	}

	system := strings.TrimSpace(strings.Join([]string{
		"You score whether two learning materials can be taught together in one coherent path.",
		"Return JSON only.",
	}, "\n"))
	user := strings.TrimSpace(strings.Join([]string{
		"FILES:",
		strings.Join(descLines, "\n"),
		"",
		"PAIRS (a_id | b_id):",
		strings.Join(pairLines, "\n"),
		"",
		"Return scores 0..1 for each pair.",
	}, "\n"))

	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"scores": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"a_id":  map[string]any{"type": "string"},
						"b_id":  map[string]any{"type": "string"},
						"score": map[string]any{"type": "number"},
					},
					"required": []string{"a_id", "b_id", "score"},
				},
			},
		},
		"required": []string{"scores"},
	}

	obj, err := ai.GenerateJSON(ctx, system, user, "pair_score", schema)
	if err != nil {
		return 0
	}
	rows := sliceAny(obj["scores"])
	maxScore := 0.0
	for _, it := range rows {
		m, ok := it.(map[string]any)
		if !ok || m == nil {
			continue
		}
		score := floatFromAny(m["score"], 0)
		if score > maxScore {
			maxScore = score
		}
	}
	return maxScore
}

func addTokens(dst map[string]bool, text string) {
	for _, tok := range tokenizeTerms(text) {
		dst[tok] = true
	}
}

func tokenizeTerms(text string) []string {
	text = strings.ToLower(text)
	var b strings.Builder
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	parts := strings.Fields(b.String())
	if len(parts) == 0 {
		return nil
	}

	stop := map[string]bool{
		"the": true, "and": true, "for": true, "with": true, "from": true, "into": true,
		"this": true, "that": true, "these": true, "those": true, "learn": true, "learning": true,
		"understand": true, "understanding": true, "overview": true, "intro": true, "introduction": true,
		"basics": true, "course": true, "path": true, "paths": true, "track": true, "tracks": true,
		"materials": true, "material": true, "file": true, "files": true, "study": true, "review": true,
	}

	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) < 2 || stop[p] {
			continue
		}
		out = append(out, p)
	}
	return out
}

func stripFileExtension(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	idx := strings.LastIndex(name, ".")
	if idx <= 0 {
		return name
	}
	return name[:idx]
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	union := len(a)
	for k := range b {
		if a[k] {
			inter++
		} else {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func formatIntakeSummaryMD(intake map[string]any) string {
	if intake == nil {
		return ""
	}
	goal := strings.TrimSpace(stringFromAny(intake["combined_goal"]))
	level := strings.TrimSpace(stringFromAny(intake["audience_level_guess"]))
	intent := mapFromAny(intake["learning_intent"])
	assumptions := stringSliceFromAny(intake["assumptions"])
	fileIntents := sliceAny(intake["file_intents"])
	ma := mapFromAny(intake["material_alignment"])

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

	// Brief, user-facing transparency about multi-material alignment decisions.
	fileNameByID := map[string]string{}
	for _, it := range fileIntents {
		m, ok := it.(map[string]any)
		if !ok || m == nil {
			continue
		}
		id := strings.TrimSpace(stringFromAny(m["file_id"]))
		name := strings.TrimSpace(stringFromAny(m["original_name"]))
		if id == "" || name == "" {
			continue
		}
		fileNameByID[id] = name
	}
	namesForIDs := func(ids []string) []string {
		out := make([]string, 0, len(ids))
		seen := map[string]bool{}
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			name := strings.TrimSpace(fileNameByID[id])
			if name == "" {
				continue
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
		return out
	}
	joinNames := func(names []string, max int) string {
		if len(names) == 0 {
			return ""
		}
		if max <= 0 {
			max = 4
		}
		if len(names) <= max {
			return strings.Join(names, " • ")
		}
		return strings.Join(names[:max], " • ") + fmt.Sprintf(" (+%d more)", len(names)-max)
	}

	usedIDs := stringSliceFromAny(ma["include_file_ids"])
	used := namesForIDs(usedIDs)
	if s := joinNames(used, 4); s != "" {
		lines = append(lines, "**Materials used**: "+s)
	}

	paths := sliceAny(intake["paths"])
	if len(paths) > 1 {
		pathTitles := make([]string, 0, len(paths))
		for _, it := range paths {
			m, ok := it.(map[string]any)
			if !ok || m == nil {
				continue
			}
			title := strings.TrimSpace(stringFromAny(m["title"]))
			if title == "" {
				title = strings.TrimSpace(stringFromAny(m["goal"]))
			}
			if title != "" {
				pathTitles = append(pathTitles, title)
			}
		}
		if s := joinNames(pathTitles, 3); s != "" {
			lines = append(lines, "**Paths proposed**: "+s)
		}
	}

	ignored := namesForIDs(append(stringSliceFromAny(ma["exclude_file_ids"]), stringSliceFromAny(ma["noise_file_ids"])...))
	if s := joinNames(ignored, 3); s != "" {
		lines = append(lines, "**Set aside for now**: "+s)
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func formatProposedPathsMD(intake map[string]any) string {
	if intake == nil {
		return ""
	}
	paths := sliceAny(intake["paths"])
	if len(paths) == 0 {
		return ""
	}

	fileNameByID := map[string]string{}
	for _, it := range sliceAny(intake["file_intents"]) {
		m, ok := it.(map[string]any)
		if !ok || m == nil {
			continue
		}
		id := strings.TrimSpace(stringFromAny(m["file_id"]))
		name := strings.TrimSpace(stringFromAny(m["original_name"]))
		if id == "" || name == "" {
			continue
		}
		fileNameByID[id] = name
	}

	namesForIDs := func(ids []string, max int) string {
		out := make([]string, 0, len(ids))
		seen := map[string]bool{}
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			name := strings.TrimSpace(fileNameByID[id])
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
			if max > 0 && len(out) >= max {
				break
			}
		}
		if len(out) == 0 {
			return ""
		}
		if max > 0 && len(out) > max {
			out = out[:max]
		}
		return strings.Join(out, " • ")
	}

	var b strings.Builder
	b.WriteString("**Proposed paths**\n")
	for i, p := range paths {
		m, ok := p.(map[string]any)
		if !ok || m == nil {
			continue
		}
		title := strings.TrimSpace(stringFromAny(m["title"]))
		if title == "" {
			title = fmt.Sprintf("Path %d", i+1)
		}
		goal := strings.TrimSpace(stringFromAny(m["goal"]))
		line := fmt.Sprintf("%d) **%s**", i+1, title)
		if goal != "" {
			line += " — " + goal
		}
		b.WriteString(line)
		b.WriteString("\n")

		core := namesForIDs(stringSliceFromAny(m["core_file_ids"]), 4)
		support := namesForIDs(stringSliceFromAny(m["support_file_ids"]), 3)
		if core != "" {
			b.WriteString("   Core: " + core + "\n")
		}
		if support != "" {
			b.WriteString("   Support: " + support + "\n")
		}
		if core != "" || support != "" {
			b.WriteString("\n")
		}
	}

	return strings.TrimSpace(b.String())
}

func formatIntakeQuestionsMD(intake map[string]any, intakeMD string) string {
	if intake == nil {
		return "I grouped your files into paths. Reply `confirm` to proceed, or tell me how you want the files regrouped."
	}
	var b strings.Builder
	b.WriteString("I grouped your files into paths.\n\n")

	if md := formatProposedPathsMD(intake); strings.TrimSpace(md) != "" {
		b.WriteString(md)
		b.WriteString("\n\n")
	}

	qs := sliceAny(intake["clarifying_questions"])
	structural := ""
	for _, q := range qs {
		m, ok := q.(map[string]any)
		if !ok || m == nil {
			continue
		}
		id := strings.ToLower(strings.TrimSpace(stringFromAny(m["id"])))
		text := strings.TrimSpace(stringFromAny(m["question"]))
		if text == "" {
			continue
		}
		if id == "structure_clarify" || strings.Contains(strings.ToLower(text), "path") ||
			strings.Contains(strings.ToLower(text), "group") || strings.Contains(strings.ToLower(text), "together") ||
			strings.Contains(strings.ToLower(text), "separate") {
			structural = text
			break
		}
	}
	if structural != "" {
		b.WriteString("Question: " + structural + "\n\n")
	}

	b.WriteString("Reply `confirm` to proceed, or tell me how you want the files regrouped.\n")
	b.WriteString("Examples: \"merge A + B\"; \"split X into its own path\"; \"move file X to path Y\".\n")
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
	workflow *workflowV1,
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
		if workflow != nil {
			meta["workflow_v1"] = workflow
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

func appendIntakeReviewMessage(
	ctx context.Context,
	deps PathIntakeDeps,
	owner uuid.UUID,
	threadID uuid.UUID,
	jobID uuid.UUID,
	materialSetID uuid.UUID,
	pathID uuid.UUID,
	content string,
	workflow *workflowV1,
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

		// Idempotency: one review message per intake job.
		var existing types.ChatMessage
		e := tx.WithContext(ctx).
			Model(&types.ChatMessage{}).
			Where("thread_id = ? AND user_id = ? AND metadata->>'kind' = ? AND metadata->>'job_id' = ?", threadID, owner, "path_intake_review", jobID.String()).
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
			"kind":            "path_intake_review",
			"job_id":          jobID.String(),
			"path_id":         pathID.String(),
			"material_set_id": materialSetID.String(),
		}
		if workflow != nil {
			meta["workflow_v1"] = workflow
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

	content := "Thanks — I’ll generate your learning path now."
	if md := formatProposedPathsMD(intake); strings.TrimSpace(md) != "" {
		content = strings.TrimSpace(strings.Join([]string{
			"Confirmed paths",
			md,
			"Generating your learning path now.",
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
	_ = intakeMD
	return nil
}

func buildFallbackIntake(files []*types.MaterialFile, summary *types.MaterialSetSummary, userContext string, userAnswers string) map[string]any {
	fileIntents := make([]any, 0, len(files))
	includeIDs := make([]string, 0, len(files))
	for _, f := range files {
		if f == nil {
			continue
		}
		if f.ID != uuid.Nil {
			includeIDs = append(includeIDs, f.ID.String())
		}
		fileIntents = append(fileIntents, map[string]any{
			"file_id":                 f.ID.String(),
			"original_name":           f.OriginalName,
			"aim":                     "Unknown (fallback)",
			"topics":                  []string{},
			"confidence":              0.0,
			"uncertainty_note":        "Automatic inference unavailable; proceeding with best effort.",
			"alignment":               "unclear",
			"include_in_primary_path": true,
			"alignment_reason":        "Fallback: cannot reliably determine alignment; including by default.",
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
		"file_intents": fileIntents,
		"material_alignment": map[string]any{
			"mode":                             "unclear",
			"primary_goal":                     goal,
			"include_file_ids":                 dedupeStrings(includeIDs),
			"exclude_file_ids":                 []string{},
			"noise_file_ids":                   []string{},
			"notes":                            "Fallback alignment used due to missing/failed AI call.",
			"recommended_next_step":            "proceed",
			"recommended_next_step_reason":     "Fallback intake cannot ask questions; proceeding with best effort.",
			"recommended_next_step_confidence": 0.2,
		},
		"paths": []map[string]any{
			{
				"path_id":          "path_1",
				"title":            "Primary path",
				"goal":             goal,
				"core_file_ids":    dedupeStrings(includeIDs),
				"support_file_ids": []string{},
				"confidence":       0.2,
				"notes":            "Fallback intake; paths inferred deterministically from available files.",
			},
		},
		"primary_path_id": "path_1",
		"combined_goal":   goal,
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

func buildIntakeMaterialFilter(files []*types.MaterialFile, intake map[string]any) map[string]any {
	valid := map[string]*types.MaterialFile{}
	allIDs := make([]string, 0, len(files))
	goalIDs := make([]string, 0, 1)
	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		id := f.ID.String()
		valid[id] = f
		allIDs = append(allIDs, id)
		name := strings.ToLower(strings.TrimSpace(f.OriginalName))
		if name == "learning_goal.txt" || name == "learning_goal.md" {
			goalIDs = append(goalIDs, id)
		}
	}
	allIDs = dedupeStrings(allIDs)

	ma := mapFromAny(intake["material_alignment"])
	mode := strings.ToLower(strings.TrimSpace(stringFromAny(ma["mode"])))
	if mode == "" {
		mode = "unclear"
	}
	primaryGoal := strings.TrimSpace(stringFromAny(ma["primary_goal"]))

	filterIDs := func(in []string) []string {
		out := make([]string, 0, len(in))
		seen := map[string]bool{}
		for _, s := range in {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if _, ok := valid[s]; !ok {
				continue
			}
			if seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
		return out
	}

	includeIDs := filterIDs(stringSliceFromAny(ma["include_file_ids"]))
	excludeIDs := filterIDs(stringSliceFromAny(ma["exclude_file_ids"]))
	noiseIDs := filterIDs(stringSliceFromAny(ma["noise_file_ids"]))

	// Backstop: derive noise_file_ids from per-file intents if the summary block omitted them.
	if len(noiseIDs) == 0 {
		rawFileIntents := sliceAny(intake["file_intents"])
		tmp := make([]string, 0, len(rawFileIntents))
		for _, it := range rawFileIntents {
			m, ok := it.(map[string]any)
			if !ok || m == nil {
				continue
			}
			if strings.ToLower(strings.TrimSpace(stringFromAny(m["alignment"]))) != "noise" {
				continue
			}
			id := strings.TrimSpace(stringFromAny(m["file_id"]))
			if id != "" {
				tmp = append(tmp, id)
			}
		}
		noiseIDs = filterIDs(tmp)
	}

	paths := sliceAny(intake["paths"])
	if mode == "multi_goal" || len(paths) > 1 {
		// In multi-path uploads, keep access to all non-noise materials so downstream planning
		// can split into separate paths without losing grounding.
		includeIDs = allIDs
	} else if len(includeIDs) == 0 {
		// Derive from per-file flags if the summary block is empty/missing.
		rawFileIntents := sliceAny(intake["file_intents"])
		for _, it := range rawFileIntents {
			m, ok := it.(map[string]any)
			if !ok || m == nil {
				continue
			}
			id := strings.TrimSpace(stringFromAny(m["file_id"]))
			if id == "" {
				continue
			}
			if _, ok := valid[id]; !ok {
				continue
			}
			if boolFromAny(m["include_in_primary_path"]) {
				includeIDs = append(includeIDs, id)
			}
		}
		includeIDs = filterIDs(includeIDs)
	}

	if len(includeIDs) == 0 {
		includeIDs = allIDs
	}

	// Never include excluded/noise in the primary include list.
	blocked := map[string]bool{}
	for _, s := range excludeIDs {
		blocked[s] = true
	}
	for _, s := range noiseIDs {
		blocked[s] = true
	}
	tmp := make([]string, 0, len(includeIDs))
	for _, s := range includeIDs {
		if blocked[s] {
			continue
		}
		tmp = append(tmp, s)
	}
	includeIDs = dedupeStrings(tmp)

	// Always include the goal seed file if present (it anchors intent).
	for _, gid := range goalIDs {
		if gid == "" || blocked[gid] {
			continue
		}
		includeIDs = dedupeStrings(append([]string{gid}, includeIDs...))
	}

	notes := strings.TrimSpace(stringFromAny(ma["notes"]))
	return map[string]any{
		"mode":             mode,
		"primary_goal":     primaryGoal,
		"include_file_ids": includeIDs,
		"exclude_file_ids": excludeIDs,
		"noise_file_ids":   noiseIDs,
		"notes":            notes,
	}
}

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

	// If this path's intake is locked (e.g., derived subpaths in a program split), skip regeneration and reuse.
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
			if !in.WaitForUser {
				// Non-interactive mode: proceed with assumptions and do not re-ask.
				intake := buildFallbackIntake(files, summary, userContextBefore(messages, qMsg.Seq), "")
				filter := buildIntakeMaterialFilter(files, intake)
				_ = writePathIntakeMeta(ctx, deps, pathID, intake, map[string]any{"intake_material_filter": filter})
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
		applyDeterministicPathStructureSelection(intake, userContextBefore(messages, qMsg.Seq), answer)
		// If the user replied but still didn't pick a structure for a multi-goal upload, keep waiting.
		// This prevents creating/splitting paths before the user explicitly confirms what they want.
		if in.WaitForUser && requiresExplicitStructureChoice(intake) {
			filter := buildIntakeMaterialFilter(files, intake)
			_ = writePathIntakeMeta(ctx, deps, pathID, intake, map[string]any{"intake_md": intakeMD, "intake_material_filter": filter})
			out.Status = "waiting_user"
			out.Meta = map[string]any{
				"reason":       "awaiting_structure_choice",
				"question_seq": qMsg.Seq,
				"question_id":  qMsg.ID.String(),
			}
			out.Intake = intake
			return out, nil
		}
		filter := buildIntakeMaterialFilter(files, intake)
		_ = writePathIntakeMeta(ctx, deps, pathID, intake, map[string]any{"intake_md": intakeMD, "intake_material_filter": filter})
		_ = maybeAppendIntakeAckMessage(ctx, deps, in.OwnerUserID, in.ThreadID, in.JobID, in.MaterialSetID, pathID, intake, intakeMD)
		out.Intake = intake
		return out, nil
	}

	userCtx := userContextBefore(messages, 1<<30)

	intake, intakeMD, err := generateIntake(ctx, deps, files, chunks, summary, prefsAny, userCtx, "", false)
	if err != nil {
		deps.Log.Warn("path_intake: generate failed; proceeding with fallback", "error", err)
		intake = buildFallbackIntake(files, summary, userCtx, "")
		filter := buildIntakeMaterialFilter(files, intake)
		_ = writePathIntakeMeta(ctx, deps, pathID, intake, map[string]any{"intake_material_filter": filter})
		_ = maybeAppendIntakeAckMessage(ctx, deps, in.OwnerUserID, in.ThreadID, in.JobID, in.MaterialSetID, pathID, intake, "")
		out.Intake = intake
		return out, nil
	}
	applyDeterministicPathStructureSelection(intake, userCtx, "")
	normalizeHardSeparateClarifyingQuestions(intake)

	needs := boolFromAny(intake["needs_clarification"])
	questions := sliceAny(intake["clarifying_questions"])

	// Production behavior (soft proceed):
	// - Only hard-pause when structure confidence is very low (see shouldHardPauseForIntake()).
	// - Otherwise, proceed and keep any clarifying questions as optional (the user can still chat).
	hardPause := in.WaitForUser && shouldHardPauseForIntake(intake, userCtx)
	if hardPause {
		needs = true
		intake["needs_clarification"] = true
		if len(questions) == 0 {
			questions = defaultClarifyingQuestionsForTracks(intake)
			if len(questions) > 0 {
				intake["clarifying_questions"] = questions
			}
		}
	} else if in.WaitForUser && needs && len(questions) > 0 {
		// Non-blocking: we keep the questions in the intake object for transparency,
		// but do not halt the build unless the safety rule triggers.
		needs = false
		intake["needs_clarification"] = false
	}

	if needs && len(questions) > 0 && in.WaitForUser {
		content := formatIntakeQuestionsMD(intake, intakeMD)
		workflow := buildIntakeWorkflowV1(intake, true)
		created, err := appendIntakeQuestionsMessage(ctx, deps, in.OwnerUserID, in.ThreadID, in.JobID, in.MaterialSetID, pathID, content, workflow)
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
	filter := buildIntakeMaterialFilter(files, intake)
	_ = writePathIntakeMeta(ctx, deps, pathID, intake, map[string]any{"intake_md": intakeMD, "intake_material_filter": filter})
	// For multi-goal uploads (or when the model produced questions but we didn't hard-pause),
	// post a non-blocking "review/override" message so the user can adjust structure while generation proceeds.
	if shouldAppendIntakeReviewMessage(intake, userCtx) {
		content := formatIntakeReviewMD(intake, intakeMD)
		workflow := buildIntakeWorkflowV1(intake, false)
		if _, err := appendIntakeReviewMessage(ctx, deps, in.OwnerUserID, in.ThreadID, in.JobID, in.MaterialSetID, pathID, content, workflow); err != nil {
			deps.Log.Warn("path_intake: failed to post intake review; continuing", "error", err)
			_ = maybeAppendIntakeAckMessage(ctx, deps, in.OwnerUserID, in.ThreadID, in.JobID, in.MaterialSetID, pathID, intake, intakeMD)
		}
	} else {
		_ = maybeAppendIntakeAckMessage(ctx, deps, in.OwnerUserID, in.ThreadID, in.JobID, in.MaterialSetID, pathID, intake, intakeMD)
	}
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

func requiresExplicitStructureChoice(intake map[string]any) bool {
	if intake == nil {
		return false
	}
	ma := mapFromAny(intake["material_alignment"])
	mode := strings.ToLower(strings.TrimSpace(stringFromAny(ma["mode"])))
	tracks := sliceAny(intake["tracks"])
	multiGoal := mode == "multi_goal" || len(tracks) > 1
	if !multiGoal {
		return false
	}
	ps := mapFromAny(intake["path_structure"])
	selected := strings.ToLower(strings.TrimSpace(stringFromAny(ps["selected_mode"])))
	return selected == "" || selected == "unspecified"
}

func hardSeparateOutliers(intake map[string]any) bool {
	if intake == nil {
		return false
	}
	ma := mapFromAny(intake["material_alignment"])
	mode := strings.ToLower(strings.TrimSpace(stringFromAny(ma["mode"])))
	if mode != "multi_goal" {
		return false
	}
	sep := dedupeStrings(stringSliceFromAny(ma["maybe_separate_track_file_ids"]))
	if len(sep) == 0 {
		return false
	}

	// Require a clear "main" cluster as well (otherwise everything is "separate" and we can't infer intent).
	include := dedupeStrings(stringSliceFromAny(ma["include_file_ids"]))
	if len(include) == 0 {
		// Derive a main include set from file_intents include_in_primary_path.
		for _, it := range sliceAny(intake["file_intents"]) {
			m, ok := it.(map[string]any)
			if !ok || m == nil {
				continue
			}
			if !boolFromAny(m["include_in_primary_path"]) {
				continue
			}
			id := strings.TrimSpace(stringFromAny(m["file_id"]))
			if id != "" {
				include = append(include, id)
			}
		}
		include = dedupeStrings(include)
	}

	sepSet := map[string]bool{}
	for _, id := range sep {
		if id != "" {
			sepSet[id] = true
		}
	}
	mainHasNonSep := false
	for _, id := range include {
		if id != "" && !sepSet[id] {
			mainHasNonSep = true
			break
		}
	}
	if !mainHasNonSep {
		return false
	}

	// Only treat as "hard separate" when the model is reasonably confident.
	conf := floatFromAny(ma["recommended_next_step_confidence"], 0)
	if conf <= 0 {
		conf = floatFromAny(intake["confidence"], 0)
	}
	if conf > 0 && conf < 0.60 {
		return false
	}

	// If the model marked any "maybe separate" file as included in the primary path, it's not a hard split.
	includeInPrimary := map[string]bool{}
	for _, it := range sliceAny(intake["file_intents"]) {
		m, ok := it.(map[string]any)
		if !ok || m == nil {
			continue
		}
		id := strings.TrimSpace(stringFromAny(m["file_id"]))
		if id == "" {
			continue
		}
		includeInPrimary[id] = boolFromAny(m["include_in_primary_path"])
	}
	for _, id := range sep {
		if includeInPrimary[id] {
			return false
		}
	}

	return true
}

func isNumericOptionToken(content string) bool {
	s := strings.ToLower(strings.TrimSpace(content))
	if s == "" {
		return false
	}

	// Ignore leading filler words ("ok", "sure") and punctuation.
	s = strings.TrimLeft(s, " \t\r\n,.:;!-")
	for _, prefix := range []string{"ok", "okay", "sure", "yeah", "yep", "yes"} {
		if strings.HasPrefix(s, prefix+" ") {
			s = strings.TrimSpace(strings.TrimPrefix(s, prefix))
			break
		}
	}

	trimmed := strings.TrimSpace(strings.TrimPrefix(s, "#"))
	if trimmed == "1" || trimmed == "2" || trimmed == "option 1" || trimmed == "option 2" || trimmed == "choice 1" || trimmed == "choice 2" {
		return true
	}
	// Allow extra filler after the token (e.g., "1 please", "option 2 then").
	for _, tok := range []string{"1", "2", "option 1", "option 2", "choice 1", "choice 2"} {
		if strings.HasPrefix(trimmed, tok+" ") {
			return true
		}
	}
	return false
}

func looksLikeHardSepConfirmation(content string) bool {
	s := strings.ToLower(strings.TrimSpace(content))
	if s == "" {
		return false
	}
	// If the user is asking a question, assume they want to discuss first.
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
		if strings.HasPrefix(s, prefix) || strings.Contains(s, "\n"+prefix) || strings.Contains(s, " "+prefix) {
			return false
		}
	}
	// Explicit negative signals (avoid resuming on a "no").
	if s == "no" || strings.HasPrefix(s, "no ") || strings.HasPrefix(s, "nah") || strings.HasPrefix(s, "nope") {
		return false
	}
	if strings.Contains(s, "keep together") {
		return false
	}
	if strings.Contains(s, "confirm") || strings.Contains(s, "confirmed") {
		return true
	}
	// Common short confirmations.
	if strings.Contains(s, "ok") || strings.Contains(s, "okay") || strings.Contains(s, "sure") || strings.Contains(s, "sounds good") || strings.Contains(s, "that works") || strings.Contains(s, "thats fine") || strings.Contains(s, "that's fine") || strings.Contains(s, "fine") {
		return true
	}
	if strings.Contains(s, "go ahead") || strings.Contains(s, "do it") || strings.Contains(s, "proceed") || strings.Contains(s, "continue") {
		return true
	}
	// Fallback: numeric option references in a hard-sep prompt are treated as "accept recommendation".
	if isNumericOptionToken(s) {
		return true
	}
	return false
}

func inferSelectedPathStructureModeFromText(content string) string {
	s := strings.ToLower(strings.TrimSpace(content))
	if s == "" {
		return ""
	}

	trimmed := strings.TrimSpace(strings.TrimPrefix(s, "#"))
	switch trimmed {
	case "1", "option 1", "choice 1":
		return "single_path"
	case "2", "option 2", "choice 2":
		return "program_with_subpaths"
	}

	if strings.Contains(s, "undo split") || strings.Contains(s, "undo the split") || strings.Contains(s, "keep together") {
		return "single_path"
	}
	if strings.Contains(s, "restore split") || strings.Contains(s, "restore the split") || strings.Contains(s, "keep the split") {
		return "program_with_subpaths"
	}

	if strings.Contains(s, "single path") || strings.Contains(s, "one path") || strings.Contains(s, "combined path") {
		return "single_path"
	}
	// Handle shorthand like "combine everything" without requiring the exact phrase "combined path".
	if strings.Contains(s, "combine") && (strings.Contains(s, "everything") || strings.Contains(s, "all of it") || strings.Contains(s, "together")) {
		return "single_path"
	}
	if strings.Contains(s, "program with subpaths") {
		return "program_with_subpaths"
	}
	if strings.Contains(s, "separate tracks") || strings.Contains(s, "separate subpaths") {
		return "program_with_subpaths"
	}
	if strings.Contains(s, "split into") && (strings.Contains(s, "track") || strings.Contains(s, "subpath") || strings.Contains(s, "sub-path")) {
		return "program_with_subpaths"
	}
	// Explicit counts (e.g., "3 tracks", "three subpaths") imply split mode.
	for _, n := range []string{"2", "3", "4", "5", "6", "7", "8", "9", "10"} {
		if strings.Contains(s, n+" track") || strings.Contains(s, n+" tracks") || strings.Contains(s, n+" subpath") || strings.Contains(s, n+" subpaths") || strings.Contains(s, n+" sub-path") || strings.Contains(s, n+" sub-paths") {
			return "program_with_subpaths"
		}
	}
	for _, n := range []string{"two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"} {
		if strings.Contains(s, n+" track") || strings.Contains(s, n+" tracks") || strings.Contains(s, n+" subpath") || strings.Contains(s, n+" subpaths") || strings.Contains(s, n+" sub-path") || strings.Contains(s, n+" sub-paths") {
			return "program_with_subpaths"
		}
	}

	return ""
}

func applyDeterministicPathStructureSelection(intake map[string]any, userContext string, userAnswers string) {
	if intake == nil {
		return
	}

	ma := mapFromAny(intake["material_alignment"])
	mode := strings.ToLower(strings.TrimSpace(stringFromAny(ma["mode"])))
	tracks := sliceAny(intake["tracks"])
	multiGoal := mode == "multi_goal" || len(tracks) > 1
	if !multiGoal {
		return
	}

	ps, ok := intake["path_structure"].(map[string]any)
	if !ok || ps == nil {
		return
	}

	recommended := strings.ToLower(strings.TrimSpace(stringFromAny(ps["recommended_mode"])))
	selected := strings.ToLower(strings.TrimSpace(stringFromAny(ps["selected_mode"])))
	hardSep := hardSeparateOutliers(intake)

	// 1) User answers override everything (follow-up pass).
	if strings.TrimSpace(userAnswers) != "" {
		uaNorm := strings.ToLower(strings.TrimSpace(userAnswers))

		// In "hard separate" cases (unrelated domains), treat numeric option tokens as "accept the recommendation"
		// to avoid confusing "option 1" (domain choice) with "option 1" (structure choice).
		if hardSep && isNumericOptionToken(uaNorm) {
			if recommended == "" || recommended == "unspecified" {
				recommended = "program_with_subpaths"
			}
			ps["selected_mode"] = recommended
			intake["needs_clarification"] = false
			return
		}

		// Also accept short/implicit confirmations ("ok that's fine", "sounds good") as "accept recommendation"
		// for hard-separate outliers so the pipeline can resume without the assistant getting in the way.
		if hardSep && selected == "unspecified" && looksLikeHardSepConfirmation(uaNorm) {
			if recommended == "" || recommended == "unspecified" {
				recommended = "program_with_subpaths"
			}
			ps["selected_mode"] = recommended
			intake["needs_clarification"] = false
			return
		}

		if m := inferSelectedPathStructureModeFromText(userAnswers); m != "" {
			ps["selected_mode"] = m
			intake["needs_clarification"] = false
			return
		}

		// "Make reasonable assumptions" is explicit consent to proceed with the recommended mode.
		if strings.Contains(uaNorm, "make reasonable assumptions") ||
			strings.Contains(uaNorm, "whatever you recommend") ||
			strings.Contains(uaNorm, "you decide") ||
			strings.Contains(uaNorm, "your call") ||
			strings.Contains(uaNorm, "pick for me") ||
			// Short confirmations (especially for hard-separate outliers).
			(hardSep && (uaNorm == "yes" || uaNorm == "y" || uaNorm == "yeah" || uaNorm == "yep" || uaNorm == "ok" || uaNorm == "okay" || uaNorm == "sure" || uaNorm == "confirm" || uaNorm == "confirmed")) ||
			strings.HasPrefix(uaNorm, "/proceed") ||
			strings.HasPrefix(uaNorm, "proceed") ||
			strings.HasPrefix(uaNorm, "continue") ||
			strings.HasPrefix(uaNorm, "go ahead") {
			if recommended == "" || recommended == "unspecified" {
				recommended = "single_path"
			}
			ps["selected_mode"] = recommended
			intake["needs_clarification"] = false
			return
		}
	}

	// 2) Initial prompt/context can also provide an explicit preference.
	if strings.TrimSpace(userContext) != "" {
		ucNorm := strings.ToLower(strings.TrimSpace(userContext))
		if hardSep && isNumericOptionToken(ucNorm) {
			if recommended == "" || recommended == "unspecified" {
				recommended = "program_with_subpaths"
			}
			ps["selected_mode"] = recommended
			intake["needs_clarification"] = false
			return
		}
		if m := inferSelectedPathStructureModeFromText(userContext); m != "" {
			ps["selected_mode"] = m
			intake["needs_clarification"] = false
			return
		}
	}

	// 3) Otherwise, force explicit confirmation (don’t let the model auto-select on first pass).
	if selected == "" || selected == "unspecified" || (selected != "single_path" && selected != "program_with_subpaths") {
		ps["selected_mode"] = "unspecified"
	}
}

func normalizeHardSeparateClarifyingQuestions(intake map[string]any) {
	if intake == nil {
		return
	}
	if !hardSeparateOutliers(intake) {
		return
	}
	if !requiresExplicitStructureChoice(intake) {
		return
	}

	ma := mapFromAny(intake["material_alignment"])
	sepIDs := dedupeStrings(stringSliceFromAny(ma["maybe_separate_track_file_ids"]))
	if len(sepIDs) == 0 {
		return
	}

	// Map file_id -> original_name for user-facing text.
	fileNameByID := map[string]string{}
	for _, it := range sliceAny(intake["file_intents"]) {
		m, ok := it.(map[string]any)
		if !ok || m == nil {
			continue
		}
		id := strings.TrimSpace(stringFromAny(m["file_id"]))
		name := strings.TrimSpace(stringFromAny(m["original_name"]))
		if id != "" && name != "" {
			fileNameByID[id] = name
		}
	}
	sepNames := make([]string, 0, len(sepIDs))
	seen := map[string]bool{}
	for _, id := range sepIDs {
		name := strings.TrimSpace(fileNameByID[strings.TrimSpace(id)])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		sepNames = append(sepNames, name)
	}

	sepLabel := "the set-aside file(s)"
	if len(sepNames) > 0 {
		if len(sepNames) <= 2 {
			sepLabel = strings.Join(sepNames, " • ")
		} else {
			sepLabel = strings.Join(sepNames[:2], " • ") + fmt.Sprintf(" (+%d more)", len(sepNames)-2)
		}
	}

	q1 := map[string]any{
		"id":       "confirm_separate_paths",
		"question": fmt.Sprintf("I think %s is unrelated to the rest. I’m going to generate it as a separate path. Is that OK? Reply `confirm` to proceed with separate paths, or reply `keep together` if you want everything combined into one path.", sepLabel),
		"reason":   "Prevents mixing unrelated domains and avoids creating extra paths in the UI without your approval.",
	}
	q2 := map[string]any{
		"id":       "priority_deadline_level",
		"question": "Which path should I prioritize first, and do you have a deadline or target level?",
		"reason":   "Helps set pacing, prerequisites, and how deep to go.",
	}

	intake["needs_clarification"] = true
	intake["clarifying_questions"] = []any{q1, q2}
}

func shouldHardPauseForIntake(intake map[string]any, userContext string) bool {
	if intake == nil {
		return false
	}
	ma := mapFromAny(intake["material_alignment"])
	mode := strings.ToLower(strings.TrimSpace(stringFromAny(ma["mode"])))

	// "Soft proceed" policy:
	// - Only hard-pause when structure confidence is very low (safety rule).
	// - Otherwise, continue the build but keep the user in the loop via non-blocking messages.
	//
	// Env knobs (production):
	// - PATH_INTAKE_HARD_PAUSE_MAX_CONFIDENCE: below this, pause and ask for confirmation (default 0.55).
	// - PATH_INTAKE_HARD_PAUSE_MAX_TRACK_CONFIDENCE: below this, pause (default = same as above).
	hardPauseMax := envFloatAllowZero("PATH_INTAKE_HARD_PAUSE_MAX_CONFIDENCE", 0.55)
	hardPauseTrackMax := envFloatAllowZero("PATH_INTAKE_HARD_PAUSE_MAX_TRACK_CONFIDENCE", hardPauseMax)

	tracks := sliceAny(intake["tracks"])
	multiGoal := mode == "multi_goal" || len(tracks) > 1
	if !multiGoal {
		// For single-goal uploads, avoid blocking unless the model is explicitly very uncertain.
		// (Goal/level clarifications are useful but not worth halting the whole build by default.)
		conf := floatFromAny(intake["confidence"], 0)
		return conf > 0 && conf < hardPauseMax
	}

	// In multi-goal uploads, do not split/spawn multiple paths until the user explicitly confirms structure.
	if requiresExplicitStructureChoice(intake) {
		return true
	}

	// If the model itself says "unclear", always pause for safety.
	if mode == "unclear" {
		return true
	}

	// Overall confidence: if very low, ask before splitting / committing structure.
	conf := floatFromAny(intake["confidence"], 0)
	if conf > 0 && conf < hardPauseMax {
		return true
	}

	// Track-level confidence: if any inferred track is very low confidence, ask.
	minTrack := 1.0
	for _, t := range tracks {
		m, ok := t.(map[string]any)
		if !ok || m == nil {
			continue
		}
		tc := floatFromAny(m["confidence"], 0)
		if tc > 0 && tc < minTrack {
			minTrack = tc
		}
	}
	if minTrack < 1.0 && minTrack > 0 && minTrack < hardPauseTrackMax {
		return true
	}

	// Defensive: if we can't confidently map files into tracks, pause.
	trackHasFiles := false
	for _, t := range tracks {
		m, ok := t.(map[string]any)
		if !ok || m == nil {
			continue
		}
		core := dedupeStrings(stringSliceFromAny(m["core_file_ids"]))
		sup := dedupeStrings(stringSliceFromAny(m["support_file_ids"]))
		if len(core)+len(sup) > 0 {
			trackHasFiles = true
			break
		}
	}
	if !trackHasFiles && len(sliceAny(intake["file_intents"])) >= 2 {
		return true
	}

	return false
}

func defaultClarifyingQuestionsForTracks(intake map[string]any) []any {
	if intake == nil {
		return nil
	}
	tracks := sliceAny(intake["tracks"])
	names := make([]string, 0, len(tracks))
	for _, t := range tracks {
		m, ok := t.(map[string]any)
		if !ok || m == nil {
			continue
		}
		name := strings.TrimSpace(stringFromAny(m["title"]))
		if name == "" {
			name = strings.TrimSpace(stringFromAny(m["goal"]))
		}
		if name != "" {
			names = append(names, name)
		}
	}
	names = dedupeStrings(names)
	if len(names) > 3 {
		names = names[:3]
	}
	themes := "multiple themes"
	if len(names) > 0 {
		themes = strings.Join(names, " / ")
	}

	q1 := map[string]any{
		"id":       "goal_split_or_prioritize",
		"question": "It looks like your upload covers " + themes + ". Do you want (1) one combined path (with modules/tracks), or (2) a broader program with separate subpaths? If you choose subpaths, which track should we build first?",
		"reason":   "This prevents mixing unrelated material and lets me generate the most coherent path structure.",
	}
	q2 := map[string]any{
		"id":       "deadline_level",
		"question": "What’s your deadline (if any) and your current level (beginner/intermediate/advanced) on these topics?",
		"reason":   "This lets me set the right depth, pacing, and prerequisites.",
	}
	return []any{q1, q2}
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
		"Explicitly decide which files belong in the primary learning path vs which are noise/off-goal or should be treated as a separate track.",
		"If the materials clearly diverge into multiple goals, propose 2–4 tracks (subpaths) and group the files into those tracks.",
		"If the materials are multi-goal, you MUST also propose two path-structure options:",
		"  (1) one combined path (single path) that contains multiple modules/tracks, and",
		"  (2) a broader program path with separate subpaths under it (one subpath per track).",
		"For each option, describe what the generated structure would look like (titles/modules and recommended order) and include brief pros/cons.",
		"If the user provided a preference (combined vs split), set path_structure.selected_mode accordingly; otherwise set it to \"unspecified\" and ask a clarifying question.",
		"If you cannot confidently determine whether the upload is single-goal vs multi-goal, set needs_clarification=true and ask the user what they want to prioritize (or whether to split into multiple tracks).",
		"Set tracks to a single track for single-goal uploads; for multi-goal uploads, tracks should represent the distinct goals. primary_track_id must match one of tracks[].track_id.",
		"Only ask clarifying questions when needed to build a high-quality learning path; keep questions minimal, actionable, and non-redundant.",
		"Prefer asking about goal, deadline, current level, and prioritization when unclear or divergent.",
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
					"maybe_separate_track_file_ids":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
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
					"maybe_separate_track_file_ids",
					"noise_file_ids",
					"notes",
					"recommended_next_step",
					"recommended_next_step_reason",
					"recommended_next_step_confidence",
				},
			},
			"path_structure": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"recommended_mode": map[string]any{
						"type": "string",
						"enum": []any{"single_path", "program_with_subpaths"},
					},
					"selected_mode": map[string]any{
						"type": "string",
						"enum": []any{"single_path", "program_with_subpaths", "unspecified"},
					},
					"options": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"properties": map[string]any{
								"option_id": map[string]any{
									"type": "string",
									"enum": []any{"single_path", "program_with_subpaths"},
								},
								"title":              map[string]any{"type": "string"},
								"what_it_looks_like": map[string]any{"type": "string"},
								"pros":               map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
								"cons":               map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
								"recommended":        map[string]any{"type": "boolean"},
							},
							"required": []string{"option_id", "title", "what_it_looks_like", "pros", "cons", "recommended"},
						},
					},
				},
				"required": []string{"recommended_mode", "selected_mode", "options"},
			},
			"primary_track_id": map[string]any{"type": "string"},
			"tracks": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"track_id": map[string]any{"type": "string"},
						"title":    map[string]any{"type": "string"},
						"goal":     map[string]any{"type": "string"},
						"core_file_ids": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
						"support_file_ids": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
						"confidence": map[string]any{"type": "number"},
						"notes":      map[string]any{"type": "string"},
					},
					"required": []string{
						"track_id",
						"title",
						"goal",
						"core_file_ids",
						"support_file_ids",
						"confidence",
						"notes",
					},
				},
			},
			"combined_goal": map[string]any{"type": "string"},
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
			"path_structure",
			"primary_track_id",
			"tracks",
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

	normalizeIntakeTracks(obj)
	intakeMD := formatIntakeSummaryMD(obj)
	_ = isFollowup // reserved for future logic; keeps signature stable
	return obj, intakeMD, nil
}

func normalizeIntakeTracks(intake map[string]any) {
	if intake == nil {
		return
	}

	rawTracks := sliceAny(intake["tracks"])
	buildDefault := func() {
		ma := mapFromAny(intake["material_alignment"])
		includeIDs := dedupeStrings(stringSliceFromAny(ma["include_file_ids"]))
		if len(includeIDs) == 0 {
			for _, it := range sliceAny(intake["file_intents"]) {
				m, ok := it.(map[string]any)
				if !ok || m == nil {
					continue
				}
				id := strings.TrimSpace(stringFromAny(m["file_id"]))
				if id != "" {
					includeIDs = append(includeIDs, id)
				}
			}
			includeIDs = dedupeStrings(includeIDs)
		}
		goal := strings.TrimSpace(stringFromAny(intake["combined_goal"]))
		if goal == "" {
			goal = strings.TrimSpace(stringFromAny(ma["primary_goal"]))
		}
		intake["tracks"] = []any{
			map[string]any{
				"track_id":         "track_1",
				"title":            "Primary track",
				"goal":             stringsOr(goal, "Learn the uploaded materials"),
				"core_file_ids":    includeIDs,
				"support_file_ids": []string{},
				"confidence":       floatFromAny(intake["confidence"], 0.25),
				"notes":            "Tracks were missing/empty; defaulted to a single primary track.",
			},
		}
		intake["primary_track_id"] = "track_1"
	}

	if len(rawTracks) == 0 {
		buildDefault()
		return
	}

	seen := map[string]bool{}
	out := make([]any, 0, len(rawTracks))
	autoN := 1
	for _, t := range rawTracks {
		m, ok := t.(map[string]any)
		if !ok || m == nil {
			continue
		}
		id := strings.TrimSpace(stringFromAny(m["track_id"]))
		if id == "" {
			id = fmt.Sprintf("track_%d", autoN)
			autoN++
			m["track_id"] = id
		}
		// Ensure uniqueness.
		if seen[id] {
			base := id
			k := 2
			for seen[id] {
				id = fmt.Sprintf("%s_%d", base, k)
				k++
			}
			m["track_id"] = id
		}
		seen[id] = true
		out = append(out, m)
	}
	if len(out) == 0 {
		buildDefault()
		return
	}
	intake["tracks"] = out

	primary := strings.TrimSpace(stringFromAny(intake["primary_track_id"]))
	if primary == "" || !seen[primary] {
		if m, ok := out[0].(map[string]any); ok && m != nil {
			intake["primary_track_id"] = strings.TrimSpace(stringFromAny(m["track_id"]))
		}
	}
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
	tracks := sliceAny(intake["tracks"])
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
	sepIDs := stringSliceFromAny(ma["maybe_separate_track_file_ids"])
	if hardSeparateOutliers(intake) && len(usedIDs) > 0 && len(sepIDs) > 0 {
		sepSet := map[string]bool{}
		for _, id := range sepIDs {
			id = strings.TrimSpace(id)
			if id != "" {
				sepSet[id] = true
			}
		}
		filtered := make([]string, 0, len(usedIDs))
		for _, id := range usedIDs {
			id = strings.TrimSpace(id)
			if id == "" || sepSet[id] {
				continue
			}
			filtered = append(filtered, id)
		}
		usedIDs = filtered
	}
	used := namesForIDs(usedIDs)
	if s := joinNames(used, 4); s != "" {
		lines = append(lines, "**Materials used**: "+s)
	}
	separate := namesForIDs(stringSliceFromAny(ma["maybe_separate_track_file_ids"]))
	if s := joinNames(separate, 3); s != "" {
		lines = append(lines, "**Possible separate track**: "+s)
	}
	ignored := namesForIDs(append(stringSliceFromAny(ma["exclude_file_ids"]), stringSliceFromAny(ma["noise_file_ids"])...))
	if s := joinNames(ignored, 3); s != "" {
		lines = append(lines, "**Set aside for now**: "+s)
	}

	// If the upload is multi-goal, expose track names briefly (helps the user verify we understood).
	if len(tracks) > 1 {
		trackTitles := make([]string, 0, len(tracks))
		for _, t := range tracks {
			m, ok := t.(map[string]any)
			if !ok || m == nil {
				continue
			}
			title := strings.TrimSpace(stringFromAny(m["title"]))
			if title == "" {
				title = strings.TrimSpace(stringFromAny(m["goal"]))
			}
			if title != "" {
				trackTitles = append(trackTitles, title)
			}
		}
		trackTitles = dedupeStrings(trackTitles)
		if len(trackTitles) > 0 {
			if len(trackTitles) <= 4 {
				lines = append(lines, "**Tracks**: "+strings.Join(trackTitles, " • "))
			} else {
				lines = append(lines, "**Tracks**: "+strings.Join(trackTitles[:4], " • ")+fmt.Sprintf(" (+%d more)", len(trackTitles)-4))
			}
		}
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func formatIntakeQuestionsMD(intake map[string]any, intakeMD string) string {
	if intake == nil {
		return "I need a bit more context to generate the best learning path. What’s your goal with these materials?"
	}
	var b strings.Builder
	b.WriteString("I reviewed your upload and I want to make sure I generate the right path.\n\n")
	b.WriteString("Path generation is paused until you reply.\n\n")
	if strings.TrimSpace(intakeMD) != "" {
		b.WriteString("**My current read**\n")
		b.WriteString(intakeMD)
		b.WriteString("\n\n")
	}

	// When the upload is multi-goal, present the two "structure" options (combined path vs program+subpaths).
	ma := mapFromAny(intake["material_alignment"])
	mode := strings.ToLower(strings.TrimSpace(stringFromAny(ma["mode"])))
	tracks := sliceAny(intake["tracks"])
	if mode == "multi_goal" || len(tracks) > 1 {
		if md := formatPathStructureOptionsMD(intake); strings.TrimSpace(md) != "" {
			b.WriteString(md)
			b.WriteString("\n\n")
		}
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

	b.WriteString("Reply in one message. If you want to talk it through first, just ask — I can help you decide. When you’re ready, answer the questions (or say “Make reasonable assumptions” and I’ll proceed).\n")
	return strings.TrimSpace(b.String())
}

func shouldAppendIntakeReviewMessage(intake map[string]any, userContext string) bool {
	if intake == nil {
		return false
	}
	ma := mapFromAny(intake["material_alignment"])
	mode := strings.ToLower(strings.TrimSpace(stringFromAny(ma["mode"])))
	tracks := sliceAny(intake["tracks"])
	if mode == "multi_goal" || len(tracks) > 1 {
		return true
	}
	// If the model produced clarifying questions, surface them as optional without blocking.
	if len(sliceAny(intake["clarifying_questions"])) > 0 {
		return true
	}
	// If the user already provided a clear goal/context, avoid extra chatter by default.
	// (They can still open the thread and review the intake snapshot in the path metadata if needed.)
	if strings.TrimSpace(userContext) != "" {
		return false
	}
	return false
}

func formatIntakeReviewMD(intake map[string]any, intakeMD string) string {
	if intake == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("I reviewed your upload. I’m generating your path now — you can still adjust the structure while it runs.\n\n")
	if strings.TrimSpace(intakeMD) != "" {
		b.WriteString("**My current read**\n")
		b.WriteString(intakeMD)
		b.WriteString("\n\n")
	}

	ma := mapFromAny(intake["material_alignment"])
	mode := strings.ToLower(strings.TrimSpace(stringFromAny(ma["mode"])))
	tracks := sliceAny(intake["tracks"])
	if mode == "multi_goal" || len(tracks) > 1 {
		if md := formatPathStructureOptionsMD(intake); strings.TrimSpace(md) != "" {
			b.WriteString(md)
			b.WriteString("\n\n")
		}
	}

	b.WriteString("Reply in one message. If you want to talk it through first, just ask — I can help you decide. When you’re ready, answer the questions (or say “Make reasonable assumptions” and I’ll proceed).\n")
	return strings.TrimSpace(b.String())
}

func formatPathStructureOptionsMD(intake map[string]any) string {
	if intake == nil {
		return ""
	}
	ps := mapFromAny(intake["path_structure"])
	if ps == nil {
		return ""
	}
	opts := sliceAny(ps["options"])
	if len(opts) == 0 {
		return ""
	}

	// If we have clearly unrelated "outlier" files (e.g., immunology mixed into a software upload),
	// do not present a combined single-path option by default. Instead, propose separate paths and ask for confirmation.
	selected := strings.ToLower(strings.TrimSpace(stringFromAny(ps["selected_mode"])))
	if (selected == "" || selected == "unspecified") && hardSeparateOutliers(intake) {
		ma := mapFromAny(intake["material_alignment"])
		sepIDs := dedupeStrings(stringSliceFromAny(ma["maybe_separate_track_file_ids"]))
		mainIDs := dedupeStrings(stringSliceFromAny(ma["include_file_ids"]))

		// Avoid listing outlier files in both "primary" and "separate".
		if len(mainIDs) > 0 && len(sepIDs) > 0 {
			sepSet := map[string]bool{}
			for _, id := range sepIDs {
				if id != "" {
					sepSet[id] = true
				}
			}
			filtered := make([]string, 0, len(mainIDs))
			for _, id := range mainIDs {
				if id != "" && !sepSet[id] {
					filtered = append(filtered, id)
				}
			}
			mainIDs = filtered
		}
		// Backstop: if the model included everything in include_file_ids, derive the main set as "all files minus outliers".
		if len(mainIDs) == 0 {
			allIDs := make([]string, 0, 8)
			for _, it := range sliceAny(intake["file_intents"]) {
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
			sepSet := map[string]bool{}
			for _, id := range sepIDs {
				if id != "" {
					sepSet[id] = true
				}
			}
			filtered := make([]string, 0, len(allIDs))
			for _, id := range allIDs {
				if id != "" && !sepSet[id] {
					filtered = append(filtered, id)
				}
			}
			mainIDs = filtered
		}

		// Map file_id -> original_name for user-facing text.
		fileNameByID := map[string]string{}
		for _, it := range sliceAny(intake["file_intents"]) {
			m, ok := it.(map[string]any)
			if !ok || m == nil {
				continue
			}
			id := strings.TrimSpace(stringFromAny(m["file_id"]))
			name := strings.TrimSpace(stringFromAny(m["original_name"]))
			if id != "" && name != "" {
				fileNameByID[id] = name
			}
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
			}
			if len(out) == 0 {
				return ""
			}
			if max <= 0 {
				max = 3
			}
			if len(out) <= max {
				return strings.Join(out, " • ")
			}
			return strings.Join(out[:max], " • ") + fmt.Sprintf(" (+%d more)", len(out)-max)
		}

		sepNames := namesForIDs(sepIDs, 3)
		mainNames := namesForIDs(mainIDs, 4)

		var b strings.Builder
		b.WriteString("**Structure recommendation**\n")
		b.WriteString("One or more files look unrelated to the rest, so I’m going to generate them as **separate paths** (instead of mixing them into one curriculum).\n\n")
		if mainNames != "" {
			b.WriteString("- **Primary path** (main set): " + mainNames + "\n")
		}
		if sepNames != "" {
			b.WriteString("- **Separate path** (set aside): " + sepNames + "\n")
		}
		b.WriteString("\n")
		b.WriteString("Reply `confirm` to proceed with separate paths, or reply `keep together` if you really want everything combined into one path.\n")
		b.WriteString("\nNote: Separate paths are still grouped under a program container for organization (you can detach later).\n")
		return strings.TrimSpace(b.String())
	}

	// Keep concise: two options max.
	if len(opts) > 2 {
		opts = opts[:2]
	}

	var b strings.Builder
	b.WriteString("**Two ways I can structure this**\n")
	for i, opt := range opts {
		m, ok := opt.(map[string]any)
		if !ok || m == nil {
			continue
		}
		title := strings.TrimSpace(stringFromAny(m["title"]))
		if title == "" {
			title = strings.TrimSpace(stringFromAny(m["option_id"]))
		}
		looks := strings.TrimSpace(stringFromAny(m["what_it_looks_like"]))
		rec := boolFromAny(m["recommended"])

		line := fmt.Sprintf("%d) **%s**", i+1, title)
		if rec {
			line += " (recommended)"
		}
		b.WriteString(line)
		b.WriteString("\n")
		if looks != "" {
			b.WriteString(looks)
			b.WriteString("\n")
		}

		pros := dedupeStrings(stringSliceFromAny(m["pros"]))
		cons := dedupeStrings(stringSliceFromAny(m["cons"]))
		if len(pros) > 0 {
			if len(pros) > 4 {
				pros = pros[:4]
			}
			b.WriteString("- Pros: " + strings.Join(pros, " • ") + "\n")
		}
		if len(cons) > 0 {
			if len(cons) > 4 {
				cons = cons[:4]
			}
			b.WriteString("- Cons: " + strings.Join(cons, " • ") + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("Note: Option **2** creates separate independent paths (subpaths) and groups them under a program container for organization.\n\n")
	b.WriteString("Reply with just 1 or 2 if you want, ask me to help you decide, or propose a custom structure (e.g., \"3 tracks: ...\").\n\n")
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
			"maybe_separate_track_file_ids":    []string{},
			"noise_file_ids":                   []string{},
			"notes":                            "Fallback alignment used due to missing/failed AI call.",
			"recommended_next_step":            "proceed",
			"recommended_next_step_reason":     "Fallback intake cannot ask questions; proceeding with best effort.",
			"recommended_next_step_confidence": 0.2,
		},
		"path_structure": map[string]any{
			"recommended_mode": "single_path",
			"selected_mode":    "single_path",
			"options": []map[string]any{
				{
					"option_id":          "single_path",
					"title":              "One combined path",
					"what_it_looks_like": "A single path that covers the uploaded materials in a coherent order.",
					"pros":               []string{"One place to learn", "Simplest navigation"},
					"cons":               []string{"May mix unrelated topics if the upload is multi-goal"},
					"recommended":        true,
				},
				{
					"option_id":          "program_with_subpaths",
					"title":              "A program with separate subpaths",
					"what_it_looks_like": "A higher-level program path that groups multiple focused subpaths (one per topic).",
					"pros":               []string{"Clear separation by topic", "Each subpath stays tightly focused"},
					"cons":               []string{"Slightly more navigation overhead"},
					"recommended":        false,
				},
			},
		},
		"primary_track_id": "track_1",
		"tracks": []map[string]any{
			{
				"track_id":         "track_1",
				"title":            "Primary track",
				"goal":             goal,
				"core_file_ids":    dedupeStrings(includeIDs),
				"support_file_ids": []string{},
				"confidence":       0.2,
				"notes":            "Fallback intake; tracks inferred deterministically from available files.",
			},
		},
		"combined_goal": goal,
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
	maybeSeparateIDs := filterIDs(stringSliceFromAny(ma["maybe_separate_track_file_ids"]))
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

	if mode == "multi_goal" {
		// In multi-goal uploads, keep access to all non-noise materials so downstream planning
		// can split the curriculum into multiple tracks/modules without losing grounding.
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
		"mode":                          mode,
		"primary_goal":                  primaryGoal,
		"include_file_ids":              includeIDs,
		"exclude_file_ids":              excludeIDs,
		"maybe_separate_track_file_ids": maybeSeparateIDs,
		"noise_file_ids":                noiseIDs,
		"notes":                         notes,
	}
}

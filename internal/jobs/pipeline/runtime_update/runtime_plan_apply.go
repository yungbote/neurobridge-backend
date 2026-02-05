package runtime_update

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func (p *Pipeline) applyRuntimePlan(dbc dbctx.Context, userID uuid.UUID, pathID uuid.UUID, nodeID uuid.UUID, typ string, data map[string]any, now time.Time) error {
	if p.paths == nil || p.nodeRuns == nil || p.pathRuns == nil || p.nodeDocs == nil || userID == uuid.Nil || pathID == uuid.Nil || nodeID == uuid.Nil {
		return nil
	}

	shouldConsider := typ == types.EventBlockViewed ||
		typ == types.EventBlockRead ||
		typ == types.EventScrollDepth ||
		typ == types.EventQuestionAnswered ||
		typ == types.EventRuntimePromptCompleted ||
		typ == types.EventRuntimePromptDismissed ||
		typ == types.EventActivityCompleted ||
		typ == types.EventQuizCompleted
	if !shouldConsider {
		return nil
	}

	pathRow, err := p.paths.GetByID(dbc, pathID)
	if err != nil || pathRow == nil {
		return nil
	}
	pathMeta := decodeJSONMap(pathRow.Metadata)
	plan := getRuntimePlan(pathMeta)
	if plan == nil {
		return nil
	}

	policy := resolveRuntimePolicy(plan, nodeID)

	pr, _ := p.pathRuns.GetByUserAndPathID(dbc, userID, pathID)
	if pr == nil {
		pr = &types.PathRun{UserID: userID, PathID: pathID}
	}
	prMeta := decodeJSONMap(pr.Metadata)
	prRuntime := mapFromAny(prMeta["runtime"])

	nr, _ := p.nodeRuns.GetByUserAndNodeID(dbc, userID, nodeID)
	if nr == nil {
		nr = &types.NodeRun{UserID: userID, PathID: pathID, NodeID: nodeID}
	}
	nrMeta := decodeJSONMap(nr.Metadata)
	nrRuntime := mapFromAny(nrMeta["runtime"])

	blockID := strings.TrimSpace(stringFromAny(data["block_id"]))
	if blockID == "" {
		blockID = strings.TrimSpace(stringFromAny(data["question_id"]))
	}
	progressState := strings.TrimSpace(stringFromAny(data["progress_state"]))
	progressConf := floatFromAny(data["progress_confidence"], 0)
	progressOk := progressEligible(progressState, progressConf)
	progressSignal := progressState != "" || progressConf > 0
	if progressSignal {
		nrRuntime["last_progress_state"] = progressState
		nrRuntime["last_progress_confidence"] = progressConf
		nrRuntime["last_progress_at"] = now.Format(time.RFC3339)
		if progressOk {
			if timeFromAny(prRuntime["progressing_since"]) == nil {
				prRuntime["progressing_since"] = now.Format(time.RFC3339)
			}
		} else {
			delete(prRuntime, "progressing_since")
		}
	}

	// Update runtime counters from events.
	blocksSeen := intFromAny(nrRuntime["blocks_seen"], 0)
	lastBlockID := stringFromAny(nrRuntime["last_block_id"])
	if typ == types.EventBlockViewed && blockID != "" && blockID != lastBlockID && progressOk {
		blocksSeen++
		nrRuntime["blocks_seen"] = blocksSeen
		nrRuntime["last_block_id"] = blockID
		nrRuntime["last_seen_at"] = now.Format(time.RFC3339)
	}

	readBlocks := stringSliceFromAny(nrRuntime["read_blocks"])
	if typ == types.EventBlockRead && blockID != "" && progressOk {
		readBlocks = appendIfMissing(readBlocks, blockID)
		nrRuntime["read_blocks"] = readBlocks
		nrRuntime["last_read_block_id"] = blockID
		nrRuntime["last_read_at"] = now.Format(time.RFC3339)
	}

	// Allow re-showing uncompleted prompts on lesson re-entry (configurable).
	if typ == types.EventNodeOpened && allowReshowUncompleted() {
		shownBlocks := stringSliceFromAny(nrRuntime["shown_blocks"])
		if len(shownBlocks) > 0 {
			completedBlocks := stringSliceFromAny(nrRuntime["completed_blocks"])
			completedSet := map[string]bool{}
			for _, id := range completedBlocks {
				completedSet[id] = true
			}
			filtered := []string{}
			for _, id := range shownBlocks {
				if completedSet[id] {
					filtered = append(filtered, id)
				}
			}
			if len(filtered) != len(shownBlocks) {
				nrRuntime["shown_blocks"] = filtered

				// Recompute prompt counters from completed blocks.
				qcCount := 0
				fcCount := 0
				if docRow, err := p.nodeDocs.GetByPathNodeID(dbc, nodeID); err == nil && docRow != nil {
					doc := content.NodeDocV1{}
					if err := json.Unmarshal(docRow.DocJSON, &doc); err == nil {
						for _, b := range doc.Blocks {
							id := strings.TrimSpace(stringFromAny(b["id"]))
							if id == "" || !completedSet[id] {
								continue
							}
							typ := strings.ToLower(strings.TrimSpace(stringFromAny(b["type"])))
							if typ == "quick_check" {
								qcCount++
							}
							if typ == "flashcard" {
								fcCount++
							}
						}
					}
				}
				nrRuntime["quick_checks_shown"] = qcCount
				nrRuntime["flashcards_shown"] = fcCount
				nrRuntime["last_quick_check_blocks"] = intFromAny(nrRuntime["last_quick_check_blocks"], 0)
				nrRuntime["last_flashcard_blocks"] = intFromAny(nrRuntime["last_flashcard_blocks"], 0)
			}
		}
	}

	failStreak := intFromAny(nrRuntime["fail_streak"], 0)
	if typ == types.EventQuestionAnswered {
		if isCorrect := boolFromAny(data["is_correct"]); isCorrect {
			failStreak = 0
		} else {
			failStreak++
		}
		nrRuntime["fail_streak"] = failStreak
	}

	completedBlocks := stringSliceFromAny(nrRuntime["completed_blocks"])
	shownBlocks := stringSliceFromAny(nrRuntime["shown_blocks"])

	// Resolve pending prompt (if any).
	pending := runtimePromptFromMap(mapFromAny(prRuntime["runtime_prompt"]))
	if pending != nil && strings.EqualFold(pending.Status, "pending") {
		handled := false
		if typ == types.EventRuntimePromptCompleted || typ == types.EventRuntimePromptDismissed {
			if pid := stringFromAny(data["prompt_id"]); pid != "" && pid == pending.ID {
				handled = true
				if typ == types.EventRuntimePromptCompleted && pending.BlockID != "" {
					completedBlocks = appendIfMissing(completedBlocks, pending.BlockID)
				}
			}
		}
		if typ == types.EventQuestionAnswered && pending.Type == "quick_check" && pending.BlockID != "" && pending.BlockID == blockID {
			if boolFromAny(data["is_correct"]) {
				handled = true
				completedBlocks = appendIfMissing(completedBlocks, pending.BlockID)
			}
		}
		if handled {
			prRuntime["runtime_prompt"] = nil
			prRuntime["last_prompt_at"] = now.Format(time.RFC3339)
			if typ == types.EventRuntimePromptCompleted {
				prRuntime["last_prompt_status"] = "completed"
			} else if typ == types.EventRuntimePromptDismissed {
				prRuntime["last_prompt_status"] = "dismissed"
			}
			nrRuntime["completed_blocks"] = completedBlocks
			prMeta["runtime"] = prRuntime
			nrMeta["runtime"] = nrRuntime
			pr.Metadata = encodeJSONMap(prMeta)
			nr.Metadata = encodeJSONMap(nrMeta)
			_ = p.pathRuns.Upsert(dbc, pr)
			_ = p.nodeRuns.Upsert(dbc, nr)
		}
		return nil
	}

	// If already at prompt rate cap, skip.
	promptWindowStart := timeFromAny(prRuntime["prompt_window_started_at"])
	if shouldResetPromptWindow(promptWindowStart, now) {
		promptWindowStart = &now
		prRuntime["prompt_window_started_at"] = now.Format(time.RFC3339)
		prRuntime["prompts_in_window"] = 0
	}
	promptsInWindow := intFromAny(prRuntime["prompts_in_window"], 0)
	if policy.MaxPromptsPerHour > 0 && promptsInWindow >= policy.MaxPromptsPerHour {
		prMeta["runtime"] = prRuntime
		pr.Metadata = encodeJSONMap(prMeta)
		_ = p.pathRuns.Upsert(dbc, pr)
		return nil
	}

	lastPromptAt := timeFromAny(prRuntime["last_prompt_at"])
	if lastPromptAt != nil && now.Sub(*lastPromptAt) < runtimePromptMinGapMinute*time.Minute {
		return nil
	}

	quickChecksShown := intFromAny(nrRuntime["quick_checks_shown"], 0)
	flashcardsShown := intFromAny(nrRuntime["flashcards_shown"], 0)
	lastQCBlocks := intFromAny(nrRuntime["last_quick_check_blocks"], 0)
	lastFCBlocks := intFromAny(nrRuntime["last_flashcard_blocks"], 0)
	readCount := len(readBlocks)
	blocksForCadence := blocksSeen
	if readCount > 0 {
		blocksForCadence = readCount
	}
	lastQCAT := timeFromAny(nrRuntime["last_quick_check_at"])
	lastFCAT := timeFromAny(nrRuntime["last_flashcard_at"])

	shouldQuick := quickChecksShown < policy.QuickCheckMaxPerLesson
	if shouldQuick {
		if policy.QuickCheckAfterBlocks > 0 {
			shouldQuick = shouldQuick && (blocksForCadence-lastQCBlocks) >= policy.QuickCheckAfterBlocks
		}
		if policy.QuickCheckMinGapBlocks > 0 {
			shouldQuick = shouldQuick && (blocksForCadence-lastQCBlocks) >= policy.QuickCheckMinGapBlocks
		}
		if policy.QuickCheckAfterMinutes > 0 && lastQCAT != nil {
			shouldQuick = shouldQuick && now.Sub(*lastQCAT).Minutes() >= float64(policy.QuickCheckAfterMinutes)
		}
	}

	shouldFlash := flashcardsShown < policy.FlashcardMaxPerLesson
	if shouldFlash {
		if policy.FlashcardAfterBlocks > 0 {
			shouldFlash = shouldFlash && (blocksForCadence-lastFCBlocks) >= policy.FlashcardAfterBlocks
		}
		if policy.FlashcardAfterMinutes > 0 && lastFCAT != nil {
			shouldFlash = shouldFlash && now.Sub(*lastFCAT).Minutes() >= float64(policy.FlashcardAfterMinutes)
		}
		if policy.FlashcardAfterFailStreak > 0 {
			shouldFlash = shouldFlash && failStreak >= policy.FlashcardAfterFailStreak
		}
	}

	shouldBreak := false
	sessionStarted := timeFromAny(prRuntime["session_started_at"])
	if sessionStarted == nil {
		sessionStarted = &now
		prRuntime["session_started_at"] = now.Format(time.RFC3339)
	}
	breakStart := sessionStarted
	if progressSignal {
		if progressOk {
			if progressingSince := timeFromAny(prRuntime["progressing_since"]); progressingSince != nil {
				breakStart = progressingSince
			}
		}
	}
	if policy.BreakAfterMinutes > 0 && breakStart != nil && now.Sub(*breakStart).Minutes() >= float64(policy.BreakAfterMinutes) {
		lastBreakAt := timeFromAny(prRuntime["last_break_at"])
		if lastBreakAt == nil || now.Sub(*lastBreakAt).Minutes() >= float64(policy.BreakAfterMinutes) {
			shouldBreak = true
		}
	}

	if progressSignal && !progressOk {
		shouldQuick = false
		shouldFlash = false
		shouldBreak = false
	}

	if !shouldQuick && !shouldFlash && !shouldBreak {
		prMeta["runtime"] = prRuntime
		nrMeta["runtime"] = nrRuntime
		pr.Metadata = encodeJSONMap(prMeta)
		nr.Metadata = encodeJSONMap(nrMeta)
		_ = p.pathRuns.Upsert(dbc, pr)
		_ = p.nodeRuns.Upsert(dbc, nr)
		return nil
	}

	docRow, err := p.nodeDocs.GetByPathNodeID(dbc, nodeID)
	if err != nil || docRow == nil {
		return nil
	}
	doc := content.NodeDocV1{}
	if err := json.Unmarshal(docRow.DocJSON, &doc); err != nil {
		return nil
	}

	blockIndex := map[string]int{}
	for i, b := range doc.Blocks {
		id := strings.TrimSpace(stringFromAny(b["id"]))
		if id != "" {
			blockIndex[id] = i
		}
	}

	readSet := map[string]bool{}
	for _, id := range readBlocks {
		s := strings.TrimSpace(id)
		if s != "" {
			readSet[s] = true
		}
	}
	lastReadIndex := -1
	if len(readSet) > 0 {
		for i, b := range doc.Blocks {
			id := strings.TrimSpace(stringFromAny(b["id"]))
			if id != "" && readSet[id] {
				lastReadIndex = i
			}
		}
	}

	extractChunkIDs := func(raw any) []string {
		out := []string{}
		for _, item := range stringSliceFromAny(raw) {
			if item == "" {
				continue
			}
			out = appendIfMissing(out, item)
		}
		// Handle citation objects [{chunk_id: "..."}].
		switch arr := raw.(type) {
		case []any:
			for _, v := range arr {
				m, ok := v.(map[string]any)
				if !ok {
					continue
				}
				id := strings.TrimSpace(stringFromAny(m["chunk_id"]))
				if id != "" {
					out = appendIfMissing(out, id)
				}
			}
		}
		return out
	}

	isTeachingBlock := func(t string) bool {
		t = strings.ToLower(strings.TrimSpace(t))
		switch t {
		case "", "quick_check", "flashcard", "heading", "divider", "objectives", "prerequisites", "key_takeaways", "glossary":
			return false
		default:
			return true
		}
	}

	inferTriggerIDs := func(block map[string]any, idx int) []string {
		if idx <= 0 {
			return nil
		}
		citeIDs := map[string]bool{}
		for _, id := range extractChunkIDs(block["citations"]) {
			if id != "" {
				citeIDs[id] = true
			}
		}
		candidates := []string{}
		for i := idx - 1; i >= 0; i-- {
			b := doc.Blocks[i]
			if b == nil {
				continue
			}
			t := strings.ToLower(strings.TrimSpace(stringFromAny(b["type"])))
			if !isTeachingBlock(t) {
				continue
			}
			id := strings.TrimSpace(stringFromAny(b["id"]))
			if id == "" {
				continue
			}
			if len(citeIDs) > 0 {
				overlap := false
				for _, c := range extractChunkIDs(b["citations"]) {
					if citeIDs[c] {
						overlap = true
						break
					}
				}
				if !overlap {
					continue
				}
			}
			candidates = append(candidates, id)
			if len(candidates) >= 3 {
				break
			}
		}
		if len(candidates) == 0 {
			// Fallback to the nearest previous teaching block.
			for i := idx - 1; i >= 0; i-- {
				b := doc.Blocks[i]
				if b == nil {
					continue
				}
				t := strings.ToLower(strings.TrimSpace(stringFromAny(b["type"])))
				if !isTeachingBlock(t) {
					continue
				}
				id := strings.TrimSpace(stringFromAny(b["id"]))
				if id != "" {
					candidates = append(candidates, id)
					break
				}
			}
		}
		return candidates
	}

	triggerSatisfied := func(block map[string]any, idx int) bool {
		triggers := stringSliceFromAny(block["trigger_after_block_ids"])
		if len(triggers) == 0 {
			triggers = inferTriggerIDs(block, idx)
		}
		if len(triggers) == 0 {
			if lastReadIndex >= 0 && idx > lastReadIndex {
				return false
			}
			return true
		}
		for _, id := range triggers {
			if id == "" {
				continue
			}
			blockIdx, ok := blockIndex[id]
			if !ok {
				return false
			}
			if blockIdx > idx {
				return false
			}
			if !readSet[id] {
				return false
			}
		}
		return true
	}

	findBlock := func(kind string) string {
		for i, b := range doc.Blocks {
			if strings.EqualFold(stringFromAny(b["type"]), kind) {
				id := strings.TrimSpace(stringFromAny(b["id"]))
				if id == "" {
					continue
				}
				if containsString(completedBlocks, id) || containsString(shownBlocks, id) {
					continue
				}
				if !triggerSatisfied(b, i) {
					continue
				}
				return id
			}
		}
		return ""
	}

	var prompt runtimePrompt
	switch {
	case shouldBreak:
		prompt = runtimePrompt{
			ID:        uuid.New().String(),
			Type:      "break",
			NodeID:    nodeID.String(),
			Status:    "pending",
			Reason:    "time_elapsed",
			CreatedAt: now.Format(time.RFC3339),
		}
	case shouldQuick:
		id := findBlock("quick_check")
		if id != "" {
			prompt = runtimePrompt{
				ID:        uuid.New().String(),
				Type:      "quick_check",
				NodeID:    nodeID.String(),
				BlockID:   id,
				Status:    "pending",
				Reason:    "cadence",
				CreatedAt: now.Format(time.RFC3339),
			}
		}
	case shouldFlash:
		id := findBlock("flashcard")
		if id != "" {
			prompt = runtimePrompt{
				ID:        uuid.New().String(),
				Type:      "flashcard",
				NodeID:    nodeID.String(),
				BlockID:   id,
				Status:    "pending",
				Reason:    "cadence",
				CreatedAt: now.Format(time.RFC3339),
			}
		}
	}

	if prompt.ID == "" {
		prMeta["runtime"] = prRuntime
		nrMeta["runtime"] = nrRuntime
		pr.Metadata = encodeJSONMap(prMeta)
		nr.Metadata = encodeJSONMap(nrMeta)
		_ = p.pathRuns.Upsert(dbc, pr)
		_ = p.nodeRuns.Upsert(dbc, nr)
		return nil
	}

	shownBlocks = appendIfMissing(shownBlocks, prompt.BlockID)
	if prompt.Type == "quick_check" {
		quickChecksShown++
		nrRuntime["quick_checks_shown"] = quickChecksShown
		nrRuntime["last_quick_check_at"] = now.Format(time.RFC3339)
		nrRuntime["last_quick_check_blocks"] = blocksForCadence
	}
	if prompt.Type == "flashcard" {
		flashcardsShown++
		nrRuntime["flashcards_shown"] = flashcardsShown
		nrRuntime["last_flashcard_at"] = now.Format(time.RFC3339)
		nrRuntime["last_flashcard_blocks"] = blocksForCadence
	}
	if prompt.Type == "break" {
		prRuntime["last_break_at"] = now.Format(time.RFC3339)
	}

	prRuntime["runtime_prompt"] = runtimePromptToMap(prompt)
	prRuntime["last_prompt_at"] = now.Format(time.RFC3339)
	prRuntime["prompts_in_window"] = promptsInWindow + 1
	prMeta["runtime"] = prRuntime
	nrRuntime["shown_blocks"] = shownBlocks
	nrMeta["runtime"] = nrRuntime

	pr.Metadata = encodeJSONMap(prMeta)
	nr.Metadata = encodeJSONMap(nrMeta)
	_ = p.pathRuns.Upsert(dbc, pr)
	_ = p.nodeRuns.Upsert(dbc, nr)

	if p.notify != nil {
		p.notify.RuntimePrompt(userID, map[string]any{
			"path_id":    pathID.String(),
			"node_id":    prompt.NodeID,
			"block_id":   prompt.BlockID,
			"type":       prompt.Type,
			"reason":     prompt.Reason,
			"prompt_id":  prompt.ID,
			"created_at": prompt.CreatedAt,
			"break_min":  policy.BreakMinMinutes,
			"break_max":  policy.BreakMaxMinutes,
		})
	}
	return nil
}

func appendIfMissing(list []string, v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return list
	}
	for _, s := range list {
		if s == v {
			return list
		}
	}
	return append(list, v)
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

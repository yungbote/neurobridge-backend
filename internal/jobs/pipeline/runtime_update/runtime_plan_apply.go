package runtime_update

import (
	"encoding/json"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
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
		if p.metrics != nil {
			p.metrics.ObserveRuntimeProgress(progressState, progressConf)
		}
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
		banditStoreMap, banditBlocks := banditStore(nrRuntime)
		banditTouched := false
		handled := false
		if typ == types.EventRuntimePromptCompleted || typ == types.EventRuntimePromptDismissed {
			if pid := stringFromAny(data["prompt_id"]); pid != "" && pid == pending.ID {
				handled = true
				if typ == types.EventRuntimePromptCompleted && pending.BlockID != "" {
					completedBlocks = appendIfMissing(completedBlocks, pending.BlockID)
				}
				if p.metrics != nil {
					if typ == types.EventRuntimePromptCompleted {
						p.metrics.IncRuntimePrompt(pending.Type, "completed")
					} else {
						p.metrics.IncRuntimePrompt(pending.Type, "dismissed")
					}
				}
				if pending.BlockID != "" {
					stats := banditBlock(banditBlocks, pending.BlockID)
					if typ == types.EventRuntimePromptCompleted {
						stats["completed"] = intFromAny(stats["completed"], 0) + 1
					} else {
						stats["dismissed"] = intFromAny(stats["dismissed"], 0) + 1
					}
					banditTouched = true
				}
			}
		}
		if typ == types.EventQuestionAnswered && pending.Type == "quick_check" && pending.BlockID != "" && pending.BlockID == blockID {
			if boolFromAny(data["is_correct"]) {
				handled = true
				completedBlocks = appendIfMissing(completedBlocks, pending.BlockID)
			}
			if p.metrics != nil {
				isCorrect := boolFromAny(data["is_correct"])
				p.metrics.IncQuickCheckAnswered(isCorrect)
				if isCorrect {
					p.metrics.IncRuntimePrompt("quick_check", "answered_correct")
				} else {
					p.metrics.IncRuntimePrompt("quick_check", "answered_incorrect")
				}
			}
			stats := banditBlock(banditBlocks, pending.BlockID)
			stats["attempts"] = intFromAny(stats["attempts"], 0) + 1
			if boolFromAny(data["is_correct"]) {
				stats["correct"] = intFromAny(stats["correct"], 0) + 1
			}
			banditTouched = true
		}
		if handled {
			if pending.DecisionTraceID != "" && p.traces != nil {
				outcome := map[string]any{
					"updated_at": now.Format(time.RFC3339),
				}
				switch typ {
				case types.EventRuntimePromptCompleted:
					outcome["outcome_event"] = "runtime_prompt_completed"
					outcome["reward"] = 1.0
				case types.EventRuntimePromptDismissed:
					outcome["outcome_event"] = "runtime_prompt_dismissed"
					outcome["reward"] = 0.0
				case types.EventQuestionAnswered:
					outcome["outcome_event"] = "question_answered"
					if boolFromAny(data["is_correct"]) {
						outcome["reward"] = 1.0
						outcome["is_correct"] = true
					} else {
						outcome["reward"] = 0.0
						outcome["is_correct"] = false
					}
				}
				if err := updateDecisionTraceOutcome(dbc, p.traces, pending.DecisionTraceID, outcome); err == nil && p.jobSvc != nil {
					policyKey := pending.PolicyKey
					if policyKey == "" {
						policyKey = runtimeRLPolicyKey()
					}
					_, _, _ = p.jobSvc.EnqueuePolicyEvalRefreshIfNeeded(dbc, userID, policyKey, "runtime_prompt_outcome")
					_, _, _ = p.jobSvc.EnqueuePolicyModelTrainIfNeeded(dbc, userID, policyKey, "runtime_prompt_outcome")
				}
			}

			prRuntime["runtime_prompt"] = nil
			prRuntime["last_prompt_at"] = now.Format(time.RFC3339)
			if typ == types.EventRuntimePromptCompleted {
				prRuntime["last_prompt_status"] = "completed"
			} else if typ == types.EventRuntimePromptDismissed {
				prRuntime["last_prompt_status"] = "dismissed"
			}
			if banditTouched {
				nrRuntime["bandit"] = banditStoreMap
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

	fatigueScore := 0.0
	breakReason := "time_elapsed"
	if fatigueEnabled() {
		fatigueScore = computeFatigue(prRuntime, promptsInWindow, failStreak, now)
		prRuntime["fatigue_score"] = fatigueScore
		prRuntime["fatigue_at"] = now.Format(time.RFC3339)

		if (progressOk || !progressSignal) && fatigueScore >= fatigueBreakThreshold() {
			lastBreakAt := timeFromAny(prRuntime["last_break_at"])
			minGap := float64(fatigueMinBreakGapMinutes())
			if lastBreakAt == nil || now.Sub(*lastBreakAt).Minutes() >= minGap {
				shouldBreak = true
				breakReason = "fatigue"
			}
		}
		if fatigueScore >= fatigueSuppressThreshold() {
			shouldQuick = false
			shouldFlash = false
		}
	}

	readinessCached := mapFromAny(nrRuntime["readiness"])
	readinessStatus := strings.ToLower(strings.TrimSpace(stringFromAny(readinessCached["status"])))
	readinessFresh := false
	if ts := timeFromAny(readinessCached["computed_at"]); ts != nil {
		if now.Sub(*ts) <= time.Duration(readinessCacheSeconds())*time.Second {
			readinessFresh = true
		}
	}
	readinessRefresh := false
	if readinessEnabled() || banditEnabled() || counterfactualEnabled() {
		if !readinessFresh {
			readinessRefresh = true
		}
		switch typ {
		case types.EventQuestionAnswered, types.EventRuntimePromptCompleted, types.EventRuntimePromptDismissed, types.EventQuizCompleted, types.EventActivityCompleted:
			readinessRefresh = true
		}
		if (shouldQuick || shouldFlash) && (banditEnabled() || counterfactualEnabled()) {
			readinessRefresh = true
		}
	}

	needDoc := shouldQuick || shouldFlash || readinessRefresh
	if !shouldBreak && !needDoc {
		prMeta["runtime"] = prRuntime
		nrMeta["runtime"] = nrRuntime
		pr.Metadata = encodeJSONMap(prMeta)
		nr.Metadata = encodeJSONMap(nrMeta)
		_ = p.pathRuns.Upsert(dbc, pr)
		_ = p.nodeRuns.Upsert(dbc, nr)
		return nil
	}

	doc := content.NodeDocV1{}
	if needDoc {
		docRow, err := p.nodeDocs.GetByPathNodeID(dbc, nodeID)
		if err != nil || docRow == nil {
			return nil
		}
		if err := json.Unmarshal(docRow.DocJSON, &doc); err != nil {
			return nil
		}
	}

	var readiness readinessResult
	readinessScore := 0.0
	if needDoc && readinessRefresh {
		readiness = computeReadiness(dbc, userID, pathID, doc, p.concepts, p.conStates, p.miscons)
		if readiness.Snapshot != nil {
			readinessStatus = strings.ToLower(strings.TrimSpace(readiness.Snapshot.Status))
			nrRuntime["readiness"] = readinessToMap(readiness.Snapshot)
			readinessScore = readiness.Snapshot.Score
		}
	}

	if readinessStatus == "not_ready" && (progressOk || !progressSignal) {
		if quickChecksShown < policy.QuickCheckMaxPerLesson {
			shouldQuick = true
		}
		if flashcardsShown < policy.FlashcardMaxPerLesson {
			shouldFlash = true
		}
	}

	testletStateByID := map[string]*types.UserTestletState{}
	if testletEnabled() && p.testlets != nil && needDoc {
		testletIDs := []string{}
		for _, b := range doc.Blocks {
			if b == nil {
				continue
			}
			kind := strings.ToLower(strings.TrimSpace(stringFromAny(b["type"])))
			if kind != "quick_check" && kind != "flashcard" {
				continue
			}
			conceptKeys := stringSliceFromAny(b["concept_keys"])
			tid := inferTestletID(b, kind, conceptKeys)
			if tid != "" {
				testletIDs = append(testletIDs, tid)
			}
		}
		if len(testletIDs) > 0 {
			if rows, err := p.testlets.ListByUserAndTestletIDs(dbc, userID, testletIDs); err == nil {
				for _, r := range rows {
					if r == nil || r.TestletID == "" {
						continue
					}
					testletStateByID[r.TestletID] = r
				}
			}
		}
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

	banditStoreMap, banditBlocks := banditStore(nrRuntime)
	totalShown := 0
	for _, raw := range banditBlocks {
		stats := mapFromAny(raw)
		totalShown += intFromAny(stats["shown"], 0)
	}

	conceptByKey := readiness.ConceptByKey
	if conceptByKey == nil {
		conceptByKey = map[string]*types.Concept{}
	}
	conceptState := readiness.ConceptState
	if conceptState == nil {
		conceptState = map[uuid.UUID]*types.UserConceptState{}
	}
	misconBy := readiness.MisconceptionBy
	if misconBy == nil {
		misconBy = map[uuid.UUID]float64{}
	}
	counterfactualTrigger := counterfactualEnabled() && (failStreak >= counterfactualFailStreak() || len(misconBy) > 0)

	buildCandidate := func(kind string, idx int, block map[string]any) (promptCandidate, bool) {
		id := strings.TrimSpace(stringFromAny(block["id"]))
		if id == "" {
			return promptCandidate{}, false
		}
		conceptIDs := extractConceptIDs(block, conceptByKey)
		conceptKeys := []string{}
		for _, cid := range conceptIDs {
			if key := readiness.ConceptKeyByID[cid]; key != "" {
				conceptKeys = appendIfMissing(conceptKeys, key)
			}
		}
		testletID := ""
		testletType := ""
		testletUnc := 0.0
		if testletEnabled() {
			testletType = inferTestletType(kind)
			testletID = inferTestletID(block, kind, conceptKeys)
			if st, ok := testletStateByID[testletID]; ok && st != nil {
				testletUnc = testletUncertainty(st)
			}
		}
		infoGain := computeInfoGain(conceptIDs, conceptState)
		if infoGain < banditMinInfoGain() && !counterfactualTrigger {
			return promptCandidate{}, false
		}

		counterfactual := false
		for _, cid := range conceptIDs {
			if _, ok := misconBy[cid]; ok {
				counterfactual = true
				break
			}
		}
		if !counterfactual && counterfactualTrigger && infoGain >= banditMinInfoGain() {
			counterfactual = true
		}

		stats := banditBlock(banditBlocks, id)
		shown := intFromAny(stats["shown"], 0)
		lastShownAt := timeFromAny(stats["last_shown_at"])

		score := infoGain
		scoreParts := map[string]float64{"info_gain": infoGain}
		if banditEnabled() {
			explore := 0.0
			if totalShown > 0 {
				explore = math.Sqrt(math.Log(float64(totalShown)+1.0) / float64(shown+1))
			}
			explore *= banditExplorationWeight()
			score += explore
			scoreParts["explore"] = explore
		}
		if testletUnc > 0 {
			boost := testletUnc * testletUncertaintyWeight()
			score += boost
			scoreParts["testlet_uncertainty"] = boost
		}
		if readinessStatus == "not_ready" {
			boost := readinessPromptBoost()
			score += boost
			scoreParts["readiness_boost"] = boost
		} else if readinessStatus == "uncertain" {
			boost := readinessPromptBoost() * 0.5
			score += boost
			scoreParts["readiness_boost"] = boost
		}
		if counterfactual {
			boost := counterfactualBoost()
			score += boost
			scoreParts["counterfactual_boost"] = boost
		}
		if banditEnabled() && lastShownAt != nil {
			recencyWindow := float64(banditRecencyPenaltyMinutes())
			if recencyWindow > 0 {
				minutesSince := now.Sub(*lastShownAt).Minutes()
				if minutesSince < recencyWindow {
					penalty := (recencyWindow - minutesSince) / recencyWindow
					score -= penalty * 0.25
					scoreParts["recency_penalty"] = penalty * 0.25
				}
			}
		}

		reason := "cadence"
		if counterfactual {
			reason = "counterfactual_probe"
		} else if readinessStatus == "not_ready" {
			reason = "readiness_not_ready"
		} else if banditEnabled() {
			reason = "bandit_info_gain"
		}

		policyFeatures := map[string]float64{}
		for k, v := range scoreParts {
			policyFeatures[k] = v
		}
		policyFeatures["fatigue_score"] = fatigueScore
		policyFeatures["progress_confidence"] = progressConf
		policyFeatures["readiness_score"] = readinessScore
		policyFeatures["fail_streak"] = float64(failStreak)
		if counterfactual {
			policyFeatures["counterfactual"] = 1
		} else {
			policyFeatures["counterfactual"] = 0
		}
		if strings.EqualFold(kind, "quick_check") {
			policyFeatures["kind_quick_check"] = 1
		} else if strings.EqualFold(kind, "flashcard") {
			policyFeatures["kind_flashcard"] = 1
		}

		return promptCandidate{
			BlockID:            id,
			Kind:               kind,
			Index:              idx,
			ConceptIDs:         conceptIDs,
			ConceptKeys:        conceptKeys,
			TestletID:          testletID,
			TestletType:        testletType,
			TestletUncertainty: testletUnc,
			InfoGain:           infoGain,
			Counterfactual:     counterfactual,
			Score:              score,
			PolicyFeatures:     policyFeatures,
			ScoreComponents:    scoreParts,
			Reason:             reason,
		}, true
	}

	collectCandidates := func(kind string) []promptCandidate {
		out := []promptCandidate{}
		for i, b := range doc.Blocks {
			if !strings.EqualFold(stringFromAny(b["type"]), kind) {
				continue
			}
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
			if cand, ok := buildCandidate(kind, i, b); ok {
				out = append(out, cand)
			}
		}
		return out
	}

	selectBest := func(list []promptCandidate) *promptCandidate {
		if len(list) == 0 {
			return nil
		}
		best := list[0]
		for i := 1; i < len(list); i++ {
			if list[i].Score > best.Score {
				best = list[i]
			}
		}
		return &best
	}

	candidates := []promptCandidate{}
	if shouldQuick {
		candidates = append(candidates, collectCandidates("quick_check")...)
	}
	if shouldFlash {
		candidates = append(candidates, collectCandidates("flashcard")...)
	}

	policyKey := runtimeRLPolicyKey()
	policyMode := runtimeRLMode()
	policyVersion := 0
	policyWeights := map[string]float64{}
	policyBias := 0.0
	hasPolicyModel := false
	if policyMode != "off" && p.models != nil {
		if model := loadActiveModelSnapshot(dbc, p.models, policyKey); model != nil {
			policyVersion = model.Version
			policyWeights, policyBias = decodePolicyWeights(model.ParamsJSON)
			if len(policyWeights) > 0 || policyBias != 0 {
				hasPolicyModel = true
			}
		}
	}
	if len(candidates) > 0 {
		baselineProbs := softmaxScores(candidates, func(c promptCandidate) float64 { return c.Score }, 1.0)
		for i := range candidates {
			candidates[i].BaselineProb = baselineProbs[i]
		}
		if hasPolicyModel {
			for i := range candidates {
				candidates[i].PolicyScore = computePolicyScore(candidates[i], policyBias, policyWeights)
			}
			policyProbs := softmaxScores(candidates, func(c promptCandidate) float64 { return c.PolicyScore }, runtimeRLSoftmaxTemp())
			for i := range candidates {
				candidates[i].PolicyProb = policyProbs[i]
			}
		} else {
			for i := range candidates {
				candidates[i].PolicyScore = candidates[i].Score
				candidates[i].PolicyProb = candidates[i].BaselineProb
			}
		}
	}

	policyModeUsed := "baseline"
	behaviorPolicyKey := "bandit_v1"
	shadowPolicyKey := ""
	policySafe := false
	if hasPolicyModel && policyMode != "off" {
		shadowPolicyKey = policyKey
		if policyMode == "active" && p.evals != nil {
			if snap, err := p.evals.GetLatestByKey(dbc, policyKey); err == nil {
				policySafe = policySafeToActivate(snap)
			}
		}
		if policyMode == "active" && policySafe && rolloutEligible(userID, runtimeRLRolloutPct()) {
			policyModeUsed = "active"
			behaviorPolicyKey = policyKey
		} else {
			policyModeUsed = "shadow"
		}
	}

	var selected *promptCandidate
	if len(candidates) > 0 {
		if policyModeUsed == "active" {
			selected = selectBestByPolicyScore(candidates)
		} else {
			selected = selectBest(candidates)
		}
		if selected != nil {
			if policyModeUsed == "active" {
				selected.BehaviorProb = selected.PolicyProb
				selected.ShadowProb = selected.BaselineProb
			} else {
				selected.BehaviorProb = selected.BaselineProb
				selected.ShadowProb = selected.PolicyProb
			}
		}
	}

	var prompt runtimePrompt
	switch {
	case shouldBreak:
		prompt = runtimePrompt{
			ID:        uuid.New().String(),
			Type:      "break",
			NodeID:    nodeID.String(),
			Status:    "pending",
			Reason:    breakReason,
			CreatedAt: now.Format(time.RFC3339),
		}
	case selected != nil:
		prompt = runtimePrompt{
			ID:            uuid.New().String(),
			Type:          selected.Kind,
			NodeID:        nodeID.String(),
			BlockID:       selected.BlockID,
			Status:        "pending",
			Reason:        selected.Reason,
			CreatedAt:     now.Format(time.RFC3339),
			PolicyKey:     behaviorPolicyKey,
			PolicyMode:    policyModeUsed,
			PolicyVersion: policyVersion,
			BehaviorProb:  selected.BehaviorProb,
			ShadowProb:    selected.ShadowProb,
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
	if p.metrics != nil {
		p.metrics.IncRuntimePrompt(prompt.Type, "shown")
	}

	if selected != nil && (prompt.Type == "quick_check" || prompt.Type == "flashcard") && p.traces != nil {
		traceID := uuid.New()
		prompt.DecisionTraceID = traceID.String()

		candPayload := make([]map[string]any, 0, len(candidates))
		for _, cand := range candidates {
			candPayload = append(candPayload, map[string]any{
				"block_id":            cand.BlockID,
				"kind":                cand.Kind,
				"index":               cand.Index,
				"info_gain":           cand.InfoGain,
				"score":               cand.Score,
				"policy_score":        cand.PolicyScore,
				"baseline_prob":       cand.BaselineProb,
				"policy_prob":         cand.PolicyProb,
				"testlet_id":          cand.TestletID,
				"testlet_type":        cand.TestletType,
				"testlet_uncertainty": cand.TestletUncertainty,
				"counterfactual":      cand.Counterfactual,
				"concept_keys":        cand.ConceptKeys,
				"score_components":    cand.ScoreComponents,
				"policy_features":     cand.PolicyFeatures,
			})
		}

		chosenPayload := map[string]any{
			"block_id":          selected.BlockID,
			"kind":              selected.Kind,
			"prompt_id":         prompt.ID,
			"policy_key":        behaviorPolicyKey,
			"policy_mode":       policyModeUsed,
			"policy_version":    policyVersion,
			"policy_prob":       selected.PolicyProb,
			"baseline_prob":     selected.BaselineProb,
			"behavior_prob":     selected.BehaviorProb,
			"shadow_policy_key": shadowPolicyKey,
			"shadow_prob":       selected.ShadowProb,
			"policy_score":      selected.PolicyScore,
			"baseline_score":    selected.Score,
			"testlet_id":        selected.TestletID,
			"testlet_type":      selected.TestletType,
			"policy_features":   selected.PolicyFeatures,
			"created_at":        now.Format(time.RFC3339),
		}

		inputs := map[string]any{
			"path_id":             pathID.String(),
			"node_id":             nodeID.String(),
			"readiness_status":    readinessStatus,
			"readiness_score":     readinessScore,
			"fatigue_score":       fatigueScore,
			"progress_state":      progressState,
			"progress_confidence": progressConf,
			"policy_mode":         policyModeUsed,
			"policy_key":          behaviorPolicyKey,
			"shadow_policy_key":   shadowPolicyKey,
			"candidate_count":     len(candidates),
		}

		pathIDCopy := pathID
		trace := &types.DecisionTrace{
			ID:           traceID,
			UserID:       userID,
			OccurredAt:   now,
			DecisionType: "runtime_prompt",
			PathID:       &pathIDCopy,
			Inputs:       datatypes.JSON(mustJSON(inputs)),
			Candidates:   datatypes.JSON(mustJSON(candPayload)),
			Chosen:       datatypes.JSON(mustJSON(chosenPayload)),
		}
		if _, err := p.traces.Create(dbc, []*types.DecisionTrace{trace}); err != nil {
			p.log.Debug("decision trace create failed", "error", err.Error())
		}
	}

	if selected != nil && (prompt.Type == "quick_check" || prompt.Type == "flashcard") {
		stats := banditBlock(banditBlocks, prompt.BlockID)
		stats["shown"] = intFromAny(stats["shown"], 0) + 1
		stats["last_shown_at"] = now.Format(time.RFC3339)
		stats["last_reason"] = prompt.Reason
		stats["last_score"] = selected.Score
		stats["last_info_gain"] = selected.InfoGain
		nrRuntime["bandit"] = banditStoreMap
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

type modelSnapshotLister interface {
	ListByKey(dbc dbctx.Context, key string, limit int) ([]*types.ModelSnapshot, error)
	GetLatestByKey(dbc dbctx.Context, key string) (*types.ModelSnapshot, error)
}

func loadActiveModelSnapshot(dbc dbctx.Context, repo modelSnapshotLister, key string) *types.ModelSnapshot {
	if repo == nil || strings.TrimSpace(key) == "" {
		return nil
	}
	rows, err := repo.ListByKey(dbc, key, 10)
	if err == nil {
		for _, row := range rows {
			if row != nil && row.Active {
				return row
			}
		}
		if len(rows) > 0 {
			return rows[0]
		}
	}
	if row, err := repo.GetLatestByKey(dbc, key); err == nil {
		return row
	}
	return nil
}

func decodePolicyWeights(raw datatypes.JSON) (map[string]float64, float64) {
	out := map[string]float64{}
	bias := 0.0
	if len(raw) == 0 || string(raw) == "null" {
		return out, bias
	}
	payload := map[string]any{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return out, bias
	}
	bias = floatFromAny(payload["bias"], bias)
	if weights, ok := payload["weights"].(map[string]any); ok {
		for k, v := range weights {
			out[k] = floatFromAny(v, 0)
		}
	}
	return out, bias
}

func computePolicyScore(c promptCandidate, bias float64, weights map[string]float64) float64 {
	score := bias
	for k, v := range c.PolicyFeatures {
		if w, ok := weights[k]; ok {
			score += w * v
		}
	}
	return score
}

func softmaxScores(cands []promptCandidate, scoreFn func(promptCandidate) float64, temp float64) []float64 {
	if len(cands) == 0 {
		return []float64{}
	}
	if temp <= 0 {
		temp = 1.0
	}
	maxScore := scoreFn(cands[0]) / temp
	for _, c := range cands[1:] {
		val := scoreFn(c) / temp
		if val > maxScore {
			maxScore = val
		}
	}
	sum := 0.0
	out := make([]float64, len(cands))
	for i, c := range cands {
		v := math.Exp((scoreFn(c) / temp) - maxScore)
		out[i] = v
		sum += v
	}
	if sum <= 0 {
		share := 1.0 / float64(len(out))
		for i := range out {
			out[i] = share
		}
		return out
	}
	for i := range out {
		out[i] = out[i] / sum
	}
	return out
}

func selectBestByPolicyScore(list []promptCandidate) *promptCandidate {
	if len(list) == 0 {
		return nil
	}
	best := list[0]
	for i := 1; i < len(list); i++ {
		if list[i].PolicyScore > best.PolicyScore {
			best = list[i]
		}
	}
	return &best
}

func policySafeToActivate(snap *types.PolicyEvalSnapshot) bool {
	if snap == nil {
		return false
	}
	if snap.Samples < runtimeRLSafeMinSamples() {
		return false
	}
	if snap.IPS < runtimeRLSafeMinIPS() {
		return false
	}
	if snap.Lift < runtimeRLSafeMinLift() {
		return false
	}
	return true
}

func rolloutEligible(userID uuid.UUID, pct float64) bool {
	if pct >= 1.0 {
		return true
	}
	if pct <= 0 || userID == uuid.Nil {
		return false
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(userID.String()))
	val := float64(h.Sum32()%10000) / 10000.0
	return val < pct
}

func inferTestletType(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		return "quick_check"
	}
	return kind
}

func inferTestletID(block map[string]any, kind string, conceptKeys []string) string {
	if block == nil {
		return ""
	}
	if v := strings.TrimSpace(stringFromAny(block["testlet_id"])); v != "" {
		return v
	}
	if v := strings.TrimSpace(stringFromAny(block["testlet_key"])); v != "" {
		return v
	}
	keys := []string{}
	for _, k := range conceptKeys {
		k = strings.TrimSpace(strings.ToLower(k))
		if k == "" {
			continue
		}
		keys = append(keys, k)
	}
	if len(keys) > 0 {
		sort.Strings(keys)
		return inferTestletType(kind) + ":" + strings.Join(keys, "|")
	}
	if v := strings.TrimSpace(stringFromAny(block["id"])); v != "" {
		return inferTestletType(kind) + ":" + v
	}
	return inferTestletType(kind)
}

func testletUncertainty(st *types.UserTestletState) float64 {
	if st == nil {
		return 0.5
	}
	a := st.BetaA
	b := st.BetaB
	if a <= 0 {
		a = 1
	}
	if b <= 0 {
		b = 1
	}
	variance := betaVariance(a, b)
	if variance <= 0 {
		return 0
	}
	// Max variance for Beta is 0.25 at a=b=1.
	return clamp01(variance / 0.25)
}

func betaVariance(a float64, b float64) float64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	sum := a + b
	return (a * b) / (sum * sum * (sum + 1))
}

func updateDecisionTraceOutcome(dbc dbctx.Context, repo repos.DecisionTraceRepo, traceID string, updates map[string]any) error {
	if repo == nil || strings.TrimSpace(traceID) == "" {
		return nil
	}
	id, err := uuid.Parse(traceID)
	if err != nil {
		return nil
	}
	rows, err := repo.GetByIDs(dbc, []uuid.UUID{id})
	if err != nil || len(rows) == 0 || rows[0] == nil {
		return err
	}
	chosen := mapFromAny(rows[0].Chosen)
	for k, v := range updates {
		chosen[k] = v
	}
	return repo.UpdateChosen(dbc, id, encodeJSONMap(chosen))
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

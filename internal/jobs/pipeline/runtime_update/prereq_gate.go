package runtime_update

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

const prereqGateSchemaVersion = 1

type prereqReadinessSnapshot struct {
	Status                string             `json:"status"`
	Score                 float64            `json:"score"`
	AvgMastery            float64            `json:"avg_mastery"`
	MinMastery            float64            `json:"min_mastery"`
	MaxUncertainty        float64            `json:"max_uncertainty"`
	CoverageDebtMax       float64            `json:"coverage_debt_max"`
	ConceptsTotal         int                `json:"concepts_total"`
	ConceptsMissing       int                `json:"concepts_missing"`
	MisconceptionsActive  int                `json:"misconceptions_active"`
	WeakConcepts          []string           `json:"weak_concepts"`
	UncertainConcepts     []string           `json:"uncertain_concepts"`
	MisconceptionConcepts []string           `json:"misconception_concepts"`
	DueReviewConcepts     []string           `json:"due_review_concepts"`
	PrereqConceptKeys     []string           `json:"prereq_concept_keys"`
	FrameBridgeFrom       string             `json:"frame_bridge_from,omitempty"`
	FrameBridgeTo         string             `json:"frame_bridge_to,omitempty"`
	FrameBridgeMD         string             `json:"frame_bridge_md,omitempty"`
	EscalationAction      string             `json:"escalation_action,omitempty"`
	EscalationReason      string             `json:"escalation_reason,omitempty"`
	Weights               map[string]float64 `json:"weights"`
	ComputedAt            string             `json:"computed_at"`
}

func (p *Pipeline) applyPrereqGate(dbc dbctx.Context, userID uuid.UUID, pathID uuid.UUID, nodeID uuid.UUID, now time.Time) error {
	if !prereqGateEnabled() || userID == uuid.Nil || pathID == uuid.Nil || nodeID == uuid.Nil {
		return nil
	}
	if p.pathNodes == nil || p.concepts == nil || p.conStates == nil || p.readiness == nil || p.gates == nil {
		return nil
	}
	if p.db != nil && !hasTable(p.db, &types.ConceptReadinessSnapshot{}) {
		return nil
	}
	if p.db != nil && !hasTable(p.db, &types.PrereqGateDecision{}) {
		return nil
	}

	failStreak := 0
	// Debounce frequent node-open events.
	if p.nodeRuns != nil {
		if nr, _ := p.nodeRuns.GetByUserAndNodeID(dbc, userID, nodeID); nr != nil {
			nrMeta := decodeJSONMap(nr.Metadata)
			nrRuntime := mapFromAny(nrMeta["runtime"])
			failStreak = intFromAny(nrRuntime["fail_streak"], 0)
			gate := mapFromAny(nrRuntime["prereq_gate"])
			if ts := timeFromAny(gate["computed_at"]); ts != nil {
				if now.Sub(*ts) <= time.Duration(prereqGateCacheSeconds())*time.Second {
					return nil
				}
			}
		}
	}

	node, err := p.pathNodes.GetByID(dbc, nodeID)
	if err != nil || node == nil {
		return nil
	}
	meta := decodeJSONMap(node.Metadata)
	prereqKeys := normalizeKeys(stringSliceFromAny(meta["prereq_concept_keys"]))
	conceptKeys := normalizeKeys(stringSliceFromAny(meta["concept_keys"]))
	allowedFrames := normalizeKeysPreserveOrder(stringSliceFromAny(meta["allowed_frames"]))

	// Resolve concept IDs for keys.
	allKeys := uniqueStrings(append(append([]string{}, prereqKeys...), conceptKeys...))
	canonicalByKey := map[string]uuid.UUID{}
	keyByID := map[uuid.UUID]string{}
	if len(allKeys) > 0 {
		rows, _ := p.concepts.GetByScopeAndKeys(dbc, "path", &pathID, allKeys)
		if len(rows) == 0 {
			rows, _ = p.concepts.GetByScopeAndKeys(dbc, "global", nil, allKeys)
		}
		for _, c := range rows {
			if c == nil || c.ID == uuid.Nil {
				continue
			}
			key := strings.TrimSpace(strings.ToLower(c.Key))
			if key == "" {
				continue
			}
			cid := c.ID
			if c.CanonicalConceptID != nil && *c.CanonicalConceptID != uuid.Nil {
				cid = *c.CanonicalConceptID
			}
			if cid == uuid.Nil {
				continue
			}
			canonicalByKey[key] = cid
			if keyByID[cid] == "" {
				keyByID[cid] = key
			}
		}
	}

	weightsByID := map[uuid.UUID]float64{}
	missingKeys := []string{}
	for _, k := range prereqKeys {
		if id := canonicalByKey[k]; id != uuid.Nil {
			if prev, ok := weightsByID[id]; !ok || 1.0 > prev {
				weightsByID[id] = 1.0
			}
		} else if k != "" {
			missingKeys = append(missingKeys, k)
		}
	}

	// Add weighted prereqs from concept edges.
	if p.edges != nil && len(conceptKeys) > 0 {
		toIDs := []uuid.UUID{}
		for _, k := range conceptKeys {
			if id := canonicalByKey[k]; id != uuid.Nil {
				toIDs = append(toIDs, id)
			}
		}
		if len(toIDs) > 0 {
			if edges, _ := p.edges.GetByToConceptIDs(dbc, toIDs); len(edges) > 0 {
				for _, e := range edges {
					if e == nil || e.FromConceptID == uuid.Nil {
						continue
					}
					if !strings.EqualFold(strings.TrimSpace(e.EdgeType), "prereq") {
						continue
					}
					strength := clamp01(e.Strength)
					if strength < prereqEdgeMinStrength() {
						continue
					}
					if prev, ok := weightsByID[e.FromConceptID]; !ok || strength > prev {
						weightsByID[e.FromConceptID] = strength
					}
				}
			}
		}
	}

	// Normalize to canonical IDs for edge-derived concepts.
	if len(weightsByID) > 0 {
		rawIDs := make([]uuid.UUID, 0, len(weightsByID))
		for id := range weightsByID {
			rawIDs = append(rawIDs, id)
		}
		if rows, _ := p.concepts.GetByIDs(dbc, rawIDs); len(rows) > 0 {
			rawToCanon := map[uuid.UUID]uuid.UUID{}
			for _, c := range rows {
				if c == nil || c.ID == uuid.Nil {
					continue
				}
				cid := c.ID
				if c.CanonicalConceptID != nil && *c.CanonicalConceptID != uuid.Nil {
					cid = *c.CanonicalConceptID
				}
				rawToCanon[c.ID] = cid
				key := strings.TrimSpace(strings.ToLower(c.Key))
				if key != "" && keyByID[cid] == "" {
					keyByID[cid] = key
				}
			}
			if len(rawToCanon) > 0 {
				normalized := map[uuid.UUID]float64{}
				for rawID, weight := range weightsByID {
					cid := rawToCanon[rawID]
					if cid == uuid.Nil {
						cid = rawID
					}
					if prev, ok := normalized[cid]; !ok || weight > prev {
						normalized[cid] = weight
					}
				}
				weightsByID = normalized
			}
		}
	}

	prereqIDs := make([]uuid.UUID, 0, len(weightsByID))
	for id := range weightsByID {
		prereqIDs = append(prereqIDs, id)
	}

	stateByID := map[uuid.UUID]*types.UserConceptState{}
	if len(prereqIDs) > 0 {
		if states, _ := p.conStates.ListByUserAndConceptIDs(dbc, userID, prereqIDs); len(states) > 0 {
			for _, st := range states {
				if st != nil && st.ConceptID != uuid.Nil {
					stateByID[st.ConceptID] = st
				}
			}
		}
	}

	misconByID := map[uuid.UUID]float64{}
	if p.miscons != nil && len(prereqIDs) > 0 {
		if miscons, _ := p.miscons.ListActiveByUserAndConceptIDs(dbc, userID, prereqIDs); len(miscons) > 0 {
			for _, m := range miscons {
				if m == nil || m.CanonicalConceptID == uuid.Nil {
					continue
				}
				conf := m.Confidence
				if conf <= 0 {
					conf = 0.5
				}
				if prev, ok := misconByID[m.CanonicalConceptID]; !ok || conf > prev {
					misconByID[m.CanonicalConceptID] = conf
				}
			}
		}
	}

	dueReview := []string{}
	escalationAction := ""
	escalationReason := ""
	if p.misconRes != nil && len(prereqIDs) > 0 {
		if rows, _ := p.misconRes.ListByUserAndConceptIDs(dbc, userID, prereqIDs); len(rows) > 0 {
			for _, r := range rows {
				if r == nil || r.ConceptID == uuid.Nil {
					continue
				}
				if strings.EqualFold(r.Status, "resolved") && r.NextReviewAt != nil && !r.NextReviewAt.After(now) {
					if key := keyByID[r.ConceptID]; key != "" {
						dueReview = append(dueReview, key)
					}
				}
				if escalationAction == "" && r.IncorrectCount >= escalationFailCount() {
					escalationAction = "alternate_modality"
					escalationReason = "misconception_fail"
				}
			}
		}
	}
	if escalationAction == "" && failStreak >= escalationFailCount() {
		escalationAction = "guided_recap"
		escalationReason = "fail_streak"
	}

	frameBridgeFrom := ""
	frameBridgeTo := ""
	frameBridgeMD := ""
	if frameBridgeEnabled() && len(allowedFrames) > 0 && p.conModels != nil && len(prereqIDs) > 0 {
		if models, _ := p.conModels.ListByUserAndConceptIDs(dbc, userID, prereqIDs); len(models) > 0 {
			frameScores := map[string]float64{}
			for _, m := range models {
				if m == nil || len(m.ActiveFrames) == 0 {
					continue
				}
				var frames []docFrameSignal
				_ = json.Unmarshal(m.ActiveFrames, &frames)
				for _, f := range frames {
					name := strings.TrimSpace(strings.ToLower(f.Frame))
					if name == "" {
						continue
					}
					score := clamp01(f.Confidence)
					if prev, ok := frameScores[name]; !ok || score > prev {
						frameScores[name] = score
					}
				}
			}
			topFrame, topScore := topFrameSignal(frameScores)
			if topFrame != "" && topScore >= frameBridgeMinConfidence() && !containsString(allowedFrames, topFrame) {
				frameBridgeFrom = topFrame
				frameBridgeTo = allowedFrames[0]
				frameBridgeMD = buildFrameBridge(frameBridgeFrom, frameBridgeTo)
			}
		}
	}

	score := 0.0
	totalWeight := 0.0
	minMastery := 1.0
	maxUnc := 0.0
	coverageDebtMax := 0.0
	missing := len(missingKeys)
	weak := append([]string{}, missingKeys...)
	uncertain := []string{}
	misconcepts := []string{}
	if missing > 0 {
		minMastery = 0
	}

	for id, weight := range weightsByID {
		if weight <= 0 {
			continue
		}
		key := keyByID[id]
		st := stateByID[id]
		if st == nil {
			missing++
			minMastery = math.Min(minMastery, 0)
			if key != "" {
				weak = append(weak, key)
			}
			totalWeight += weight
			continue
		}
		mastery, conf, unc, debt, _ := deriveConceptSignals(st, now)
		if debt > coverageDebtMax {
			coverageDebtMax = debt
		}
		effective := mastery * (0.4 + 0.6*conf)
		if debt > 0 {
			effective = effective * (1 - 0.2*debt)
		}
		score += effective * weight
		totalWeight += weight
		minMastery = math.Min(minMastery, mastery)
		if unc > maxUnc {
			maxUnc = unc
		}
		if mastery < readinessMinMastery() && key != "" {
			weak = append(weak, key)
		}
		if unc > 0.6 && key != "" {
			uncertain = append(uncertain, key)
		}
		if debt >= coverageDebtThreshold() && key != "" {
			dueReview = append(dueReview, key)
		}
		if _, ok := misconByID[id]; ok && key != "" {
			misconcepts = append(misconcepts, key)
		}
	}

	if totalWeight > 0 {
		score = score / totalWeight
	} else if missing == 0 {
		score = 1
		minMastery = 1
	}

	status := "uncertain"
	if totalWeight == 0 && missing == 0 {
		status = "ready"
	} else if score >= readinessReadyMin() && minMastery >= readinessMinMastery() && len(misconByID) <= readinessMaxMisconceptionsReady() {
		status = "ready"
	} else if score < readinessUncertainMin() || len(misconByID) > readinessMaxMisconceptionsReady() {
		status = "not_ready"
	}

	weightsByKey := map[string]float64{}
	for id, weight := range weightsByID {
		if key := keyByID[id]; key != "" {
			weightsByKey[key] = weight
		}
	}

	snapshot := prereqReadinessSnapshot{
		Status:                status,
		Score:                 clamp01(score),
		AvgMastery:            clamp01(score),
		MinMastery:            clamp01(minMastery),
		MaxUncertainty:        clamp01(maxUnc),
		CoverageDebtMax:       clamp01(coverageDebtMax),
		ConceptsTotal:         len(weightsByID),
		ConceptsMissing:       missing,
		MisconceptionsActive:  len(misconByID),
		WeakConcepts:          uniqueStrings(weak),
		UncertainConcepts:     uniqueStrings(uncertain),
		MisconceptionConcepts: uniqueStrings(misconcepts),
		DueReviewConcepts:     uniqueStrings(dueReview),
		PrereqConceptKeys:     uniqueStrings(prereqKeys),
		FrameBridgeFrom:       frameBridgeFrom,
		FrameBridgeTo:         frameBridgeTo,
		FrameBridgeMD:         frameBridgeMD,
		EscalationAction:      escalationAction,
		EscalationReason:      escalationReason,
		Weights:               weightsByKey,
		ComputedAt:            now.UTC().Format(time.RFC3339),
	}

	if p.metrics != nil {
		p.metrics.ObserveConvergenceReadiness(snapshot.Status, snapshot.Score, snapshot.CoverageDebtMax, snapshot.MisconceptionsActive)
	}

	snapMap := map[string]any{
		"status":                 snapshot.Status,
		"score":                  snapshot.Score,
		"avg_mastery":            snapshot.AvgMastery,
		"min_mastery":            snapshot.MinMastery,
		"max_uncertainty":        snapshot.MaxUncertainty,
		"coverage_debt_max":      snapshot.CoverageDebtMax,
		"concepts_total":         snapshot.ConceptsTotal,
		"concepts_missing":       snapshot.ConceptsMissing,
		"misconceptions_active":  snapshot.MisconceptionsActive,
		"weak_concepts":          snapshot.WeakConcepts,
		"uncertain_concepts":     snapshot.UncertainConcepts,
		"misconception_concepts": snapshot.MisconceptionConcepts,
		"due_review_concepts":    snapshot.DueReviewConcepts,
		"prereq_concept_keys":    snapshot.PrereqConceptKeys,
		"frame_bridge_from":      snapshot.FrameBridgeFrom,
		"frame_bridge_to":        snapshot.FrameBridgeTo,
		"frame_bridge_md":        snapshot.FrameBridgeMD,
		"escalation_action":      snapshot.EscalationAction,
		"escalation_reason":      snapshot.EscalationReason,
		"weights":                snapshot.Weights,
		"computed_at":            snapshot.ComputedAt,
	}

	snapshotID := computePrereqSnapshotID(snapMap)
	policyVersion := prereqGatePolicyVersion()

	if p.readiness != nil {
		_ = p.readiness.Upsert(dbc, &types.ConceptReadinessSnapshot{
			UserID:        userID,
			PathID:        pathID,
			PathNodeID:    nodeID,
			SnapshotID:    snapshotID,
			PolicyVersion: policyVersion,
			SchemaVersion: prereqGateSchemaVersion,
			Status:        snapshot.Status,
			Score:         snapshot.Score,
			SnapshotJSON:  datatypes.JSON(mustJSON(snapMap)),
			CreatedAt:     now.UTC(),
		})
	}

	mode := prereqGateMode()
	decision := "allow"
	reason := "ready"
	if snapshot.Status == "not_ready" {
		if mode == "hard" {
			decision = "blocked"
			reason = "not_ready"
		} else {
			decision = "soft_remediate"
			reason = "not_ready"
		}
	} else if snapshot.Status == "uncertain" || len(snapshot.DueReviewConcepts) > 0 {
		decision = "soft_remediate"
		if len(snapshot.DueReviewConcepts) > 0 && snapshot.Status == "ready" {
			reason = "due_review"
		} else {
			reason = "uncertain"
		}
	}

	if p.metrics != nil {
		p.metrics.IncConvergenceGateDecision(decision, reason)
	}

	actions := []map[string]any{}
	for _, k := range snapshot.WeakConcepts {
		actions = append(actions, map[string]any{
			"type":         "review_prereq",
			"concept_keys": []string{k},
			"priority":     1,
		})
	}
	for _, k := range snapshot.MisconceptionConcepts {
		actions = append(actions, map[string]any{
			"type":         "counterfactual_probe",
			"concept_keys": []string{k},
			"priority":     2,
		})
	}
	for _, k := range snapshot.DueReviewConcepts {
		actions = append(actions, map[string]any{
			"type":         "reinforcement_probe",
			"concept_keys": []string{k},
			"priority":     3,
		})
	}
	if snapshot.FrameBridgeMD != "" {
		actions = append(actions, map[string]any{
			"type":          "frame_bridge",
			"from_frame":    snapshot.FrameBridgeFrom,
			"to_frame":      snapshot.FrameBridgeTo,
			"instructions":  snapshot.FrameBridgeMD,
			"priority":      3,
		})
	}
	if snapshot.EscalationAction != "" {
		actions = append(actions, map[string]any{
			"type":     "escalation",
			"action":   snapshot.EscalationAction,
			"reason":   snapshot.EscalationReason,
			"priority": 4,
		})
	}

	evidence := map[string]any{
		"status":                 snapshot.Status,
		"score":                  snapshot.Score,
		"mode":                   mode,
		"decision":               decision,
		"reason":                 reason,
		"weak_concepts":          snapshot.WeakConcepts,
		"uncertain_concepts":     snapshot.UncertainConcepts,
		"misconception_concepts": snapshot.MisconceptionConcepts,
		"due_review_concepts":    snapshot.DueReviewConcepts,
		"prereq_concept_keys":    snapshot.PrereqConceptKeys,
		"frame_bridge_from":      snapshot.FrameBridgeFrom,
		"frame_bridge_to":        snapshot.FrameBridgeTo,
		"frame_bridge_md":        snapshot.FrameBridgeMD,
		"escalation_action":      snapshot.EscalationAction,
		"escalation_reason":      snapshot.EscalationReason,
		"weights":                snapshot.Weights,
		"actions":                actions,
		"computed_at":            snapshot.ComputedAt,
	}

	if p.gates != nil {
		_ = p.gates.Upsert(dbc, &types.PrereqGateDecision{
			UserID:          userID,
			PathID:          pathID,
			PathNodeID:      nodeID,
			SnapshotID:      snapshotID,
			PolicyVersion:   policyVersion,
			SchemaVersion:   prereqGateSchemaVersion,
			ReadinessStatus: snapshot.Status,
			ReadinessScore:  snapshot.Score,
			GateMode:        mode,
			Decision:        decision,
			Reason:          reason,
			EvidenceJSON:    datatypes.JSON(mustJSON(evidence)),
			CreatedAt:       now.UTC(),
		})
	}

	if p.nodeRuns != nil {
		if nr, _ := p.nodeRuns.GetByUserAndNodeID(dbc, userID, nodeID); nr != nil {
			nrMeta := decodeJSONMap(nr.Metadata)
			nrRuntime := mapFromAny(nrMeta["runtime"])
			nrRuntime["prereq_gate"] = map[string]any{
				"status":                 snapshot.Status,
				"score":                  snapshot.Score,
				"decision":               decision,
				"mode":                   mode,
				"reason":                 reason,
				"weak_concepts":           snapshot.WeakConcepts,
				"uncertain_concepts":      snapshot.UncertainConcepts,
				"misconception_concepts":  snapshot.MisconceptionConcepts,
				"due_review_concepts":     snapshot.DueReviewConcepts,
				"frame_bridge_from":       snapshot.FrameBridgeFrom,
				"frame_bridge_to":         snapshot.FrameBridgeTo,
				"frame_bridge_md":         snapshot.FrameBridgeMD,
				"escalation_action":       snapshot.EscalationAction,
				"escalation_reason":       snapshot.EscalationReason,
				"snapshot_id":             snapshotID,
				"computed_at":             snapshot.ComputedAt,
			}
			nrMeta["runtime"] = nrRuntime
			nr.Metadata = encodeJSONMap(nrMeta)
			_ = p.nodeRuns.Upsert(dbc, nr)
		}
	}

	return nil
}

func computePrereqSnapshotID(snapshot map[string]any) string {
	if snapshot == nil {
		return ""
	}
	clone := map[string]any{}
	for k, v := range snapshot {
		if k == "computed_at" {
			continue
		}
		clone[k] = v
	}
	b, _ := json.Marshal(clone)
	h := fnv.New64a()
	_, _ = h.Write(b)
	return fmt.Sprintf("rdy_%x", h.Sum64())
}

type docFrameSignal struct {
	Frame      string  `json:"frame"`
	Confidence float64 `json:"confidence"`
}

func topFrameSignal(scores map[string]float64) (string, float64) {
	if len(scores) == 0 {
		return "", 0
	}
	keys := make([]string, 0, len(scores))
	for k := range scores {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	best := ""
	bestScore := 0.0
	for _, k := range keys {
		score := scores[k]
		if best == "" || score > bestScore || (score == bestScore && k < best) {
			best = k
			bestScore = score
		}
	}
	return best, bestScore
}

func buildFrameBridge(fromFrame string, toFrame string) string {
	fromFrame = strings.TrimSpace(fromFrame)
	toFrame = strings.TrimSpace(toFrame)
	if fromFrame == "" || toFrame == "" || strings.EqualFold(fromFrame, toFrame) {
		return ""
	}
	lines := []string{
		fmt.Sprintf("Try reframing this idea from **%s** to **%s**.", fromFrame, toFrame),
		"",
		"Bridge steps:",
		fmt.Sprintf("- Name the core objects or quantities in the **%s** view.", fromFrame),
		fmt.Sprintf("- Map each object to the closest equivalent in the **%s** view.", toFrame),
		"- Restate the rule using the target frame.",
		"- Check the restatement on a simple example.",
	}
	return strings.Join(lines, "\n")
}

func normalizeKeysPreserveOrder(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(strings.ToLower(k))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}

func normalizeKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(strings.ToLower(k))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func hasTable(db *gorm.DB, model any) bool {
	if db == nil {
		return false
	}
	return db.Migrator().HasTable(model)
}

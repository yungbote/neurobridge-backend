package runtime_update

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

const beliefSnapshotSchemaVersion = 1

func (p *Pipeline) maybeUpsertBeliefSnapshot(
	dbc dbctx.Context,
	userID uuid.UUID,
	pathID uuid.UUID,
	nodeID uuid.UUID,
	sessionID string,
	doc content.NodeDocV1,
	readiness readinessResult,
	prRuntime map[string]any,
	nrRuntime map[string]any,
	now time.Time,
) (string, map[string]any) {
	if !beliefSnapshotEnabled() || p.beliefs == nil {
		return "", nil
	}
	if p.db != nil && !hasTable(p.db, &types.UserBeliefSnapshot{}) {
		return "", nil
	}
	if userID == uuid.Nil || pathID == uuid.Nil || nodeID == uuid.Nil {
		return "", nil
	}
	if p.concepts == nil || p.conStates == nil {
		return "", nil
	}

	keys := normalizeKeysPreserveOrder(collectConceptKeysFromDoc(doc))
	if len(keys) == 0 {
		return "", nil
	}

	if readiness.ConceptKeyByID == nil || len(readiness.ConceptKeyByID) == 0 {
		readiness = computeReadiness(dbc, userID, pathID, doc, p.concepts, p.edges, p.conStates, p.miscons, now)
	}

	conceptIDs := collectConceptIDsFromReadiness(readiness)
	if len(conceptIDs) == 0 {
		return "", nil
	}

	modelByID := map[uuid.UUID]*types.UserConceptModel{}
	if p.conModels != nil {
		if rows, _ := p.conModels.ListByUserAndConceptIDs(dbc, userID, conceptIDs); len(rows) > 0 {
			for _, m := range rows {
				if m != nil && m.CanonicalConceptID != uuid.Nil {
					modelByID[m.CanonicalConceptID] = m
				}
			}
		}
	}

	misconsByID := readiness.MisconceptionsByID
	if len(misconsByID) == 0 && p.miscons != nil {
		misconsByID = map[uuid.UUID][]*types.UserMisconceptionInstance{}
		if rows, _ := p.miscons.ListActiveByUserAndConceptIDs(dbc, userID, conceptIDs); len(rows) > 0 {
			for _, m := range rows {
				if m == nil || m.CanonicalConceptID == uuid.Nil {
					continue
				}
				misconsByID[m.CanonicalConceptID] = append(misconsByID[m.CanonicalConceptID], m)
			}
		}
	}

	snapshot := buildBeliefSnapshot(
		userID,
		pathID,
		nodeID,
		sessionID,
		keys,
		readiness,
		modelByID,
		misconsByID,
		prRuntime,
		nrRuntime,
		now,
	)
	if len(snapshot) == 0 {
		return "", nil
	}

	snapshotID := computeBeliefSnapshotID(snapshot)
	if snapshotID == "" {
		return "", snapshot
	}
	snapshot["snapshot_id"] = snapshotID

	row := &types.UserBeliefSnapshot{
		UserID:        userID,
		PathID:        pathID,
		PathNodeID:    nodeID,
		SnapshotID:    snapshotID,
		PolicyVersion: beliefSnapshotPolicyVersion(),
		SchemaVersion: beliefSnapshotSchemaVersion,
		SessionID:     sessionID,
		SnapshotJSON:  datatypes.JSON(mustJSON(snapshot)),
		CreatedAt:     now.UTC(),
	}
	_ = p.beliefs.Upsert(dbc, row)
	return snapshotID, snapshot
}

func buildBeliefSnapshot(
	userID uuid.UUID,
	pathID uuid.UUID,
	nodeID uuid.UUID,
	sessionID string,
	keys []string,
	readiness readinessResult,
	modelByID map[uuid.UUID]*types.UserConceptModel,
	misconsByID map[uuid.UUID][]*types.UserMisconceptionInstance,
	prRuntime map[string]any,
	nrRuntime map[string]any,
	now time.Time,
) map[string]any {
	if userID == uuid.Nil || pathID == uuid.Nil || nodeID == uuid.Nil {
		return nil
	}
	if len(keys) == 0 {
		return nil
	}

	seenKeys := map[string]bool{}
	concepts := make([]map[string]any, 0, len(keys))
	activeConcepts := []string{}
	uncSum := 0.0
	uncCount := 0.0

	for _, key := range keys {
		k := strings.TrimSpace(strings.ToLower(key))
		if k == "" || seenKeys[k] {
			continue
		}
		seenKeys[k] = true
		cid := conceptIDForKey(readiness, k)
		entry, unc, isActive := beliefConceptEntry(cid, k, readiness, now)
		concepts = append(concepts, entry)
		uncSum += unc
		uncCount += 1
		if isActive {
			activeConcepts = append(activeConcepts, k)
		}
	}

	for _, cid := range collectConceptIDsFromReadiness(readiness) {
		k := strings.TrimSpace(strings.ToLower(readiness.ConceptKeyByID[cid]))
		if k == "" {
			k = strings.ToLower(cid.String())
		}
		if seenKeys[k] {
			continue
		}
		seenKeys[k] = true
		entry, unc, isActive := beliefConceptEntry(cid, k, readiness, now)
		concepts = append(concepts, entry)
		uncSum += unc
		uncCount += 1
		if isActive {
			activeConcepts = append(activeConcepts, k)
		}
	}

	sort.Slice(concepts, func(i, j int) bool {
		a := strings.TrimSpace(stringFromAny(concepts[i]["concept_key"]))
		b := strings.TrimSpace(stringFromAny(concepts[j]["concept_key"]))
		if a == b {
			return stringFromAny(concepts[i]["concept_id"]) < stringFromAny(concepts[j]["concept_id"])
		}
		return a < b
	})

	globalUnc := 0.5
	if uncCount > 0 {
		globalUnc = clamp01(uncSum / uncCount)
	}

	miscons := buildBeliefMisconceptions(misconsByID, readiness)
	frames := buildBeliefFrames(modelByID, readiness)
	fatigue := buildBeliefFatigue(prRuntime, now)
	motivation := buildBeliefMotivation(prRuntime, nrRuntime)
	transfer := buildBeliefTransfer(nrRuntime)
	flow := buildBeliefFlow(nrRuntime)

	uncertainty := map[string]any{
		"global_uncertainty": globalUnc,
		"active_concepts":    uniqueStrings(activeConcepts),
	}

	belief := map[string]any{
		"concepts":       concepts,
		"misconceptions": miscons,
		"frames":         frames,
		"fatigue":        fatigue,
		"motivation":     motivation,
		"transfer":       transfer,
		"uncertainty":    uncertainty,
		"flow":           flow,
	}

	snapshot := map[string]any{
		"schema_version": beliefSnapshotSchemaVersion,
		"policy_version": beliefSnapshotPolicyVersion(),
		"user_id":        userID.String(),
		"path_id":        pathID.String(),
		"path_node_id":   nodeID.String(),
		"computed_at":    now.UTC().Format(time.RFC3339),
		"belief":         belief,
	}
	if strings.TrimSpace(sessionID) != "" {
		snapshot["session_id"] = strings.TrimSpace(sessionID)
	}
	return snapshot
}

func beliefConceptEntry(cid uuid.UUID, key string, readiness readinessResult, now time.Time) (map[string]any, float64, bool) {
	entry := map[string]any{
		"concept_key": key,
	}
	if cid != uuid.Nil {
		entry["concept_id"] = cid.String()
	}
	st := (*types.UserConceptState)(nil)
	if cid != uuid.Nil && readiness.ConceptState != nil {
		st = readiness.ConceptState[cid]
	}
	if st == nil {
		entry["mastery"] = 0.0
		entry["confidence"] = 0.0
		entry["epistemic_uncertainty"] = 1.0
		entry["aleatoric_uncertainty"] = 1.0
		entry["coverage_debt"] = 1.0
		return entry, 1.0, true
	}
	mastery, conf, unc, debt, _ := deriveConceptSignals(st, now)
	entry["mastery"] = clamp01(mastery)
	entry["confidence"] = clamp01(conf)
	entry["epistemic_uncertainty"] = clamp01(st.EpistemicUncertainty)
	entry["aleatoric_uncertainty"] = clamp01(st.AleatoricUncertainty)
	entry["coverage_debt"] = clamp01(debt)
	if st.LastSeenAt != nil && !st.LastSeenAt.IsZero() {
		entry["last_seen_at"] = st.LastSeenAt.UTC().Format(time.RFC3339)
	}
	isActive := mastery < readinessMinMastery() || unc > 0.55
	return entry, unc, isActive
}

func buildBeliefMisconceptions(misconsByID map[uuid.UUID][]*types.UserMisconceptionInstance, readiness readinessResult) []map[string]any {
	out := []map[string]any{}
	if len(misconsByID) == 0 {
		return out
	}
	for cid, rows := range misconsByID {
		if cid == uuid.Nil || len(rows) == 0 {
			continue
		}
		for _, m := range rows {
			if m == nil {
				continue
			}
			key := strings.TrimSpace(m.Description)
			if m.PatternID != nil && strings.TrimSpace(*m.PatternID) != "" {
				key = strings.TrimSpace(*m.PatternID)
			}
			if key == "" {
				continue
			}
			support := types.DecodeMisconceptionSupport(m.Support)
			entry := map[string]any{
				"concept_id":            cid.String(),
				"misconception_key":     key,
				"signature_type":        support.SignatureType,
				"frame_from":            support.FrameFrom,
				"frame_to":              support.FrameTo,
				"confidence":            clamp01(m.Confidence),
				"resolution_confidence": clamp01(support.ResolutionConfidence),
				"status":                strings.TrimSpace(m.Status),
			}
			if m.LastSeenAt != nil && !m.LastSeenAt.IsZero() {
				entry["last_seen_at"] = m.LastSeenAt.UTC().Format(time.RFC3339)
			}
			out = append(out, entry)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a := stringFromAny(out[i]["concept_id"]) + ":" + stringFromAny(out[i]["misconception_key"])
		b := stringFromAny(out[j]["concept_id"]) + ":" + stringFromAny(out[j]["misconception_key"])
		return a < b
	})
	return out
}

func buildBeliefFrames(modelByID map[uuid.UUID]*types.UserConceptModel, readiness readinessResult) map[string]any {
	frameProfile := map[string]float64{}
	if len(modelByID) > 0 {
		for cid, m := range modelByID {
			if m == nil || len(m.ActiveFrames) == 0 {
				continue
			}
			var frames []docFrameSignal
			_ = json.Unmarshal(m.ActiveFrames, &frames)
			for _, f := range frames {
				name := strings.TrimSpace(f.Frame)
				if name == "" {
					continue
				}
				score := clamp01(f.Confidence)
				if prev, ok := frameProfile[name]; !ok || score > prev {
					frameProfile[name] = score
				}
			}
			_ = cid
		}
	}

	preferred := []string{}
	if len(frameProfile) > 0 {
		type pair struct {
			name  string
			score float64
		}
		list := make([]pair, 0, len(frameProfile))
		for k, v := range frameProfile {
			list = append(list, pair{name: k, score: v})
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].score == list[j].score {
				return list[i].name < list[j].name
			}
			return list[i].score > list[j].score
		})
		for i, item := range list {
			if i >= 3 {
				break
			}
			preferred = append(preferred, item.name)
		}
	}

	frameAlign := 0.0
	for _, v := range frameProfile {
		if v > frameAlign {
			frameAlign = v
		}
	}

	return map[string]any{
		"frame_profile":    frameProfile,
		"preferred_frames": preferred,
		"frame_alignment":  clamp01(frameAlign),
	}
}

func buildBeliefFatigue(prRuntime map[string]any, now time.Time) map[string]any {
	fatigueScore := clamp01(floatFromAny(prRuntime["fatigue_score"], 0))
	sessionMinutes := 0.0
	if started := timeFromAny(prRuntime["session_started_at"]); started != nil {
		sessionMinutes = now.Sub(*started).Minutes()
		if sessionMinutes < 0 {
			sessionMinutes = 0
		}
	}
	promptsInWindow := intFromAny(prRuntime["prompts_in_window"], 0)
	return map[string]any{
		"fatigue_score":     fatigueScore,
		"session_minutes":   sessionMinutes,
		"prompts_in_window": promptsInWindow,
	}
}

func buildBeliefMotivation(prRuntime map[string]any, nrRuntime map[string]any) map[string]any {
	blocksSeen := intFromAny(nrRuntime["blocks_seen"], 0)
	readBlocks := stringSliceFromAny(nrRuntime["read_blocks"])
	readDepth := 0.0
	if blocksSeen > 0 {
		readDepth = float64(len(readBlocks)) / float64(blocksSeen)
	} else if len(readBlocks) > 0 {
		readDepth = 1
	}
	readDepth = clamp01(readDepth)
	progressConf := clamp01(floatFromAny(nrRuntime["last_progress_confidence"], 0))
	engagement := 0.5*readDepth + 0.5*progressConf
	if engagement == 0 {
		engagement = 0.5
	}

	_, blocks := banditStore(nrRuntime)
	shown := 0
	dismissed := 0
	for _, raw := range blocks {
		m := mapFromAny(raw)
		if m == nil {
			continue
		}
		shown += intFromAny(m["shown"], 0)
		dismissed += intFromAny(m["dismissed"], 0)
	}
	friction := 0.0
	if shown > 0 {
		friction = float64(dismissed) / float64(shown)
	}
	friction = clamp01(friction)

	return map[string]any{
		"engagement_score":       clamp01(engagement),
		"friction_score":         friction,
		"last_prompt_dismissals": dismissed,
	}
}

func buildBeliefTransfer(nrRuntime map[string]any) map[string]any {
	successes := intFromAny(nrRuntime["transfer_successes"], 0)
	failures := intFromAny(nrRuntime["transfer_failures"], 0)
	total := successes + failures
	reliability := 0.5
	if total > 0 {
		reliability = float64(successes) / float64(total)
	}
	return map[string]any{
		"transfer_reliability": clamp01(reliability),
		"transfer_successes":   successes,
		"transfer_failures":    failures,
	}
}

func buildBeliefFlow(nrRuntime map[string]any) map[string]any {
	return map[string]any{
		"budget_total":     floatFromAny(nrRuntime["flow_budget_total"], 0),
		"budget_remaining": floatFromAny(nrRuntime["flow_budget_remaining"], 0),
		"disruption_spend": floatFromAny(nrRuntime["flow_disruption_spend"], 0),
	}
}

func computeBeliefSnapshotID(snapshot map[string]any) string {
	if snapshot == nil {
		return ""
	}
	clone := map[string]any{}
	for k, v := range snapshot {
		if k == "computed_at" || k == "snapshot_id" {
			continue
		}
		clone[k] = v
	}
	b, _ := json.Marshal(clone)
	h := fnv.New64a()
	_, _ = h.Write(b)
	return fmt.Sprintf("blf_%x", h.Sum64())
}

func collectConceptIDsFromReadiness(r readinessResult) []uuid.UUID {
	ids := []uuid.UUID{}
	seen := map[uuid.UUID]bool{}
	for id := range r.ConceptKeyByID {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	for id := range r.ConceptState {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return ids[i].String() < ids[j].String()
	})
	return ids
}

func conceptIDForKey(r readinessResult, key string) uuid.UUID {
	if r.ConceptByKey == nil {
		return uuid.Nil
	}
	if c := r.ConceptByKey[key]; c != nil {
		cid := c.ID
		if c.CanonicalConceptID != nil && *c.CanonicalConceptID != uuid.Nil {
			cid = *c.CanonicalConceptID
		}
		return cid
	}
	return uuid.Nil
}

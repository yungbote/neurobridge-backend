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
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

const interventionPlanSchemaVersion = 1

type misconEntry struct {
	key       string
	conceptID uuid.UUID
}

func (p *Pipeline) maybeUpsertInterventionPlan(
	dbc dbctx.Context,
	userID uuid.UUID,
	pathID uuid.UUID,
	nodeID uuid.UUID,
	snapshotID string,
	snapshot map[string]any,
	readiness readinessResult,
	prRuntime map[string]any,
	nrRuntime map[string]any,
	now time.Time,
) (string, []map[string]any) {
	if !interventionPlannerEnabled() || p.plans == nil {
		return "", nil
	}
	if p.db != nil && !hasTable(p.db, &types.InterventionPlan{}) {
		return "", nil
	}
	if userID == uuid.Nil || pathID == uuid.Nil || nodeID == uuid.Nil {
		return "", nil
	}
	if strings.TrimSpace(snapshotID) == "" {
		return "", nil
	}
	if snapshot == nil {
		return "", nil
	}

	var causalEdges []*types.MisconceptionCausalEdge
	if p.misconEdges != nil {
		ids := collectMisconceptionConceptIDs(snapshot)
		if len(ids) > 0 {
			if rows, err := p.misconEdges.ListByUserAndConceptIDs(dbc, userID, ids); err == nil {
				causalEdges = rows
			}
		}
	}

	plan := buildInterventionPlan(snapshotID, snapshot, readiness, prRuntime, nrRuntime, causalEdges, now)
	if plan == nil {
		return "", nil
	}
	planID := computeInterventionPlanID(plan)
	if planID == "" {
		return "", nil
	}
	plan["plan_id"] = planID

	actions := []map[string]any{}
	if list, ok := plan["actions"].([]map[string]any); ok {
		actions = list
	} else if raw, ok := plan["actions"].([]any); ok {
		for _, item := range raw {
			if m, ok := item.(map[string]any); ok {
				actions = append(actions, m)
			}
		}
	}

	actions = ensureInterventionActionIDs(planID, actions)
	plan["actions"] = actions

	constraints := map[string]any{}
	if m, ok := plan["constraints"].(map[string]any); ok {
		constraints = m
	}

	row := &types.InterventionPlan{
		UserID:              userID,
		PathID:              pathID,
		PathNodeID:          nodeID,
		PlanID:              planID,
		SnapshotID:          strings.TrimSpace(snapshotID),
		PolicyVersion:       interventionPolicyVersion(),
		SchemaVersion:       interventionPlanSchemaVersion,
		FlowBudgetTotal:     floatFromAny(plan["flow_budget_total"], 0),
		FlowBudgetRemaining: floatFromAny(plan["flow_budget_remaining"], 0),
		ExpectedGain:        floatFromAny(plan["expected_gain"], 0),
		FlowCost:            floatFromAny(plan["flow_cost"], 0),
		ActionsJSON:         datatypes.JSON(mustJSON(actions)),
		ConstraintsJSON:     datatypes.JSON(mustJSON(constraints)),
		PlanJSON:            datatypes.JSON(mustJSON(plan)),
		CreatedAt:           now.UTC(),
	}
	_ = p.plans.Upsert(dbc, row)
	return planID, actions
}

func buildInterventionPlan(
	snapshotID string,
	snapshot map[string]any,
	readiness readinessResult,
	prRuntime map[string]any,
	nrRuntime map[string]any,
	causalEdges []*types.MisconceptionCausalEdge,
	now time.Time,
) map[string]any {
	if strings.TrimSpace(snapshotID) == "" || snapshot == nil {
		return nil
	}
	risk, debt := planRiskDebt(snapshot, readiness)
	flowTotal, flowRemaining := planFlowBudget(nrRuntime)

	actions := planActions(snapshot, readiness, causalEdges, risk, debt, flowRemaining)
	expectedGain := 0.0
	flowCost := 0.0
	for _, a := range actions {
		expectedGain += floatFromAny(a["expected_gain"], 0)
		flowCost += floatFromAny(a["cost"], 0)
	}

	constraints := map[string]any{
		"max_flow_disruption": plannerFlowMaxDisruption(),
		"min_coverage":        plannerMinCoverage(),
		"max_time_minutes":    plannerMaxTimeMinutes(),
	}

	plan := map[string]any{
		"schema_version":        interventionPlanSchemaVersion,
		"policy_version":        interventionPolicyVersion(),
		"snapshot_id":           snapshotID,
		"user_id":               stringFromAny(snapshot["user_id"]),
		"path_id":               stringFromAny(snapshot["path_id"]),
		"path_node_id":          stringFromAny(snapshot["path_node_id"]),
		"created_at":            now.UTC().Format(time.RFC3339),
		"flow_budget_total":     flowTotal,
		"flow_budget_remaining": flowRemaining,
		"expected_gain":         clamp01(expectedGain),
		"flow_cost":             clamp01(flowCost),
		"constraints":           constraints,
		"actions":               actions,
	}
	return plan
}

func planRiskDebt(snapshot map[string]any, readiness readinessResult) (float64, float64) {
	risk := 0.0
	debt := 0.0

	if readiness.Snapshot != nil {
		risk = clamp01(1.0 - readiness.Snapshot.Score)
		debt = clamp01(readiness.Snapshot.CoverageDebtMax)
		if readiness.Snapshot.MinMastery < readinessMinMastery() {
			risk = clamp01(risk + 0.15)
		}
		if readiness.Snapshot.MisconceptionsActive > 0 {
			boost := 0.1 + 0.05*float64(minInt(readiness.Snapshot.MisconceptionsActive, 4))
			risk = clamp01(risk + boost)
		}
		return risk, debt
	}

	belief := mapFromAny(snapshot["belief"])
	concepts := beliefArray(belief, "concepts")
	if len(concepts) == 0 {
		return risk, debt
	}
	sum := 0.0
	maxDebt := 0.0
	count := 0.0
	for _, c := range concepts {
		mastery := clamp01(floatFromAny(c["mastery"], 0))
		sum += mastery
		count += 1
		d := clamp01(floatFromAny(c["coverage_debt"], 0))
		if d > maxDebt {
			maxDebt = d
		}
	}
	if count > 0 {
		avg := sum / count
		risk = clamp01(1.0 - avg)
	}
	debt = clamp01(maxDebt)
	return risk, debt
}

func planFlowBudget(nrRuntime map[string]any) (float64, float64) {
	total := floatFromAny(nrRuntime["flow_budget_total"], 0)
	remaining := floatFromAny(nrRuntime["flow_budget_remaining"], 0)
	if total <= 0 {
		total = plannerFlowBudgetTotal()
	}
	if remaining <= 0 || remaining > total {
		remaining = total
	}
	return total, remaining
}

func planActions(snapshot map[string]any, readiness readinessResult, causalEdges []*types.MisconceptionCausalEdge, risk, debt, flowRemaining float64) []map[string]any {
	actions := []map[string]any{}
	maxActions := plannerMaxActions()
	if maxActions == 0 {
		return actions
	}

	miscons := planMisconceptionKeys(snapshot, causalEdges)
	weak := planWeakConcepts(readiness, snapshot)
	transferReliability := planTransferReliability(snapshot)

	add := func(action map[string]any) {
		if action == nil {
			return
		}
		actions = append(actions, action)
	}

	if risk >= plannerRiskThreshold() {
		add(newPlanAction("review_prereq", "prereq_bridge", 0, "readiness_risk", 0.25, risk, weak, nil))
	}
	if len(miscons) > 0 {
		add(newPlanAction("frame_bridge", "reframe", 1, "misconception_frame", 0.2, 0.6, weak, miscons))
		add(newPlanAction("reinforcement_probe", "none", 2, "misconception_retest", 0.12, 0.5, weak, miscons))
	}
	if debt >= plannerDebtThreshold() {
		add(newPlanAction("micro_bridge", "prereq_bridge", 3, "coverage_debt", 0.15, debt, weak, nil))
	}
	if transferReliability > 0 && transferReliability < 0.6 {
		add(newPlanAction("transfer_check", "transfer_check", 4, "transfer_low", 0.12, 0.4, weak, nil))
	}
	if risk >= 0.7 && flowRemaining < 0.2 {
		add(newPlanAction("escalation", "none", 5, "flow_exhausted", 0.3, 0.2, weak, miscons))
	}

	sort.Slice(actions, func(i, j int) bool {
		pi := intFromAny(actions[i]["priority"], 0)
		pj := intFromAny(actions[j]["priority"], 0)
		if pi == pj {
			return stringFromAny(actions[i]["type"]) < stringFromAny(actions[j]["type"])
		}
		return pi < pj
	})

	limited := []map[string]any{}
	cost := 0.0
	for _, a := range actions {
		if len(limited) >= maxActions {
			break
		}
		nextCost := floatFromAny(a["cost"], 0)
		if cost+nextCost > plannerFlowMaxDisruption() && plannerFlowMaxDisruption() > 0 {
			continue
		}
		limited = append(limited, a)
		cost += nextCost
	}
	return limited
}

func newPlanAction(actionType, slot string, priority int, reason string, cost float64, gain float64, concepts []string, miscons []string) map[string]any {
	out := map[string]any{
		"type":          actionType,
		"priority":      priority,
		"slot":          slot,
		"reason":        reason,
		"cost":          clamp01(cost),
		"expected_gain": clamp01(gain),
	}
	if len(concepts) > 0 {
		out["target_concepts"] = uniqueStrings(concepts)
	}
	if len(miscons) > 0 {
		out["target_misconceptions"] = uniqueStrings(miscons)
	}
	return out
}

func computeInterventionPlanID(plan map[string]any) string {
	if plan == nil {
		return ""
	}
	clone := map[string]any{}
	for k, v := range plan {
		if k == "created_at" || k == "plan_id" {
			continue
		}
		if k == "actions" {
			clone[k] = stripInterventionActionIDs(v)
			continue
		}
		clone[k] = v
	}
	b, _ := json.Marshal(clone)
	h := fnv.New64a()
	_, _ = h.Write(b)
	return fmt.Sprintf("plan_%x", h.Sum64())
}

func stripInterventionActionIDs(raw any) []map[string]any {
	actions := []map[string]any{}
	switch t := raw.(type) {
	case []map[string]any:
		for _, item := range t {
			if item == nil {
				continue
			}
			clone := map[string]any{}
			for k, v := range item {
				if strings.EqualFold(strings.TrimSpace(k), "action_id") {
					continue
				}
				clone[k] = v
			}
			actions = append(actions, clone)
		}
	case []any:
		for _, item := range t {
			m, ok := item.(map[string]any)
			if !ok || m == nil {
				continue
			}
			clone := map[string]any{}
			for k, v := range m {
				if strings.EqualFold(strings.TrimSpace(k), "action_id") {
					continue
				}
				clone[k] = v
			}
			actions = append(actions, clone)
		}
	}
	return actions
}

func ensureInterventionActionIDs(planID string, actions []map[string]any) []map[string]any {
	if planID == "" || len(actions) == 0 {
		return actions
	}
	out := make([]map[string]any, 0, len(actions))
	for _, action := range actions {
		if action == nil {
			continue
		}
		id := strings.TrimSpace(stringFromAny(action["action_id"]))
		if id == "" {
			id = computeInterventionActionID(planID, action)
		}
		action["action_id"] = id
		out = append(out, action)
	}
	return out
}

func computeInterventionActionID(planID string, action map[string]any) string {
	if planID == "" || action == nil {
		return ""
	}
	clone := map[string]any{}
	for k, v := range action {
		if strings.EqualFold(strings.TrimSpace(k), "action_id") {
			continue
		}
		clone[k] = v
	}
	payload := map[string]any{
		"plan_id": planID,
		"action":  clone,
	}
	b, _ := json.Marshal(payload)
	h := fnv.New64a()
	_, _ = h.Write(b)
	return fmt.Sprintf("ipa_%x", h.Sum64())
}

func planMisconceptionKeys(snapshot map[string]any, causalEdges []*types.MisconceptionCausalEdge) []string {
	belief := mapFromAny(snapshot["belief"])
	miscons := beliefArray(belief, "misconceptions")
	if len(miscons) == 0 {
		return nil
	}
	entries := []misconEntry{}
	for _, m := range miscons {
		key := strings.TrimSpace(stringFromAny(m["misconception_key"]))
		if key == "" {
			continue
		}
		cid := uuid.Nil
		if raw := strings.TrimSpace(stringFromAny(m["concept_id"])); raw != "" {
			if parsed, err := uuid.Parse(raw); err == nil {
				cid = parsed
			}
		}
		entries = append(entries, misconEntry{key: key, conceptID: cid})
	}
	if len(entries) == 0 {
		return nil
	}
	filtered := filterMisconceptionEntriesByCausalGraph(entries, causalEdges)
	out := []string{}
	for _, e := range filtered {
		if e.key != "" {
			out = append(out, e.key)
		}
	}
	return uniqueStrings(out)
}

func filterMisconceptionEntriesByCausalGraph(entries []misconEntry, edges []*types.MisconceptionCausalEdge) []misconEntry {
	if len(entries) == 0 || len(edges) == 0 {
		return entries
	}
	conceptSet := map[uuid.UUID]bool{}
	for _, e := range entries {
		if e.conceptID != uuid.Nil {
			conceptSet[e.conceptID] = true
		}
	}
	if len(conceptSet) == 0 {
		return entries
	}
	downstream := map[uuid.UUID]bool{}
	for _, edge := range edges {
		if edge == nil || edge.FromConceptID == uuid.Nil || edge.ToConceptID == uuid.Nil {
			continue
		}
		if edge.Strength < 0.2 {
			continue
		}
		if conceptSet[edge.FromConceptID] && conceptSet[edge.ToConceptID] {
			downstream[edge.ToConceptID] = true
		}
	}
	if len(downstream) == 0 {
		return entries
	}
	out := []misconEntry{}
	for _, e := range entries {
		if e.conceptID != uuid.Nil && downstream[e.conceptID] {
			continue
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return entries
	}
	return out
}

func collectMisconceptionConceptIDs(snapshot map[string]any) []uuid.UUID {
	belief := mapFromAny(snapshot["belief"])
	miscons := beliefArray(belief, "misconceptions")
	out := []uuid.UUID{}
	seen := map[uuid.UUID]bool{}
	for _, m := range miscons {
		raw := strings.TrimSpace(stringFromAny(m["concept_id"]))
		if raw == "" {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func planWeakConcepts(readiness readinessResult, snapshot map[string]any) []string {
	out := []string{}
	if readiness.Snapshot != nil {
		out = append(out, readiness.Snapshot.WeakConcepts...)
		out = append(out, readiness.Snapshot.MisconceptionConcepts...)
		out = append(out, readiness.Snapshot.UncertainConcepts...)
		return uniqueStrings(out)
	}
	belief := mapFromAny(snapshot["belief"])
	concepts := beliefArray(belief, "concepts")
	for _, c := range concepts {
		mastery := clamp01(floatFromAny(c["mastery"], 0))
		conf := clamp01(floatFromAny(c["confidence"], 0))
		if mastery < readinessMinMastery() || conf < 0.5 {
			key := strings.TrimSpace(stringFromAny(c["concept_key"]))
			if key != "" {
				out = append(out, key)
			}
		}
	}
	return uniqueStrings(out)
}

func planTransferReliability(snapshot map[string]any) float64 {
	belief := mapFromAny(snapshot["belief"])
	transfer := mapFromAny(belief["transfer"])
	return clamp01(floatFromAny(transfer["transfer_reliability"], 0))
}

func beliefArray(belief map[string]any, key string) []map[string]any {
	out := []map[string]any{}
	if belief == nil {
		return out
	}
	switch raw := belief[key].(type) {
	case []map[string]any:
		return raw
	case []any:
		for _, item := range raw {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

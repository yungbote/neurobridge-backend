package runtime_update

import (
	"strings"
)

type interventionPlanActionLink struct {
	PlanID         string
	ActionID       string
	ActionType     string
	ActionSlot     string
	ActionReason   string
	ActionPriority int
}

func matchInterventionPlanAction(planID string, actions []map[string]any, promptReason string) *interventionPlanActionLink {
	if strings.TrimSpace(planID) == "" {
		return nil
	}
	link := &interventionPlanActionLink{PlanID: strings.TrimSpace(planID)}
	if len(actions) == 0 {
		return link
	}
	reason := strings.ToLower(strings.TrimSpace(promptReason))
	if reason == "" {
		return link
	}
	bestScore := 0
	bestPriority := 1 << 30
	var best map[string]any
	for _, action := range actions {
		if action == nil {
			continue
		}
		aType := strings.ToLower(strings.TrimSpace(stringFromAny(action["type"])))
		aSlot := strings.ToLower(strings.TrimSpace(stringFromAny(action["slot"])))
		aReason := strings.ToLower(strings.TrimSpace(stringFromAny(action["reason"])))
		score := actionMatchScore(reason, aType, aSlot, aReason)
		if score == 0 {
			continue
		}
		priority := intFromAny(action["priority"], 0)
		if score > bestScore || (score == bestScore && priority < bestPriority) {
			bestScore = score
			bestPriority = priority
			best = action
		}
	}
	if best == nil {
		return link
	}
	link.ActionID = strings.TrimSpace(stringFromAny(best["action_id"]))
	link.ActionType = strings.TrimSpace(stringFromAny(best["type"]))
	link.ActionSlot = strings.TrimSpace(stringFromAny(best["slot"]))
	link.ActionReason = strings.TrimSpace(stringFromAny(best["reason"]))
	link.ActionPriority = intFromAny(best["priority"], 0)
	return link
}

func actionMatchScore(reason, actionType, actionSlot, actionReason string) int {
	score := 0
	if reason != "" {
		if reason == actionSlot || reason == actionType {
			score = 4
		}
	}
	switch reason {
	case "prereq_bridge":
		if actionSlot == "prereq_bridge" {
			score = maxInt(score, 3)
		}
		if actionType == "review_prereq" || actionType == "micro_bridge" {
			score = maxInt(score, 2)
		}
	case "prereq_retest":
		if actionType == "reinforcement_probe" || actionReason == "misconception_retest" {
			score = maxInt(score, 2)
		}
	case "counterfactual_probe":
		if actionType == "reinforcement_probe" {
			score = maxInt(score, 1)
		}
	case "readiness_not_ready":
		if actionType == "review_prereq" {
			score = maxInt(score, 1)
		}
	case "transfer_check":
		if actionType == "transfer_check" || actionSlot == "transfer_check" {
			score = maxInt(score, 2)
		}
	case "reframe":
		if actionType == "frame_bridge" || actionSlot == "reframe" {
			score = maxInt(score, 2)
		}
	case "escalation":
		if actionType == "escalation" {
			score = maxInt(score, 1)
		}
	}
	return score
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

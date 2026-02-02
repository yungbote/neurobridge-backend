package prompts

func runtimePlanBreakPolicySchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"after_minutes":     IntSchema(),
			"min_break_minutes": IntSchema(),
			"max_break_minutes": IntSchema(),
		},
		"required":             []string{"after_minutes", "min_break_minutes", "max_break_minutes"},
		"additionalProperties": false,
	}
}

func runtimePlanQuickCheckPolicySchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"after_blocks":   IntSchema(),
			"after_minutes":  IntSchema(),
			"max_per_lesson": IntSchema(),
			"min_gap_blocks": IntSchema(),
		},
		"required":             []string{"after_blocks", "after_minutes", "max_per_lesson", "min_gap_blocks"},
		"additionalProperties": false,
	}
}

func runtimePlanFlashcardPolicySchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"after_blocks":      IntSchema(),
			"after_minutes":     IntSchema(),
			"after_fail_streak": IntSchema(),
			"max_per_lesson":    IntSchema(),
		},
		"required":             []string{"after_blocks", "after_minutes", "after_fail_streak", "max_per_lesson"},
		"additionalProperties": false,
	}
}

func runtimePlanWeightsSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"mastery":   NumberSchema(),
			"retention": NumberSchema(),
			"pace":      NumberSchema(),
			"fatigue":   NumberSchema(),
		},
		"required":             []string{"mastery", "retention", "pace", "fatigue"},
		"additionalProperties": false,
	}
}

func runtimePlanMultipliersSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"break":       NumberSchema(),
			"quick_check": NumberSchema(),
			"flashcard":   NumberSchema(),
		},
		"required":             []string{"break", "quick_check", "flashcard"},
		"additionalProperties": false,
	}
}

func runtimePlanPolicySchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target_session_minutes": IntSchema(),
			"max_prompts_per_hour":   IntSchema(),
			"break_policy":           runtimePlanBreakPolicySchema(),
			"quick_check_policy":     runtimePlanQuickCheckPolicySchema(),
			"flashcard_policy":       runtimePlanFlashcardPolicySchema(),
			"policy_profile":         EnumSchema("balanced", "gentle", "intensive", "review"),
			"objective_weights":      runtimePlanWeightsSchema(),
			"cadence_multipliers":    runtimePlanMultipliersSchema(),
		},
		"required":             []string{"target_session_minutes", "max_prompts_per_hour", "break_policy", "quick_check_policy", "flashcard_policy", "policy_profile", "objective_weights", "cadence_multipliers"},
		"additionalProperties": false,
	}
}

func RuntimePlanSchema() map[string]any {
	module := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"module_index":           IntSchema(),
			"target_session_minutes": IntSchema(),
			"break_policy":           runtimePlanBreakPolicySchema(),
			"quick_check_policy":     runtimePlanQuickCheckPolicySchema(),
			"flashcard_policy":       runtimePlanFlashcardPolicySchema(),
			"policy_profile":         EnumSchema("balanced", "gentle", "intensive", "review"),
		},
		"required":             []string{"module_index", "target_session_minutes", "break_policy", "quick_check_policy", "flashcard_policy", "policy_profile"},
		"additionalProperties": false,
	}
	lesson := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"node_id":            map[string]any{"type": "string"},
			"node_index":         IntSchema(),
			"lesson_index":       IntSchema(),
			"estimated_minutes":  IntSchema(),
			"break_policy":       runtimePlanBreakPolicySchema(),
			"quick_check_policy": runtimePlanQuickCheckPolicySchema(),
			"flashcard_policy":   runtimePlanFlashcardPolicySchema(),
			"policy_profile":     EnumSchema("balanced", "gentle", "intensive", "review"),
		},
		"required":             []string{"node_id", "node_index", "lesson_index", "estimated_minutes", "break_policy", "quick_check_policy", "flashcard_policy", "policy_profile"},
		"additionalProperties": false,
	}
	return SchemaVersionedObject(1, map[string]any{
		"path":    runtimePlanPolicySchema(),
		"modules": map[string]any{"type": "array", "items": module},
		"lessons": map[string]any{"type": "array", "items": lesson},
	}, []string{"path", "modules", "lessons"})
}

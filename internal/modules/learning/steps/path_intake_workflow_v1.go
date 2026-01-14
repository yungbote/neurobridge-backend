package steps

import "strings"

// workflowV1 is a small, machine-readable payload embedded into chat message metadata.
// It enables deterministic UX (quick replies) and backend routing without relying on LLM tool calls.
type workflowV1 struct {
	Version  int                `json:"version"`
	Kind     string             `json:"kind"`
	Step     string             `json:"step,omitempty"`
	Blocking bool               `json:"blocking"`
	Actions  []workflowV1Action `json:"actions"`
}

type workflowV1Action struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Token   string `json:"token"`
	Variant string `json:"variant,omitempty"` // "primary" | "secondary" | "subtle"
}

func buildIntakeWorkflowV1(intake map[string]any, blocking bool) *workflowV1 {
	if intake == nil {
		return nil
	}

	ma := mapFromAny(intake["material_alignment"])
	mode := strings.ToLower(strings.TrimSpace(stringFromAny(ma["mode"])))
	tracks := sliceAny(intake["tracks"])
	if mode != "multi_goal" && len(tracks) <= 1 {
		return nil
	}

	ps := mapFromAny(intake["path_structure"])
	recommended := strings.ToLower(strings.TrimSpace(stringFromAny(ps["recommended_mode"])))
	if recommended == "" || recommended == "unspecified" {
		// When the model didn't populate it, fall back to a safe default.
		recommended = "program_with_subpaths"
	}

	if hardSeparateOutliers(intake) {
		return &workflowV1{
			Version:  1,
			Kind:     "path_intake",
			Step:     "confirm_separate_paths",
			Blocking: blocking,
			Actions: []workflowV1Action{
				{ID: "confirm_separate_paths", Label: "Confirm split", Token: "confirm", Variant: "primary"},
				{ID: "keep_together", Label: "Keep together", Token: "keep together", Variant: "secondary"},
			},
		}
	}

	variant1 := "secondary"
	variant2 := "secondary"
	switch recommended {
	case "single_path":
		variant1 = "primary"
	case "program_with_subpaths":
		variant2 = "primary"
	}

	actions := []workflowV1Action{
		{ID: "structure_single_path", Label: "1 · One path", Token: "1", Variant: variant1},
		{ID: "structure_program_with_subpaths", Label: "2 · Split tracks", Token: "2", Variant: variant2},
	}
	if blocking {
		actions = append(actions, workflowV1Action{
			ID:      "make_reasonable_assumptions",
			Label:   "Assume & proceed",
			Token:   "Make reasonable assumptions",
			Variant: "subtle",
		})
	}

	return &workflowV1{
		Version:  1,
		Kind:     "path_intake",
		Step:     "choose_structure",
		Blocking: blocking,
		Actions:  actions,
	}
}

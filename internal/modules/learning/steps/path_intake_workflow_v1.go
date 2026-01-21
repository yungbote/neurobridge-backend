package steps



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

	paths := sliceAny(intake["paths"])
	if len(paths) == 0 {
		return nil
	}

	actions := []workflowV1Action{
		{ID: "confirm_paths", Label: "Confirm paths", Token: "confirm", Variant: "primary"},
		{ID: "change_paths", Label: "Change grouping", Token: "change grouping", Variant: "secondary"},
	}

	return &workflowV1{
		Version:  1,
		Kind:     "path_intake",
		Step:     "confirm_paths",
		Blocking: blocking,
		Actions:  actions,
	}
}

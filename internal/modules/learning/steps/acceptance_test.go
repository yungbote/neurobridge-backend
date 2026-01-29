package steps

import "testing"

func findAcceptanceCheck(checks []AcceptanceCheck, id string) *AcceptanceCheck {
	for i := range checks {
		if checks[i].ID == id {
			return &checks[i]
		}
	}
	return nil
}

func TestEvaluateAcceptanceLargeSetPasses(t *testing.T) {
	metrics := AcceptanceMetrics{
		PageCount:         500,
		FileCount:         12,
		ConceptCount:      120,
		NodeCount:         30,
		UncoveredConcepts: 5,
	}
	res := EvaluateAcceptance(metrics, DefaultAcceptanceThresholds())
	if !res.Passed {
		t.Fatalf("expected acceptance to pass, got warnings=%v", res.Warnings)
	}
	if chk := findAcceptanceCheck(res.Checks, "large_set_concepts"); chk == nil || !chk.Passed {
		t.Fatalf("expected large_set_concepts to pass, got %+v", chk)
	}
	if chk := findAcceptanceCheck(res.Checks, "nodes_scale_with_concepts"); chk == nil || !chk.Passed {
		t.Fatalf("expected nodes_scale_with_concepts to pass, got %+v", chk)
	}
}

func TestEvaluateAcceptanceLargeSetFailsWhenTooFewConcepts(t *testing.T) {
	metrics := AcceptanceMetrics{
		PageCount:         400,
		FileCount:         8,
		ConceptCount:      12,
		NodeCount:         3,
		UncoveredConcepts: 6,
		PromptSizeErrors:  0,
	}
	res := EvaluateAcceptance(metrics, DefaultAcceptanceThresholds())
	if res.Passed {
		t.Fatalf("expected acceptance to fail, got pass")
	}
	if chk := findAcceptanceCheck(res.Checks, "large_set_concepts"); chk == nil || chk.Passed {
		t.Fatalf("expected large_set_concepts to fail, got %+v", chk)
	}
}

func TestEvaluateAcceptanceSmallSetPasses(t *testing.T) {
	metrics := AcceptanceMetrics{
		PageCount:    12,
		FileCount:    1,
		ConceptCount: 6,
		NodeCount:    2,
		UnitCount:    1,
		LessonCount:  1,
	}
	res := EvaluateAcceptance(metrics, DefaultAcceptanceThresholds())
	if !res.Passed {
		t.Fatalf("expected small set acceptance to pass, got warnings=%v", res.Warnings)
	}
	if chk := findAcceptanceCheck(res.Checks, "small_set_node_counts"); chk == nil || !chk.Passed {
		t.Fatalf("expected small_set_node_counts to pass, got %+v", chk)
	}
}

func TestEvaluateAcceptancePromptSizeFailure(t *testing.T) {
	metrics := AcceptanceMetrics{
		PageCount:         30,
		ConceptCount:      10,
		NodeCount:         3,
		UncoveredConcepts: 1,
		PromptSizeErrors:  2,
	}
	res := EvaluateAcceptance(metrics, DefaultAcceptanceThresholds())
	if res.Passed {
		t.Fatalf("expected acceptance to fail due to prompt size errors")
	}
	chk := findAcceptanceCheck(res.Checks, "prompt_size_failures")
	if chk == nil || chk.Passed {
		t.Fatalf("expected prompt_size_failures to fail, got %+v", chk)
	}
}

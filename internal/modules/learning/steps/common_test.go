package steps

import (
	"testing"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func TestFallbackConceptKeysForNode_PrefersLexicalMatches(t *testing.T) {
	concepts := []*types.Concept{
		{Key: "svm", Name: "Support Vector Machines", SortIndex: 100},
		{Key: "gradient_descent", Name: "Gradient Descent", SortIndex: 1},
		{Key: "backpropagation", Name: "Backpropagation", SortIndex: 50},
	}

	got := fallbackConceptKeysForNode("Intro to Gradient Descent", "", concepts, 8)
	if len(got) != 1 || got[0] != "gradient_descent" {
		t.Fatalf("unexpected keys: %#v", got)
	}
}

func TestFallbackConceptKeysForNode_FallsBackToImportance(t *testing.T) {
	concepts := []*types.Concept{
		{Key: "a", Name: "Alpha", SortIndex: 10},
		{Key: "b", Name: "Beta", SortIndex: 30},
		{Key: "c", Name: "Gamma", SortIndex: 20},
	}

	got := fallbackConceptKeysForNode("Unrelated Title", "no overlap here", concepts, 2)
	if len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Fatalf("unexpected keys: %#v", got)
	}
}

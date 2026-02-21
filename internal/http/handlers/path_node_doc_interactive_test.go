package handlers

import (
	"testing"

	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
)

func TestEnsureNodeDocInteractiveFallback_AddsMissingInteractiveBlocks(t *testing.T) {
	doc := content.NodeDocV1{
		SchemaVersion:    1,
		Title:            "HTTP Basics",
		Summary:          "Overview",
		ConceptKeys:      []string{"browser", "server", "http"},
		EstimatedMinutes: 5,
		Blocks: []map[string]any{
			{
				"id":   "teach_1",
				"type": "paragraph",
				"md":   "A browser sends a request and the server responds.",
				"citations": []any{
					map[string]any{"chunk_id": "11111111-1111-4111-8111-111111111111", "quote": ""},
				},
			},
		},
	}

	patched, changed := ensureNodeDocInteractiveFallback(doc)
	if !changed {
		t.Fatalf("expected fallback to change doc")
	}
	qc, fc := countInteractiveBlocks(patched)
	if qc < 1 {
		t.Fatalf("expected at least one quick_check, got %d", qc)
	}
	if fc < 1 {
		t.Fatalf("expected at least one flashcard, got %d", fc)
	}
}

func TestEnsureNodeDocInteractiveFallback_NoChangeWhenInteractiveExists(t *testing.T) {
	doc := content.NodeDocV1{
		SchemaVersion:    1,
		Title:            "Existing interactive",
		Summary:          "Overview",
		ConceptKeys:      []string{"http"},
		EstimatedMinutes: 5,
		Blocks: []map[string]any{
			{"id": "p1", "type": "paragraph", "md": "Teaching paragraph"},
			{
				"id":        "qc_1",
				"type":      "quick_check",
				"kind":      "short_answer",
				"prompt_md": "Prompt",
				"options":   []any{},
				"answer_id": "",
				"answer_md": "Answer",
			},
			{"id": "fc_1", "type": "flashcard", "front_md": "Front", "back_md": "Back"},
		},
	}

	patched, changed := ensureNodeDocInteractiveFallback(doc)
	if changed {
		t.Fatalf("expected no fallback changes when interactive blocks already exist")
	}
	qc, fc := countInteractiveBlocks(patched)
	if qc != 1 || fc != 1 {
		t.Fatalf("expected existing counts to remain unchanged (qc=%d fc=%d)", qc, fc)
	}
}

func countInteractiveBlocks(doc content.NodeDocV1) (int, int) {
	qc := 0
	fc := 0
	for _, b := range doc.Blocks {
		switch stringFromAny(b["type"]) {
		case "quick_check":
			qc++
		case "flashcard":
			fc++
		}
	}
	return qc, fc
}

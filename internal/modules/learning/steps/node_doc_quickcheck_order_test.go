package steps

import (
	"testing"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
)

func TestEnsureQuickChecksAfterTeaching_ReordersAfterTeaching(t *testing.T) {
	introID := uuid.New()
	teachID := uuid.New()
	allowed := map[string]bool{
		introID.String(): true,
		teachID.String(): true,
	}
	chunkByID := map[uuid.UUID]*types.MaterialChunk{
		introID: {ID: introID, Text: "Intro chunk."},
		teachID: {ID: teachID, Text: "Teaching chunk."},
	}

	doc := content.NodeDocV1{
		SchemaVersion: 1,
		Title:         "Lesson",
		ConceptKeys:   []string{"k"},
		Blocks: []map[string]any{
			{
				"type": "paragraph",
				"md":   "Intro paragraph.",
				"citations": []any{
					map[string]any{"chunk_id": introID.String(), "quote": "x", "loc": map[string]any{"page": 0, "start": 0, "end": 0}},
				},
			},
			{
				"type":      "quick_check",
				"kind":      "short_answer",
				"options":   []any{},
				"answer_id": "",
				"prompt_md": "What does the material say?",
				"answer_md": "Reference answer.",
				"citations": []any{
					map[string]any{"chunk_id": teachID.String(), "quote": "y", "loc": map[string]any{"page": 0, "start": 0, "end": 0}},
				},
			},
			{
				"type": "paragraph",
				"md":   "Teaching paragraph.",
				"citations": []any{
					map[string]any{"chunk_id": teachID.String(), "quote": "z", "loc": map[string]any{"page": 0, "start": 0, "end": 0}},
				},
			},
		},
	}

	doc, _, _ = sanitizeNodeDocCitations(doc, allowed, chunkByID, []uuid.UUID{introID, teachID})
	patched, _, changed := ensureQuickChecksAfterTeaching(doc, chunkByID)
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if errs, _ := content.ValidateNodeDocV1(patched, allowed, content.NodeDocRequirements{}); len(errs) > 0 {
		t.Fatalf("expected no validation errors, got: %v", errs)
	}

	qcIndex := -1
	teachingIndex := -1
	for i, b := range patched.Blocks {
		if b == nil {
			continue
		}
		if stringFromAny(b["type"]) == "quick_check" {
			qcIndex = i
		}
		if stringFromAny(b["type"]) == "paragraph" && stringFromAny(b["md"]) == "Teaching paragraph." {
			teachingIndex = i
		}
	}
	if teachingIndex < 0 || qcIndex < 0 {
		t.Fatalf("expected both teaching paragraph and quick_check present (teaching=%d qc=%d)", teachingIndex, qcIndex)
	}
	if qcIndex <= teachingIndex {
		t.Fatalf("expected quick_check after teaching paragraph (teaching=%d qc=%d)", teachingIndex, qcIndex)
	}
}

func TestEnsureQuickChecksAfterTeaching_InsertsContextWhenUntaught(t *testing.T) {
	introID := uuid.New()
	untaughtID := uuid.New()
	allowed := map[string]bool{
		introID.String():    true,
		untaughtID.String(): true,
	}
	chunkByID := map[uuid.UUID]*types.MaterialChunk{
		introID:    {ID: introID, Text: "Intro chunk."},
		untaughtID: {ID: untaughtID, Text: "Some grounding excerpt text."},
	}

	doc := content.NodeDocV1{
		SchemaVersion: 1,
		Title:         "Lesson",
		ConceptKeys:   []string{"k"},
		Blocks: []map[string]any{
			{
				"type": "paragraph",
				"md":   "Intro paragraph.",
				"citations": []any{
					map[string]any{"chunk_id": introID.String(), "quote": "x", "loc": map[string]any{"page": 0, "start": 0, "end": 0}},
				},
			},
			{
				"type":      "quick_check",
				"kind":      "short_answer",
				"options":   []any{},
				"answer_id": "",
				"prompt_md": "Question?",
				"answer_md": "Answer.",
				"citations": []any{
					map[string]any{"chunk_id": untaughtID.String(), "quote": "y", "loc": map[string]any{"page": 0, "start": 0, "end": 0}},
				},
			},
		},
	}

	doc, _, _ = sanitizeNodeDocCitations(doc, allowed, chunkByID, []uuid.UUID{introID, untaughtID})
	patched, stats, changed := ensureQuickChecksAfterTeaching(doc, chunkByID)
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if stats.ContextParagraphsInserted != 1 {
		t.Fatalf("expected ContextParagraphsInserted=1 got %d", stats.ContextParagraphsInserted)
	}
	if len(patched.Blocks) != 3 {
		t.Fatalf("expected 3 blocks after insertion, got %d", len(patched.Blocks))
	}
	if stringFromAny(patched.Blocks[1]["type"]) != "paragraph" {
		t.Fatalf("expected inserted paragraph at index 1, got %q", stringFromAny(patched.Blocks[1]["type"]))
	}
	if stringFromAny(patched.Blocks[2]["type"]) != "quick_check" {
		t.Fatalf("expected quick_check at index 2, got %q", stringFromAny(patched.Blocks[2]["type"]))
	}
	if got := extractChunkIDsFromCitations(patched.Blocks[1]["citations"]); len(got) != 1 || got[0] != untaughtID.String() {
		t.Fatalf("expected inserted paragraph to cite %s got %v", untaughtID.String(), got)
	}
	if errs, _ := content.ValidateNodeDocV1(patched, allowed, content.NodeDocRequirements{}); len(errs) > 0 {
		t.Fatalf("expected no validation errors, got: %v", errs)
	}
}

func TestInjectMissingMustCiteCitations_SkipsQuickCheck(t *testing.T) {
	missingID := uuid.New()
	otherID := uuid.New()
	chunkByID := map[uuid.UUID]*types.MaterialChunk{
		missingID: {ID: missingID, Text: "Chunk text."},
	}

	doc := content.NodeDocV1{
		SchemaVersion: 1,
		Title:         "Lesson",
		ConceptKeys:   []string{"k"},
		Blocks: []map[string]any{
			{
				"type":      "quick_check",
				"prompt_md": "Q?",
				"answer_md": "A.",
				"citations": []any{
					map[string]any{"chunk_id": otherID.String(), "quote": "x", "loc": map[string]any{"page": 0, "start": 0, "end": 0}},
				},
			},
			{
				"type": "paragraph",
				"md":   "Body.",
				"citations": []any{
					map[string]any{"chunk_id": otherID.String(), "quote": "y", "loc": map[string]any{"page": 0, "start": 0, "end": 0}},
				},
			},
		},
	}

	patched, ok := injectMissingMustCiteCitations(doc, []string{missingID.String()}, chunkByID)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	for _, id := range extractChunkIDsFromCitations(patched.Blocks[0]["citations"]) {
		if id == missingID.String() {
			t.Fatalf("expected quick_check citations to remain unchanged, got %v", extractChunkIDsFromCitations(patched.Blocks[0]["citations"]))
		}
	}
	gotPara := extractChunkIDsFromCitations(patched.Blocks[1]["citations"])
	found := false
	for _, id := range gotPara {
		if id == missingID.String() {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected paragraph to have injected citation %s, got %v", missingID.String(), gotPara)
	}
}

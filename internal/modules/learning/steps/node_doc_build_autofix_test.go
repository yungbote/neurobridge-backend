package steps

import (
	"testing"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
)

func TestEnsureNodeDocMeetsMinima_PadsParagraphsToMeetRequirements(t *testing.T) {
	cid := uuid.New()
	allowed := map[string]bool{cid.String(): true}
	chunkByID := map[uuid.UUID]*types.MaterialChunk{
		cid: {ID: cid, Text: "Grounding excerpt text."},
	}

	doc := content.NodeDocV1{
		SchemaVersion:    1,
		Title:            "Test Doc",
		Summary:          "Summary",
		ConceptKeys:      []string{"test_concept"},
		EstimatedMinutes: 5,
		Blocks:           make([]map[string]any, 0, 8),
	}
	for i := 0; i < 7; i++ {
		doc.Blocks = append(doc.Blocks, map[string]any{
			"type": "paragraph",
			"md":   "Short paragraph.",
		})
	}

	req := content.NodeDocRequirements{MinParagraphs: 8}

	// Before padding, validation should fail the paragraph minimum (after citation sanitization).
	doc0, _, _ := sanitizeNodeDocCitations(doc, allowed, chunkByID, []uuid.UUID{cid})
	if errs, _ := content.ValidateNodeDocV1(doc0, allowed, req); len(errs) == 0 {
		t.Fatalf("expected validation errors before padding, got none")
	}

	// After padding, the doc should pass validation.
	doc1, _ := ensureNodeDocMeetsMinima(doc, req, allowed, chunkByID, []uuid.UUID{cid})
	doc1, _, _ = sanitizeNodeDocCitations(doc1, allowed, chunkByID, []uuid.UUID{cid})
	if errs, _ := content.ValidateNodeDocV1(doc1, allowed, req); len(errs) > 0 {
		t.Fatalf("expected no validation errors after padding, got: %v", errs)
	}

	metrics := content.NodeDocMetrics(doc1)
	bc, _ := metrics["block_counts"].(map[string]int)
	if bc["paragraph"] < 8 {
		t.Fatalf("expected paragraphs>=8 got %d", bc["paragraph"])
	}
}

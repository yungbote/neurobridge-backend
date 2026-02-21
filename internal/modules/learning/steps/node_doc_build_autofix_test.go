package steps

import (
	"testing"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/docgen"
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

func TestEnsureNodeDocBlueprintObjectives_InsertsObjectivesBlockWhenMissing(t *testing.T) {
	doc := content.NodeDocV1{
		SchemaVersion:    1,
		Title:            "HTTP Basics",
		Summary:          "Overview",
		ConceptKeys:      []string{"http"},
		EstimatedMinutes: 6,
		Blocks: []map[string]any{
			{"type": "paragraph", "md": "A browser sends requests and a server responds."},
		},
	}
	blueprint := docgen.DocBlueprintV1{
		SchemaVersion:    docgen.DocBlueprintSchemaVersion,
		BlueprintVersion: "doc_blueprint_v1.0.0",
		PathID:           uuid.New().String(),
		PathNodeID:       uuid.New().String(),
		Objectives: []string{
			"Define browser, server, and HTTP at a high level, and describe the basic request/response exchange without deep protocol details.",
		},
	}

	before := docgen.ValidateDocAgainstBlueprint(doc, blueprint)
	if before.Passed {
		t.Fatalf("expected missing_objective before autofix")
	}

	patched, added, changed := ensureNodeDocBlueprintObjectives(doc, blueprint)
	if !changed {
		t.Fatalf("expected objectives autofix to change doc")
	}
	if len(added) != 1 {
		t.Fatalf("expected one objective inserted, got %d (%v)", len(added), added)
	}

	after := docgen.ValidateDocAgainstBlueprint(patched, blueprint)
	for _, v := range after.Violations {
		if v.Code == "missing_objective" {
			t.Fatalf("expected no missing_objective after autofix, got violations: %+v", after.Violations)
		}
	}
}

func TestEnsureNodeDocBlueprintObjectives_AppendsToExistingObjectivesBlock(t *testing.T) {
	alreadyCovered := "Explain what a browser does."
	missing := "Explain what a server does."
	doc := content.NodeDocV1{
		SchemaVersion:    1,
		Title:            "Web Components",
		Summary:          "Overview",
		ConceptKeys:      []string{"browser", "server"},
		EstimatedMinutes: 5,
		Blocks: []map[string]any{
			{
				"type":     "objectives",
				"title":    "Objectives",
				"items_md": []any{alreadyCovered},
			},
			{"type": "paragraph", "md": alreadyCovered},
		},
	}
	blueprint := docgen.DocBlueprintV1{
		SchemaVersion:    docgen.DocBlueprintSchemaVersion,
		BlueprintVersion: "doc_blueprint_v1.0.0",
		PathID:           uuid.New().String(),
		PathNodeID:       uuid.New().String(),
		Objectives:       []string{alreadyCovered, missing},
	}

	patched, added, changed := ensureNodeDocBlueprintObjectives(doc, blueprint)
	if !changed {
		t.Fatalf("expected objective appended to existing objectives block")
	}
	if len(added) != 1 || added[0] != missing {
		t.Fatalf("expected appended objective %q, got %v", missing, added)
	}
	objBlock, ok := patched.Blocks[0]["items_md"].([]any)
	if !ok {
		t.Fatalf("expected objectives.items_md as []any")
	}
	if len(objBlock) != 2 {
		t.Fatalf("expected 2 objectives after append, got %d (%v)", len(objBlock), objBlock)
	}
}

func TestEnsureNodeDocInteractiveMinima_AddsQuickChecksAndFlashcards(t *testing.T) {
	cid := uuid.New()
	allowed := map[string]bool{cid.String(): true}
	chunkByID := map[uuid.UUID]*types.MaterialChunk{
		cid: {ID: cid, Text: "Grounding excerpt text."},
	}

	doc := content.NodeDocV1{
		SchemaVersion:    1,
		Title:            "HTTP Basics",
		Summary:          "Overview",
		ConceptKeys:      []string{"browser", "server", "http"},
		EstimatedMinutes: 5,
		Blocks: []map[string]any{
			{
				"id":        "teach_1",
				"type":      "paragraph",
				"md":        "A browser sends an HTTP request to a server and receives a response.",
				"citations": []any{map[string]any{"chunk_id": cid.String(), "quote": "", "loc": map[string]any{"page": 0, "start": 0, "end": 0}}},
			},
		},
	}
	req := content.NodeDocRequirements{
		MinQuickChecks: 2,
		MinFlashcards:  1,
	}

	patched, changed := ensureNodeDocInteractiveMinima(doc, req, allowed, chunkByID, []uuid.UUID{cid})
	if !changed {
		t.Fatalf("expected interactive minima autofix to change doc")
	}
	patched, _, _ = sanitizeNodeDocCitations(patched, allowed, chunkByID, []uuid.UUID{cid})
	if errs, _ := content.ValidateNodeDocV1(patched, allowed, req); len(errs) > 0 {
		t.Fatalf("expected no validation errors after interactive minima autofix, got: %v", errs)
	}

	metrics := content.NodeDocMetrics(patched)
	bc, _ := metrics["block_counts"].(map[string]int)
	if bc["quick_check"] < 2 {
		t.Fatalf("expected quick_check>=2 got %d", bc["quick_check"])
	}
	if bc["flashcard"] < 1 {
		t.Fatalf("expected flashcard>=1 got %d", bc["flashcard"])
	}
}

func TestNodeDocRequirementsForTemplate_EnforcesInteractiveFloors(t *testing.T) {
	req := nodeDocRequirementsForTemplate("lesson", "concept")
	if req.MinQuickChecks < 2 {
		t.Fatalf("expected MinQuickChecks floor >=2, got %d", req.MinQuickChecks)
	}
	if req.MinFlashcards < 1 {
		t.Fatalf("expected MinFlashcards floor >=1, got %d", req.MinFlashcards)
	}
}

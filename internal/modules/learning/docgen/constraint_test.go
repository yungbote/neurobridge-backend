package docgen

import (
	"testing"

	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
)

func TestValidateDocAgainstBlueprint(t *testing.T) {
	doc := content.NodeDocV1{
		SchemaVersion: 1,
		Title:         "Sample",
		Summary:       "Summary",
		ConceptKeys:   []string{"concept_a"},
		Blocks: []map[string]any{
			{"id": "b1", "type": "paragraph", "md": "Hello"},
			{"id": "b2", "type": "quick_check", "prompt_md": "Q?", "answer_md": "A"},
		},
	}
	bp := DocBlueprintV1{
		SchemaVersion:       DocBlueprintSchemaVersion,
		BlueprintVersion:    "doc_blueprint_v1.0.0",
		PathID:              "path",
		PathNodeID:          "node",
		Objectives:          []string{},
		RequiredConceptKeys: []string{"concept_a"},
		Constraints: DocBlueprintConstraints{
			MinBlocks:          2,
			MinQuickChecks:     1,
			RequiredBlockKinds: []string{"paragraph"},
		},
	}

	report := ValidateDocAgainstBlueprint(doc, bp)
	if !report.Passed {
		t.Fatalf("expected pass, got violations: %+v", report.Violations)
	}
}

func TestValidateDocAgainstBlueprintOptionalSlots(t *testing.T) {
	doc := content.NodeDocV1{
		SchemaVersion: 1,
		Title:         "Sample",
		Summary:       "Summary",
		ConceptKeys:   []string{"concept_a"},
		Blocks: []map[string]any{
			{"id": "slot_prereq_bridge_1", "type": "paragraph", "md": "Bridge text"},
			{"id": "b2", "type": "quick_check", "prompt_md": "Q?", "answer_md": "A"},
		},
	}
	bp := DocBlueprintV1{
		SchemaVersion:       DocBlueprintSchemaVersion,
		BlueprintVersion:    "doc_blueprint_v1.0.0",
		PathID:              "path",
		PathNodeID:          "node",
		Objectives:          []string{},
		RequiredConceptKeys: []string{"concept_a"},
		OptionalSlots: []DocOptionalSlot{
			{
				SlotID:            "prereq_bridge",
				Purpose:           "prereq",
				MinBlocks:         1,
				MaxBlocks:         1,
				AllowedBlockKinds: []string{"paragraph"},
			},
		},
		Constraints: DocBlueprintConstraints{
			MinBlocks:          2,
			MinQuickChecks:     1,
			RequiredBlockKinds: []string{"paragraph"},
		},
	}

	report := ValidateDocAgainstBlueprint(doc, bp)
	if !report.Passed {
		t.Fatalf("expected pass, got violations: %+v", report.Violations)
	}
}

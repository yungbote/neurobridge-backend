package docgen

import (
	"strings"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
)

// ValidateDocAgainstBlueprint enforces immutable blueprint constraints on a doc.
func ValidateDocAgainstBlueprint(doc content.NodeDocV1, blueprint DocBlueprintV1) DocConstraintReportV1 {
	report := DocConstraintReportV1{
		SchemaVersion: DocConstraintReportSchemaVersion,
		Passed:        true,
		Violations:    []DocConstraintViolation{},
		CheckedAt:     time.Now().UTC().Format(time.RFC3339),
	}

	if errs := blueprint.Validate(); len(errs) > 0 {
		for _, e := range errs {
			report.Violations = append(report.Violations, DocConstraintViolation{
				Code:     "blueprint_invalid",
				Severity: "error",
				Message:  e,
			})
		}
	}

	if doc.SchemaVersion != 1 {
		report.Violations = append(report.Violations, DocConstraintViolation{
			Code:     "doc_schema_version",
			Severity: "error",
			Message:  "doc schema_version must be 1",
		})
	}

	blockCounts := map[string]int{}
	docText := ""
	if metrics := content.NodeDocMetrics(doc); len(metrics) > 0 {
		if bc, ok := metrics["block_counts"].(map[string]int); ok {
			blockCounts = bc
		}
		if dt, ok := metrics["doc_text"].(string); ok {
			docText = dt
		}
	}
	blockTotal := len(doc.Blocks)

	// Block count constraints.
	if blueprint.Constraints.MinBlocks > 0 && blockTotal < blueprint.Constraints.MinBlocks {
		report.Violations = append(report.Violations, DocConstraintViolation{
			Code:     "min_blocks",
			Severity: "error",
			Message:  "doc has fewer blocks than min",
		})
	}
	if blueprint.Constraints.MaxBlocks > 0 && blockTotal > blueprint.Constraints.MaxBlocks {
		report.Violations = append(report.Violations, DocConstraintViolation{
			Code:     "max_blocks",
			Severity: "error",
			Message:  "doc has more blocks than max",
		})
	}

	// Quick checks / flashcards.
	if blueprint.Constraints.MinQuickChecks > 0 && blockCounts["quick_check"] < blueprint.Constraints.MinQuickChecks {
		report.Violations = append(report.Violations, DocConstraintViolation{
			Code:     "min_quick_checks",
			Severity: "error",
			Message:  "doc has fewer quick_check blocks than min",
		})
	}
	if blueprint.Constraints.MaxQuickChecks > 0 && blockCounts["quick_check"] > blueprint.Constraints.MaxQuickChecks {
		report.Violations = append(report.Violations, DocConstraintViolation{
			Code:     "max_quick_checks",
			Severity: "error",
			Message:  "doc has more quick_check blocks than max",
		})
	}
	if blueprint.Constraints.MinFlashcards > 0 && blockCounts["flashcard"] < blueprint.Constraints.MinFlashcards {
		report.Violations = append(report.Violations, DocConstraintViolation{
			Code:     "min_flashcards",
			Severity: "error",
			Message:  "doc has fewer flashcard blocks than min",
		})
	}
	if blueprint.Constraints.MaxFlashcards > 0 && blockCounts["flashcard"] > blueprint.Constraints.MaxFlashcards {
		report.Violations = append(report.Violations, DocConstraintViolation{
			Code:     "max_flashcards",
			Severity: "error",
			Message:  "doc has more flashcard blocks than max",
		})
	}

	// Required block kinds.
	for _, kind := range blueprint.Constraints.RequiredBlockKinds {
		k := strings.ToLower(strings.TrimSpace(kind))
		if k == "" {
			continue
		}
		switch k {
		case "pitfalls":
			if blockCounts["misconceptions"]+blockCounts["common_mistakes"] == 0 {
				report.Violations = append(report.Violations, DocConstraintViolation{
					Code:     "required_block_kind",
					Severity: "error",
					Message:  "missing pitfalls block",
				})
			}
		default:
			if blockCounts[k] == 0 {
				report.Violations = append(report.Violations, DocConstraintViolation{
					Code:     "required_block_kind",
					Severity: "error",
					Message:  "missing required block kind: " + k,
				})
			}
		}
	}

	// Required concept keys.
	docConcepts := map[string]bool{}
	for _, k := range doc.ConceptKeys {
		if s := strings.TrimSpace(k); s != "" {
			docConcepts[strings.ToLower(s)] = true
		}
	}
	for _, b := range doc.Blocks {
		for _, k := range stringSliceFromAny(b["concept_keys"]) {
			if s := strings.TrimSpace(k); s != "" {
				docConcepts[strings.ToLower(s)] = true
			}
		}
	}
	for _, k := range blueprint.RequiredConceptKeys {
		if s := strings.ToLower(strings.TrimSpace(k)); s != "" && !docConcepts[s] {
			report.Violations = append(report.Violations, DocConstraintViolation{
				Code:     "missing_required_concept",
				Severity: "error",
				Message:  "missing required concept key: " + k,
			})
		}
	}

	// Required claims by citation id.
	docCitations := map[string]bool{}
	for _, b := range doc.Blocks {
		for _, cid := range citationIDsFromAny(b["citations"]) {
			docCitations[cid] = true
		}
	}
	for _, claim := range blueprint.RequiredClaims {
		if !claim.Required {
			continue
		}
		found := false
		for _, id := range claim.CitationIDs {
			if id != "" && docCitations[id] {
				found = true
				break
			}
		}
		if !found {
			report.Violations = append(report.Violations, DocConstraintViolation{
				Code:     "missing_required_claim",
				Severity: "error",
				Message:  "missing required claim: " + claim.ClaimID,
			})
		}
	}

	// Optional slots (targeted slot injection).
	if len(blueprint.OptionalSlots) > 0 {
		fills := SlotFillsFromDoc(doc, blueprint)
		fillByID := map[string]DocSlotFill{}
		for _, f := range fills {
			id := normalizeSlotID(f.SlotID)
			if id != "" {
				fillByID[id] = f
			}
		}
		for _, slot := range blueprint.OptionalSlots {
			slotID := normalizeSlotID(slot.SlotID)
			if slotID == "" {
				continue
			}
			fill := fillByID[slotID]
			count := fill.FilledBlocks
			minBlocks := slot.MinBlocks
			maxBlocks := slot.MaxBlocks
			if minBlocks < 0 {
				minBlocks = 0
			}
			if maxBlocks < 0 {
				maxBlocks = 0
			}
			if minBlocks > 0 && count < minBlocks {
				report.Violations = append(report.Violations, DocConstraintViolation{
					Code:     "slot_min_blocks",
					Severity: "error",
					Message:  "slot has fewer blocks than min: " + slotID,
					BlockID:  slotID,
				})
			}
			if maxBlocks > 0 && count > maxBlocks {
				report.Violations = append(report.Violations, DocConstraintViolation{
					Code:     "slot_max_blocks",
					Severity: "error",
					Message:  "slot has more blocks than max: " + slotID,
					BlockID:  slotID,
				})
			}
			allowed := normalizeBlockKinds(slot.AllowedBlockKinds)
			if len(allowed) > 0 && len(fill.BlockKinds) > 0 {
				allowedSet := map[string]bool{}
				for _, k := range allowed {
					allowedSet[k] = true
				}
				for i, kind := range fill.BlockKinds {
					if kind == "" {
						continue
					}
					if !allowedSet[kind] {
						blockID := slotID
						if i < len(fill.BlockIDs) && strings.TrimSpace(fill.BlockIDs[i]) != "" {
							blockID = strings.TrimSpace(fill.BlockIDs[i])
						}
						report.Violations = append(report.Violations, DocConstraintViolation{
							Code:     "slot_block_kind",
							Severity: "error",
							Message:  "slot block kind not allowed: " + kind,
							BlockID:  blockID,
						})
					}
				}
			}
		}
	}

	// Objectives presence (best-effort string match).
	if len(blueprint.Objectives) > 0 && docText != "" {
		lower := strings.ToLower(docText)
		for _, obj := range blueprint.Objectives {
			if s := strings.ToLower(strings.TrimSpace(obj)); s != "" && !strings.Contains(lower, s) {
				report.Violations = append(report.Violations, DocConstraintViolation{
					Code:     "missing_objective",
					Severity: "error",
					Message:  "missing objective: " + obj,
				})
			}
		}
	}

	// Forbidden phrases.
	if len(blueprint.Constraints.ForbiddenPhrases) > 0 && docText != "" {
		lower := strings.ToLower(docText)
		for _, phrase := range blueprint.Constraints.ForbiddenPhrases {
			p := strings.ToLower(strings.TrimSpace(phrase))
			if p != "" && strings.Contains(lower, p) {
				report.Violations = append(report.Violations, DocConstraintViolation{
					Code:     "forbidden_phrase",
					Severity: "error",
					Message:  "forbidden phrase present: " + phrase,
				})
			}
		}
	}

	if len(report.Violations) > 0 {
		report.Passed = false
	}
	return report
}

func stringSliceFromAny(v any) []string {
	out := []string{}
	switch raw := v.(type) {
	case []string:
		for _, s := range raw {
			out = append(out, s)
		}
	case []any:
		for _, item := range raw {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func citationIDsFromAny(v any) []string {
	out := []string{}
	switch raw := v.(type) {
	case []any:
		for _, item := range raw {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			id := strings.TrimSpace(stringFromAny(m["chunk_id"]))
			if id == "" {
				id = strings.TrimSpace(stringFromAny(m["chunkID"]))
			}
			if id != "" {
				out = append(out, id)
			}
		}
	}
	return out
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

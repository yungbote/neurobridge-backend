package content

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
)

var wordRE = regexp.MustCompile(`[A-Za-z0-9]+(?:'[A-Za-z0-9]+)?`)

func WordCount(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	return len(wordRE.FindAllString(s, -1))
}

func NormalizeConceptKeys(keys []string) []string {
	out := make([]string, 0, len(keys))
	seen := map[string]bool{}
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}

func NodeDocMetrics(doc NodeDocV1) map[string]any {
	blocks := doc.Blocks
	blockCounts := map[string]int{}
	wordCount := WordCount(doc.Title) + WordCount(doc.Summary)

	concat := []string{doc.Title, doc.Summary}

	for _, b := range blocks {
		t := strings.ToLower(strings.TrimSpace(stringFromAny(b["type"])))
		if t == "" {
			t = "unknown"
		}
		blockCounts[t]++

		switch t {
		case "heading":
			concat = append(concat, stringFromAny(b["text"]))
			wordCount += WordCount(stringFromAny(b["text"]))
		case "paragraph":
			concat = append(concat, stringFromAny(b["md"]))
			wordCount += WordCount(stripMD(stringFromAny(b["md"])))
		case "callout":
			concat = append(concat, stringFromAny(b["title"]), stringFromAny(b["md"]))
			wordCount += WordCount(stripMD(stringFromAny(b["title"]) + " " + stringFromAny(b["md"])))
		case "code":
			concat = append(concat, stringFromAny(b["filename"]), stringFromAny(b["language"]))
		case "figure":
			concat = append(concat, stringFromAny(b["caption"]))
			wordCount += WordCount(stripMD(stringFromAny(b["caption"])))
		case "video":
			concat = append(concat, stringFromAny(b["caption"]))
			wordCount += WordCount(stripMD(stringFromAny(b["caption"])))
		case "diagram":
			concat = append(concat, stringFromAny(b["caption"]))
			wordCount += WordCount(stripMD(stringFromAny(b["caption"])))
		case "table":
			concat = append(concat, stringFromAny(b["caption"]))
			wordCount += WordCount(stripMD(stringFromAny(b["caption"])))
		case "equation":
			concat = append(concat, stringFromAny(b["latex"]), stringFromAny(b["caption"]))
			wordCount += WordCount(stripMD(stringFromAny(b["caption"])))
		case "quick_check":
			p := stringFromAny(b["prompt_md"])
			a := stringFromAny(b["answer_md"])
			concat = append(concat, p, a)
			wordCount += WordCount(stripMD(p + " " + a))
			if raw, ok := b["options"].([]any); ok && len(raw) > 0 {
				opts := make([]string, 0, len(raw))
				for _, x := range raw {
					m, ok := x.(map[string]any)
					if !ok || m == nil {
						continue
					}
					txt := strings.TrimSpace(stringFromAny(m["text"]))
					if txt != "" {
						opts = append(opts, txt)
					}
				}
				if len(opts) > 0 {
					joined := strings.Join(opts, "\n")
					concat = append(concat, joined)
					wordCount += WordCount(stripMD(joined))
				}
			}
		case "flashcard":
			front := stringFromAny(b["front_md"])
			back := stringFromAny(b["back_md"])
			concat = append(concat, front, back)
			wordCount += WordCount(stripMD(front + " " + back))
		case "objectives", "prerequisites", "key_takeaways", "common_mistakes", "misconceptions", "edge_cases", "heuristics", "checklist", "connections":
			items := stringSliceFromAny(b["items_md"])
			joined := strings.Join(items, "\n")
			concat = append(concat, stringFromAny(b["title"]), joined)
			wordCount += WordCount(stripMD(stringFromAny(b["title"]) + " " + joined))
		case "steps":
			steps := stringSliceFromAny(b["steps_md"])
			joined := strings.Join(steps, "\n")
			concat = append(concat, stringFromAny(b["title"]), joined)
			wordCount += WordCount(stripMD(stringFromAny(b["title"]) + " " + joined))
		case "glossary":
			concat = append(concat, stringFromAny(b["title"]))
			wordCount += WordCount(stripMD(stringFromAny(b["title"])))
			if arr, ok := b["terms"].([]any); ok {
				for _, it := range arr {
					m, ok := it.(map[string]any)
					if !ok {
						continue
					}
					term := stringFromAny(m["term"])
					def := stringFromAny(m["definition_md"])
					concat = append(concat, term, def)
					wordCount += WordCount(stripMD(term + " " + def))
				}
			}
		case "faq":
			concat = append(concat, stringFromAny(b["title"]))
			wordCount += WordCount(stripMD(stringFromAny(b["title"])))
			if arr, ok := b["qas"].([]any); ok {
				for _, it := range arr {
					m, ok := it.(map[string]any)
					if !ok {
						continue
					}
					q := stringFromAny(m["question_md"])
					a := stringFromAny(m["answer_md"])
					concat = append(concat, q, a)
					wordCount += WordCount(stripMD(q + " " + a))
				}
			}
		case "intuition", "mental_model", "why_it_matters":
			concat = append(concat, stringFromAny(b["title"]), stringFromAny(b["md"]))
			wordCount += WordCount(stripMD(stringFromAny(b["title"]) + " " + stringFromAny(b["md"])))
		}
	}

	return map[string]any{
		"word_count":   wordCount,
		"block_counts": blockCounts,
		"doc_text":     strings.TrimSpace(strings.Join(concat, "\n")),
	}
}

type NodeDocRequirements struct {
	MinWordCount    int
	MinHeadings     int
	MinParagraphs   int
	MinCallouts     int
	MinQuickChecks  int
	MinFlashcards   int
	MinDiagrams     int
	MinTables       int
	MinWhyItMatters int
	MinIntuition    int
	MinMentalModels int
	MinPitfalls     int // misconceptions + common_mistakes
	MinSteps        int
	MinChecklist    int
	MinConnections  int
	RequireMedia    bool
	RequireExample  bool
}

func DefaultNodeDocRequirements() NodeDocRequirements {
	return NodeDocRequirements{
		// Default to a course-quality "concept" lesson: narrative + worked example + retrieval.
		MinWordCount:    1100,
		MinHeadings:     3,
		MinParagraphs:   8,
		MinCallouts:     2,
		MinQuickChecks:  3,
		MinFlashcards:   0,
		MinDiagrams:     0,
		MinTables:       0,
		MinWhyItMatters: 1,
		MinIntuition:    1,
		MinMentalModels: 1,
		MinPitfalls:     1,
		RequireMedia:    false,
		RequireExample:  true,
	}
}

func ValidateNodeDocV1(doc NodeDocV1, allowedChunkIDs map[string]bool, req NodeDocRequirements) ([]string, map[string]any) {
	errs := make([]string, 0)

	if doc.SchemaVersion != 1 {
		errs = append(errs, fmt.Sprintf("schema_version must be 1 (got %d)", doc.SchemaVersion))
	}
	if strings.TrimSpace(doc.Title) == "" {
		errs = append(errs, "title missing")
	}
	doc.ConceptKeys = NormalizeConceptKeys(doc.ConceptKeys)
	if len(doc.ConceptKeys) == 0 {
		errs = append(errs, "concept_keys missing")
	}
	if len(doc.Blocks) == 0 {
		errs = append(errs, "blocks missing")
	}

	metrics := NodeDocMetrics(doc)
	wordCount, _ := metrics["word_count"].(int)
	if req.MinWordCount > 0 && wordCount < req.MinWordCount {
		errs = append(errs, fmt.Sprintf("word_count too low (%d < %d)", wordCount, req.MinWordCount))
	}

	bc, _ := metrics["block_counts"].(map[string]int)
	headingCount := bc["heading"]
	if req.MinHeadings > 0 && headingCount < req.MinHeadings {
		errs = append(errs, fmt.Sprintf("need >=%d headings (got %d)", req.MinHeadings, headingCount))
	}
	if req.MinParagraphs > 0 && bc["paragraph"] < req.MinParagraphs {
		errs = append(errs, fmt.Sprintf("need >=%d paragraph blocks (got %d)", req.MinParagraphs, bc["paragraph"]))
	}
	if req.MinCallouts > 0 && bc["callout"] < req.MinCallouts {
		errs = append(errs, fmt.Sprintf("need >=%d callout blocks (got %d)", req.MinCallouts, bc["callout"]))
	}
	qcCount := bc["quick_check"]
	if req.MinQuickChecks > 0 && qcCount < req.MinQuickChecks {
		errs = append(errs, fmt.Sprintf("need >=%d quick_check blocks (got %d)", req.MinQuickChecks, qcCount))
	}
	fcCount := bc["flashcard"]
	if req.MinFlashcards > 0 && fcCount < req.MinFlashcards {
		errs = append(errs, fmt.Sprintf("need >=%d flashcard blocks (got %d)", req.MinFlashcards, fcCount))
	}
	if req.MinDiagrams > 0 && bc["diagram"] < req.MinDiagrams {
		errs = append(errs, fmt.Sprintf("need >=%d diagram blocks (got %d)", req.MinDiagrams, bc["diagram"]))
	}
	if req.MinTables > 0 && bc["table"] < req.MinTables {
		errs = append(errs, fmt.Sprintf("need >=%d table blocks (got %d)", req.MinTables, bc["table"]))
	}
	if req.MinWhyItMatters > 0 && bc["why_it_matters"] < req.MinWhyItMatters {
		errs = append(errs, fmt.Sprintf("need >=%d why_it_matters blocks (got %d)", req.MinWhyItMatters, bc["why_it_matters"]))
	}
	if req.MinIntuition > 0 && bc["intuition"] < req.MinIntuition {
		errs = append(errs, fmt.Sprintf("need >=%d intuition blocks (got %d)", req.MinIntuition, bc["intuition"]))
	}
	if req.MinMentalModels > 0 && bc["mental_model"] < req.MinMentalModels {
		errs = append(errs, fmt.Sprintf("need >=%d mental_model blocks (got %d)", req.MinMentalModels, bc["mental_model"]))
	}
	if req.MinPitfalls > 0 && bc["misconceptions"]+bc["common_mistakes"] < req.MinPitfalls {
		errs = append(errs, fmt.Sprintf("need >=%d misconceptions|common_mistakes blocks (got %d)", req.MinPitfalls, bc["misconceptions"]+bc["common_mistakes"]))
	}
	if req.MinSteps > 0 && bc["steps"] < req.MinSteps {
		errs = append(errs, fmt.Sprintf("need >=%d steps blocks (got %d)", req.MinSteps, bc["steps"]))
	}
	if req.MinChecklist > 0 && bc["checklist"] < req.MinChecklist {
		errs = append(errs, fmt.Sprintf("need >=%d checklist blocks (got %d)", req.MinChecklist, bc["checklist"]))
	}
	if req.MinConnections > 0 && bc["connections"] < req.MinConnections {
		errs = append(errs, fmt.Sprintf("need >=%d connections blocks (got %d)", req.MinConnections, bc["connections"]))
	}
	hasMedia := bc["figure"] > 0 || bc["diagram"] > 0 || bc["table"] > 0
	if req.RequireMedia && !hasMedia {
		errs = append(errs, "need at least one figure|diagram|table block")
	}

	if req.RequireExample && !hasWorkedExample(doc) {
		errs = append(errs, "missing worked example (heading containing 'example' or a tip callout titled 'Worked example')")
	}

	// Reject meta content in learner-facing text.
	docText, _ := metrics["doc_text"].(string)
	if bad := findBannedPhrases(docText); len(bad) > 0 {
		// Store exact hits in metrics for observability, but avoid echoing banned phrases in
		// retry feedback (otherwise the model can “learn” the phrase from the error string).
		metrics["banned_phrases"] = bad
		errs = append(errs, fmt.Sprintf("contains banned meta phrasing (%d hits)", len(bad)))
	}

	// Per-block validation (citations + required fields).
	for i, b := range doc.Blocks {
		t := strings.ToLower(strings.TrimSpace(stringFromAny(b["type"])))
		switch t {
		case "heading":
			level := intFromAny(b["level"], 0)
			if level < 2 || level > 4 {
				errs = append(errs, fmt.Sprintf("block[%d] heading.level must be 2-4 (got %d)", i, level))
			}
			if strings.TrimSpace(stringFromAny(b["text"])) == "" {
				errs = append(errs, fmt.Sprintf("block[%d] heading.text missing", i))
			}
		case "paragraph":
			if strings.TrimSpace(stringFromAny(b["md"])) == "" {
				errs = append(errs, fmt.Sprintf("block[%d] paragraph.md missing", i))
			}
			errs = append(errs, validateCitations(i, b["citations"], allowedChunkIDs)...)
		case "callout":
			variant := strings.ToLower(strings.TrimSpace(stringFromAny(b["variant"])))
			if variant != "info" && variant != "tip" && variant != "warning" {
				errs = append(errs, fmt.Sprintf("block[%d] callout.variant invalid (%q)", i, variant))
			}
			if strings.TrimSpace(stringFromAny(b["md"])) == "" {
				errs = append(errs, fmt.Sprintf("block[%d] callout.md missing", i))
			}
			errs = append(errs, validateCitations(i, b["citations"], allowedChunkIDs)...)
		case "code":
			if strings.TrimSpace(stringFromAny(b["code"])) == "" {
				errs = append(errs, fmt.Sprintf("block[%d] code.code missing", i))
			}
		case "figure":
			asset, _ := b["asset"].(map[string]any)
			url := strings.TrimSpace(stringFromAny(asset["url"]))
			if url == "" {
				errs = append(errs, fmt.Sprintf("block[%d] figure.asset.url missing", i))
			}
			errs = append(errs, validateCitations(i, b["citations"], allowedChunkIDs)...)
		case "video":
			if strings.TrimSpace(stringFromAny(b["url"])) == "" {
				errs = append(errs, fmt.Sprintf("block[%d] video.url missing", i))
			}
		case "diagram":
			kind := strings.ToLower(strings.TrimSpace(stringFromAny(b["kind"])))
			if kind != "svg" && kind != "mermaid" {
				errs = append(errs, fmt.Sprintf("block[%d] diagram.kind invalid (%q)", i, kind))
			}
			if strings.TrimSpace(stringFromAny(b["source"])) == "" {
				errs = append(errs, fmt.Sprintf("block[%d] diagram.source missing", i))
			}
			errs = append(errs, validateCitations(i, b["citations"], allowedChunkIDs)...)
		case "table":
			cols := stringSliceFromAny(b["columns"])
			rows := stringMatrixFromAny(b["rows"])
			if len(cols) == 0 {
				errs = append(errs, fmt.Sprintf("block[%d] table.columns missing", i))
			}
			if len(rows) == 0 {
				errs = append(errs, fmt.Sprintf("block[%d] table.rows missing", i))
			}
			errs = append(errs, validateCitations(i, b["citations"], allowedChunkIDs)...)
		case "equation":
			if strings.TrimSpace(stringFromAny(b["latex"])) == "" {
				errs = append(errs, fmt.Sprintf("block[%d] equation.latex missing", i))
			}
			// display is required by schema; best-effort validate type
			if _, ok := b["display"].(bool); !ok {
				if _, okFloat := b["display"].(float64); !okFloat {
					errs = append(errs, fmt.Sprintf("block[%d] equation.display missing", i))
				}
			}
			errs = append(errs, validateCitations(i, b["citations"], allowedChunkIDs)...)
		case "quick_check":
			if strings.TrimSpace(stringFromAny(b["prompt_md"])) == "" {
				errs = append(errs, fmt.Sprintf("block[%d] quick_check.prompt_md missing", i))
			}
			if strings.TrimSpace(stringFromAny(b["answer_md"])) == "" {
				errs = append(errs, fmt.Sprintf("block[%d] quick_check.answer_md missing", i))
			}
			kind := strings.ToLower(strings.TrimSpace(stringFromAny(b["kind"])))
			answerID := strings.TrimSpace(stringFromAny(b["answer_id"]))
			rawOptions, _ := b["options"].([]any)
			isChoice := kind == "mcq" || kind == "true_false" || len(rawOptions) > 0 || answerID != ""
			if isChoice {
				label := kind
				if strings.TrimSpace(label) == "" {
					label = "choice"
				}
				if len(rawOptions) < 2 {
					errs = append(errs, fmt.Sprintf("block[%d] quick_check.options needs >=2 options for %s", i, label))
				}
				optIDs := map[string]bool{}
				for j, x := range rawOptions {
					m, ok := x.(map[string]any)
					if !ok || m == nil {
						errs = append(errs, fmt.Sprintf("block[%d] quick_check.options[%d] invalid", i, j))
						continue
					}
					oid := strings.TrimSpace(stringFromAny(m["id"]))
					txt := strings.TrimSpace(stringFromAny(m["text"]))
					if oid == "" {
						errs = append(errs, fmt.Sprintf("block[%d] quick_check.options[%d].id missing", i, j))
					} else if optIDs[oid] {
						errs = append(errs, fmt.Sprintf("block[%d] quick_check.options[%d].id duplicate %q", i, j, oid))
					}
					if txt == "" {
						errs = append(errs, fmt.Sprintf("block[%d] quick_check.options[%d].text missing", i, j))
					}
					if oid != "" {
						optIDs[oid] = true
					}
				}
				if strings.TrimSpace(answerID) == "" {
					errs = append(errs, fmt.Sprintf("block[%d] quick_check.answer_id missing", i))
				} else if len(optIDs) > 0 && !optIDs[answerID] {
					errs = append(errs, fmt.Sprintf("block[%d] quick_check.answer_id %q not in options", i, answerID))
				}
			}
			errs = append(errs, validateCitations(i, b["citations"], allowedChunkIDs)...)
		case "flashcard":
			if strings.TrimSpace(stringFromAny(b["front_md"])) == "" {
				errs = append(errs, fmt.Sprintf("block[%d] flashcard.front_md missing", i))
			}
			if strings.TrimSpace(stringFromAny(b["back_md"])) == "" {
				errs = append(errs, fmt.Sprintf("block[%d] flashcard.back_md missing", i))
			}
			errs = append(errs, validateCitations(i, b["citations"], allowedChunkIDs)...)
		case "divider":
			// ok
		case "objectives", "prerequisites", "key_takeaways", "common_mistakes", "misconceptions", "edge_cases", "heuristics", "checklist", "connections":
			items := stringSliceFromAny(b["items_md"])
			if len(items) == 0 {
				errs = append(errs, fmt.Sprintf("block[%d] %s.items_md missing", i, t))
			}
			errs = append(errs, validateCitations(i, b["citations"], allowedChunkIDs)...)
		case "steps":
			steps := stringSliceFromAny(b["steps_md"])
			if len(steps) == 0 {
				errs = append(errs, fmt.Sprintf("block[%d] steps.steps_md missing", i))
			}
			errs = append(errs, validateCitations(i, b["citations"], allowedChunkIDs)...)
		case "glossary":
			arr, ok := b["terms"].([]any)
			if !ok || len(arr) == 0 {
				errs = append(errs, fmt.Sprintf("block[%d] glossary.terms missing", i))
			} else {
				for j, it := range arr {
					m, ok := it.(map[string]any)
					if !ok {
						errs = append(errs, fmt.Sprintf("block[%d] glossary.terms[%d] invalid", i, j))
						continue
					}
					if strings.TrimSpace(stringFromAny(m["term"])) == "" {
						errs = append(errs, fmt.Sprintf("block[%d] glossary.terms[%d].term missing", i, j))
					}
					if strings.TrimSpace(stringFromAny(m["definition_md"])) == "" {
						errs = append(errs, fmt.Sprintf("block[%d] glossary.terms[%d].definition_md missing", i, j))
					}
				}
			}
			errs = append(errs, validateCitations(i, b["citations"], allowedChunkIDs)...)
		case "faq":
			arr, ok := b["qas"].([]any)
			if !ok || len(arr) == 0 {
				errs = append(errs, fmt.Sprintf("block[%d] faq.qas missing", i))
			} else {
				for j, it := range arr {
					m, ok := it.(map[string]any)
					if !ok {
						errs = append(errs, fmt.Sprintf("block[%d] faq.qas[%d] invalid", i, j))
						continue
					}
					if strings.TrimSpace(stringFromAny(m["question_md"])) == "" {
						errs = append(errs, fmt.Sprintf("block[%d] faq.qas[%d].question_md missing", i, j))
					}
					if strings.TrimSpace(stringFromAny(m["answer_md"])) == "" {
						errs = append(errs, fmt.Sprintf("block[%d] faq.qas[%d].answer_md missing", i, j))
					}
				}
			}
			errs = append(errs, validateCitations(i, b["citations"], allowedChunkIDs)...)
		case "intuition", "mental_model", "why_it_matters":
			if strings.TrimSpace(stringFromAny(b["md"])) == "" {
				errs = append(errs, fmt.Sprintf("block[%d] %s.md missing", i, t))
			}
			errs = append(errs, validateCitations(i, b["citations"], allowedChunkIDs)...)
		default:
			errs = append(errs, fmt.Sprintf("block[%d] unknown type %q", i, t))
		}
	}

	return dedupeStrings(errs), metrics
}

func ValidateDrillPayloadV1(p DrillPayloadV1, allowedChunkIDs map[string]bool, kind string, minCount int, maxCount int, allowedConceptKeys []string) ([]string, map[string]any) {
	errs := make([]string, 0)

	if p.SchemaVersion != 1 {
		errs = append(errs, fmt.Sprintf("schema_version must be 1 (got %d)", p.SchemaVersion))
	}
	k := strings.ToLower(strings.TrimSpace(p.Kind))
	if kind != "" && k != strings.ToLower(strings.TrimSpace(kind)) {
		errs = append(errs, fmt.Sprintf("payload.kind mismatch (got %q want %q)", k, kind))
	}
	if k != "flashcards" && k != "quiz" {
		errs = append(errs, fmt.Sprintf("kind must be flashcards|quiz (got %q)", k))
	}

	metrics := map[string]any{}
	allowedKeys := map[string]bool{}
	for _, k := range allowedConceptKeys {
		k = strings.TrimSpace(strings.ToLower(k))
		if k != "" {
			allowedKeys[k] = true
		}
	}
	enforceConceptKeys := len(allowedKeys) > 0
	coveredConceptKeys := map[string]bool{}

	switch k {
	case "flashcards":
		if len(p.Questions) > 0 {
			errs = append(errs, "flashcards payload must have questions=[]")
		}
		n := len(p.Cards)
		metrics["cards_count"] = n
		if n == 0 {
			errs = append(errs, "no cards")
		}
		if minCount > 0 && n < minCount {
			errs = append(errs, fmt.Sprintf("too few cards (%d < %d)", n, minCount))
		}
		if maxCount > 0 && n > maxCount {
			errs = append(errs, fmt.Sprintf("too many cards (%d > %d)", n, maxCount))
		}
		for i, c := range p.Cards {
			if strings.TrimSpace(c.FrontMD) == "" {
				errs = append(errs, fmt.Sprintf("card[%d] front_md missing", i))
			}
			if strings.TrimSpace(c.BackMD) == "" {
				errs = append(errs, fmt.Sprintf("card[%d] back_md missing", i))
			}
			if enforceConceptKeys && len(c.ConceptKeys) == 0 {
				errs = append(errs, fmt.Sprintf("card[%d] concept_keys missing", i))
			}
			for _, ck := range c.ConceptKeys {
				ck = strings.TrimSpace(strings.ToLower(ck))
				if ck == "" {
					errs = append(errs, fmt.Sprintf("card[%d] concept_keys contains empty key", i))
					continue
				}
				if enforceConceptKeys && !allowedKeys[ck] {
					errs = append(errs, fmt.Sprintf("card[%d] concept_key %q not allowed", i, ck))
					continue
				}
				coveredConceptKeys[ck] = true
			}
			errs = append(errs, validateCitationRefs(fmt.Sprintf("card[%d]", i), c.Citations, allowedChunkIDs)...)
		}
	case "quiz":
		if len(p.Cards) > 0 {
			errs = append(errs, "quiz payload must have cards=[]")
		}
		n := len(p.Questions)
		metrics["questions_count"] = n
		if n == 0 {
			errs = append(errs, "no questions")
		}
		if minCount > 0 && n < minCount {
			errs = append(errs, fmt.Sprintf("too few questions (%d < %d)", n, minCount))
		}
		if maxCount > 0 && n > maxCount {
			errs = append(errs, fmt.Sprintf("too many questions (%d > %d)", n, maxCount))
		}
		for i, q := range p.Questions {
			prefix := fmt.Sprintf("question[%d]", i)
			if strings.TrimSpace(q.ID) == "" {
				errs = append(errs, prefix+" id missing")
			}
			if enforceConceptKeys && len(q.ConceptKeys) == 0 {
				errs = append(errs, prefix+" concept_keys missing")
			}
			for _, ck := range q.ConceptKeys {
				ck = strings.TrimSpace(strings.ToLower(ck))
				if ck == "" {
					errs = append(errs, prefix+" concept_keys contains empty key")
					continue
				}
				if enforceConceptKeys && !allowedKeys[ck] {
					errs = append(errs, fmt.Sprintf("%s concept_key %q not allowed", prefix, ck))
					continue
				}
				coveredConceptKeys[ck] = true
			}
			if strings.TrimSpace(q.PromptMD) == "" {
				errs = append(errs, prefix+" prompt_md missing")
			}
			if strings.TrimSpace(q.ExplanationMD) == "" {
				errs = append(errs, prefix+" explanation_md missing")
			}
			if len(q.Options) < 2 {
				errs = append(errs, prefix+" needs >=2 options")
			}
			optIDs := map[string]bool{}
			for j, o := range q.Options {
				if strings.TrimSpace(o.ID) == "" {
					errs = append(errs, fmt.Sprintf("%s option[%d] id missing", prefix, j))
				}
				if strings.TrimSpace(o.Text) == "" {
					errs = append(errs, fmt.Sprintf("%s option[%d] text missing", prefix, j))
				}
				if o.ID != "" {
					if optIDs[o.ID] {
						errs = append(errs, fmt.Sprintf("%s option ids must be unique (dup %q)", prefix, o.ID))
					}
					optIDs[o.ID] = true
				}
			}
			if strings.TrimSpace(q.AnswerID) == "" {
				errs = append(errs, prefix+" answer_id missing")
			} else if !optIDs[q.AnswerID] {
				errs = append(errs, fmt.Sprintf("%s answer_id %q not in options", prefix, q.AnswerID))
			}
			errs = append(errs, validateCitationRefs(prefix, q.Citations, allowedChunkIDs)...)
		}
	}

	metrics["concept_keys_enforced"] = enforceConceptKeys
	metrics["concept_keys_covered"] = len(coveredConceptKeys)

	return dedupeStrings(errs), metrics
}

func findBannedPhrases(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	l := strings.ToLower(text)
	phrases := []string{
		"quick check-in",
		"entry check",
		"before we dive in",
		"answer these",
		"here's the plan",
		"here is the plan",
		"plan:",
		"up next",
		"next up",
		"next lesson",
		"next module",
		"in the next lesson",
		"you've seen the plan",
		"youve seen the plan",
		"let's anchor",
		"lets anchor",
		"no magic",
		"no sorcery",
		"your next hop",
		// "next hop" is a legitimate networking term; allow it unless phrased as "your next hop".
		"bridge-in",
		"bridge in",
		"bridge-out",
		"bridge out",
		"recommended drills",
		"reveal answer",
		"wrap-up",
		"wrap up",
		"i can tailor this",
		"pick one",
		"what are you using this for",
		"what's your current",
		"what is your current",
		"do you prefer",
		"any constraints",
		"while you think about that",
		"if you want to go deeper",
		"if you'd like to go deeper",
		"let me know if you want",
	}
	var hit []string
	for _, p := range phrases {
		if strings.Contains(l, p) {
			hit = append(hit, p)
		}
	}
	sort.Strings(hit)
	return hit
}

// DetectMetaPhrases returns a list of meta phrases found in raw text.
func DetectMetaPhrases(text string) []string {
	return findBannedPhrases(text)
}

// DetectNodeDocMetaPhrases scans a NodeDocV1 for meta phrasing.
func DetectNodeDocMetaPhrases(doc NodeDocV1) []string {
	metrics := NodeDocMetrics(doc)
	if raw, ok := metrics["doc_text"].(string); ok {
		return findBannedPhrases(raw)
	}
	return nil
}

func hasWorkedExample(doc NodeDocV1) bool {
	for _, b := range doc.Blocks {
		t := strings.ToLower(strings.TrimSpace(stringFromAny(b["type"])))
		switch t {
		case "heading":
			txt := strings.ToLower(strings.TrimSpace(stringFromAny(b["text"])))
			if strings.Contains(txt, "example") {
				return true
			}
		case "callout":
			variant := strings.ToLower(strings.TrimSpace(stringFromAny(b["variant"])))
			title := strings.ToLower(strings.TrimSpace(stringFromAny(b["title"])))
			if variant == "tip" && (title == "worked example" || strings.HasPrefix(title, "worked example")) {
				return true
			}
		}
	}
	return false
}

func validateCitations(blockIndex int, raw any, allowed map[string]bool) []string {
	arr, ok := raw.([]any)
	if !ok {
		// If missing, treat as empty.
		arr = nil
	}
	refs := make([]CitationRefV1, 0, len(arr))
	for _, x := range arr {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		locAny, _ := m["loc"].(map[string]any)
		refs = append(refs, CitationRefV1{
			ChunkID: strings.TrimSpace(stringFromAny(m["chunk_id"])),
			Quote:   strings.TrimSpace(stringFromAny(m["quote"])),
			Loc: CitationLocV1{
				Page:  intFromAny(locAny["page"], 0),
				Start: intFromAny(locAny["start"], 0),
				End:   intFromAny(locAny["end"], 0),
			},
		})
	}
	return validateCitationRefs(fmt.Sprintf("block[%d]", blockIndex), refs, allowed)
}

func validateCitationRefs(prefix string, refs []CitationRefV1, allowed map[string]bool) []string {
	errs := make([]string, 0)
	if len(refs) == 0 {
		errs = append(errs, prefix+" citations missing")
		return errs
	}
	seen := map[string]bool{}
	for _, c := range refs {
		id := strings.TrimSpace(c.ChunkID)
		if id == "" {
			errs = append(errs, prefix+" citation.chunk_id missing")
			continue
		}
		if _, err := uuid.Parse(id); err != nil {
			errs = append(errs, prefix+" citation.chunk_id invalid uuid: "+id)
			continue
		}
		if allowed != nil && len(allowed) > 0 && !allowed[id] {
			errs = append(errs, prefix+" citation.chunk_id not allowed: "+id)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		if len(c.Quote) > 240 {
			errs = append(errs, prefix+" citation.quote too long")
		}
		if c.Loc.Start < 0 || c.Loc.End < 0 || c.Loc.Page < 0 {
			errs = append(errs, prefix+" citation.loc must be non-negative")
		}
		if c.Loc.End > 0 && c.Loc.Start > 0 && c.Loc.End < c.Loc.Start {
			errs = append(errs, prefix+" citation.loc end < start")
		}
	}
	return errs
}

func stripMD(s string) string {
	// Cheaply remove some markdown noise for word counts.
	s = strings.ReplaceAll(s, "`", " ")
	s = strings.ReplaceAll(s, "*", " ")
	s = strings.ReplaceAll(s, "#", " ")
	return s
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(v)
	}
}

func intFromAny(v any, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		i, _ := t.Int64()
		return int(i)
	default:
		return def
	}
}

func stringSliceFromAny(v any) []string {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, it := range t {
			s := strings.TrimSpace(stringFromAny(it))
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func stringMatrixFromAny(v any) [][]string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([][]string, 0, len(arr))
	for _, row := range arr {
		out = append(out, stringSliceFromAny(row))
	}
	return out
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

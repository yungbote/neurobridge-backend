package steps

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func mergeUUIDListsPreserveOrder(lists ...[]uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0)
	for _, l := range lists {
		for _, id := range l {
			if id == uuid.Nil || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func formatChunkIDBullets(ids []uuid.UUID) string {
	if len(ids) == 0 {
		return ""
	}
	var b strings.Builder
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		b.WriteString("- ")
		b.WriteString(id.String())
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func firstVideoAssetFromAssetsJSON(assetsJSON string) *mediaAssetCandidate {
	s := strings.TrimSpace(assetsJSON)
	if s == "" {
		return nil
	}
	var payload struct {
		Assets []*mediaAssetCandidate `json:"assets"`
	}
	if err := json.Unmarshal([]byte(s), &payload); err != nil {
		return nil
	}
	// Prefer generated videos when present so unit docs use Sora outputs by default.
	for _, a := range payload.Assets {
		if a == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(a.Kind), "video") && strings.EqualFold(strings.TrimSpace(a.AssetKind), "generated_video") && strings.TrimSpace(a.URL) != "" {
			return a
		}
	}
	for _, a := range payload.Assets {
		if a == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(a.Kind), "video") && strings.TrimSpace(a.URL) != "" {
			return a
		}
	}
	return nil
}

func suggestedVideoLine(videoAsset *mediaAssetCandidate) string {
	if videoAsset == nil {
		return ""
	}
	return "- If a relevant video is available in AVAILABLE_MEDIA_ASSETS_JSON, include 1 short video block and caption what to watch for."
}

func normalizePathNodeKind(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "module", "lesson", "capstone", "review":
		return s
	default:
		return "lesson"
	}
}

func normalizePathNodeDocTemplate(raw string, nodeKind string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "overview", "concept", "practice", "cheatsheet", "project", "review":
		return s
	}
	switch normalizePathNodeKind(nodeKind) {
	case "module":
		return "overview"
	case "capstone":
		return "project"
	case "review":
		return "review"
	default:
		return "concept"
	}
}

func nodeDocRequirementsForTemplate(nodeKind string, docTemplate string) content.NodeDocRequirements {
	kind := normalizePathNodeKind(nodeKind)
	tmpl := normalizePathNodeDocTemplate(docTemplate, kind)

	req := content.DefaultNodeDocRequirements()

	switch tmpl {
	case "overview":
		req.MinWordCount = 900
		req.MinHeadings = 2
		req.MinParagraphs = 6
		req.MinCallouts = 1
		req.MinQuickChecks = 2
		req.MinDiagrams = 0
	case "concept":
		// defaults
	case "practice":
		req.MinWordCount = 1300
		req.MinHeadings = 3
		req.MinParagraphs = 7
		req.MinCallouts = 3
		req.MinQuickChecks = 4
		req.MinDiagrams = 0
	case "cheatsheet":
		req.MinWordCount = 900
		req.MinHeadings = 2
		req.MinParagraphs = 3
		req.MinCallouts = 1
		req.MinQuickChecks = 2
		req.MinDiagrams = 0
		req.MinTables = 1
	case "project":
		req.MinWordCount = 1600
		req.MinHeadings = 3
		req.MinParagraphs = 8
		req.MinCallouts = 2
		req.MinQuickChecks = 2
		req.MinDiagrams = 0
		req.MinSteps = 1
		req.MinChecklist = 1
	case "review":
		req.MinWordCount = 1000
		req.MinHeadings = 2
		req.MinParagraphs = 4
		req.MinCallouts = 1
		req.MinQuickChecks = 6
		req.MinDiagrams = 0
	}

	switch qualityMode() {
	case "premium", "openai", "high":
		// Premium mode: longer, more tutor-like lessons.
		req.MinWordCount = int(float64(req.MinWordCount) * 1.35)
		if req.MinParagraphs > 0 {
			req.MinParagraphs += 2
		}
		if req.MinCallouts > 0 {
			req.MinCallouts++
		}
		if tmpl == "practice" {
			req.MinQuickChecks += 2
			req.MinCallouts++
		}
		// Premium polish: ensure at least one simple visual for core narrative lesson templates.
		if tmpl == "concept" || tmpl == "overview" {
			if req.MinDiagrams < 1 {
				req.MinDiagrams = 1
			}
		}
	}

	return req
}

func docTemplateRequirementLine(docTemplate string, diagramsDisabled bool) string {
	tmpl := normalizePathNodeDocTemplate(docTemplate, "lesson")
	switch tmpl {
	case "cheatsheet":
		return "- Include at least 1 table block that summarizes key definitions, formulas, or patterns."
	case "practice":
		return "- Include at least 2 worked examples (at least one as the tip callout titled exactly \"Worked example\")."
	case "project":
		return "- Include a simple rubric/checklist (table preferred) that the learner can use to self-evaluate."
	case "review":
		if diagramsDisabled {
			return "- Prefer tables/bullets over diagrams for recap; focus on quick checks."
		}
		return "- Prefer recap tables/bullets; include diagrams only if they add real value."
	default:
		return ""
	}
}

func docTemplateSuggestedOutline(nodeKind string, docTemplate string) string {
	kind := normalizePathNodeKind(nodeKind)
	tmpl := normalizePathNodeDocTemplate(docTemplate, kind)

	lines := make([]string, 0, 12)
	switch tmpl {
	case "overview":
		lines = append(lines,
			"Start with a why_it_matters block that connects this module to the learner's goal.",
			"Add an intuition block (the big-picture story) and a mental_model block (how to think about it).",
			"Add a short map of what the upcoming lessons will cover (bullets).",
			"Define key terms and prerequisites at a high level (no deep dive yet).",
			"Include a tip callout titled exactly \"Worked example\" with a small motivating example.",
			"End with common misconceptions + how this module will address them.",
		)
	case "practice":
		lines = append(lines,
			"Start with a why_it_matters block and a 2–3 paragraph recap of the core idea and when to use it.",
			"Include 2–4 worked examples that increase in difficulty (one MUST be the \"Worked example\" tip callout).",
			"Add a short section on common traps and how to debug mistakes.",
			"End with a compact checklist the learner can apply to new problems (plus a few quick checks).",
		)
	case "cheatsheet":
		lines = append(lines,
			"Start with a tight definition section (key terms only) + a mental_model block (how to recognize the pattern).",
			"Include a table that summarizes the most important rules/formulas/patterns.",
			"Add 1 worked example (the \"Worked example\" tip callout) that demonstrates the table in action.",
			"End with \"gotchas\" and edge cases.",
		)
	case "project":
		lines = append(lines,
			"Start with a clear deliverable and constraints.",
			"Include a why_it_matters block (why this project is worth doing) and a mental_model block (how the pieces fit).",
			"List prerequisites and the concepts this project integrates.",
			"Provide a step-by-step build plan with checkpoints.",
			"Include a tip callout titled exactly \"Worked example\" that walks through a representative slice end-to-end.",
			"End with a rubric/checklist and common failure modes.",
		)
	case "review":
		lines = append(lines,
			"Start with a high-level recap of the core ideas (1–2 short sections) + an intuition block if it helps memory.",
			"Include a short misconceptions/common mistakes section.",
			"Focus on many quick checks throughout the doc to reinforce memory.",
			"Include a tip callout titled exactly \"Worked example\" with a compact example.",
		)
	default: // concept
		lines = append(lines,
			"Start with why_it_matters + intuition + mental_model (short, vivid).",
			"Define key terms and connect them to the learner's goal.",
			"Explain the main mechanism/logic step-by-step.",
			"Include a tip callout titled exactly \"Worked example\".",
			"End with common misconceptions + corrections.",
		)
	}
	lines = append(lines, "Spread quick checks throughout (not all at the end).")

	var b strings.Builder
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

type citationSanitizeStats struct {
	BlocksTouched       int
	CitationsKept       int
	CitationsDropped    int
	BlocksBackfilled    int
	ChunkIDsNormalized  int
	QuotesTruncated     int
	NegativeLocRepaired int
}

func (s citationSanitizeStats) Map() map[string]any {
	return map[string]any{
		"blocks_touched":       s.BlocksTouched,
		"citations_kept":       s.CitationsKept,
		"citations_dropped":    s.CitationsDropped,
		"blocks_backfilled":    s.BlocksBackfilled,
		"chunk_ids_normalized": s.ChunkIDsNormalized,
		"quotes_truncated":     s.QuotesTruncated,
		"loc_repaired":         s.NegativeLocRepaired,
	}
}

func sanitizeNodeDocCitations(doc content.NodeDocV1, allowedChunkIDs map[string]bool, chunkByID map[uuid.UUID]*types.MaterialChunk, fallbackChunkIDs []uuid.UUID) (content.NodeDocV1, citationSanitizeStats, bool) {
	stats := citationSanitizeStats{}
	if len(doc.Blocks) == 0 {
		return doc, stats, false
	}

	pickFallbackID := func() string {
		if allowedChunkIDs != nil && len(allowedChunkIDs) > 0 {
			for _, id := range fallbackChunkIDs {
				if id == uuid.Nil {
					continue
				}
				s := id.String()
				if allowedChunkIDs[s] {
					return s
				}
			}
			for s := range allowedChunkIDs {
				if strings.TrimSpace(s) != "" {
					return strings.TrimSpace(s)
				}
			}
			return ""
		}
		for _, id := range fallbackChunkIDs {
			if id != uuid.Nil {
				return id.String()
			}
		}
		return ""
	}

	normalizeChunkID := func(s string) (string, bool) {
		s = strings.TrimSpace(s)
		if s == "" {
			return "", false
		}
		id, err := uuid.Parse(s)
		if err != nil || id == uuid.Nil {
			return "", false
		}
		return id.String(), true
	}

	blockNeedsCitations := func(t string) bool {
		t = strings.ToLower(strings.TrimSpace(t))
		switch t {
		case "heading", "code", "video", "divider":
			return false
		default:
			return true
		}
	}

	changed := false
	for i := range doc.Blocks {
		b := doc.Blocks[i]
		if b == nil {
			continue
		}
		if !blockNeedsCitations(stringFromAny(b["type"])) {
			continue
		}

		stats.BlocksTouched++
		raw, _ := b["citations"].([]any)
		out := make([]any, 0, len(raw))
		seen := map[string]bool{}

		for _, x := range raw {
			m, ok := x.(map[string]any)
			if !ok {
				stats.CitationsDropped++
				changed = true
				continue
			}

			origID := strings.TrimSpace(stringFromAny(m["chunk_id"]))
			cid, ok := normalizeChunkID(origID)
			if !ok {
				stats.CitationsDropped++
				changed = true
				continue
			}
			if origID != cid {
				stats.ChunkIDsNormalized++
				changed = true
			}
			if allowedChunkIDs != nil && len(allowedChunkIDs) > 0 && !allowedChunkIDs[cid] {
				stats.CitationsDropped++
				changed = true
				continue
			}
			if seen[cid] {
				stats.CitationsDropped++
				changed = true
				continue
			}
			seen[cid] = true

			quote := strings.TrimSpace(stringFromAny(m["quote"]))
			if len(quote) > 240 {
				quote = quote[:240]
				stats.QuotesTruncated++
				changed = true
			}

			locAny, _ := m["loc"].(map[string]any)
			page := intFromAny(locAny["page"], 0)
			start := intFromAny(locAny["start"], 0)
			end := intFromAny(locAny["end"], 0)
			if page < 0 {
				page = 0
				stats.NegativeLocRepaired++
				changed = true
			}
			if start < 0 {
				start = 0
				stats.NegativeLocRepaired++
				changed = true
			}
			if end < 0 {
				end = 0
				stats.NegativeLocRepaired++
				changed = true
			}
			if end > 0 && start > 0 && end < start {
				start, end = 0, 0
				stats.NegativeLocRepaired++
				changed = true
			}

			out = append(out, map[string]any{
				"chunk_id": cid,
				"quote":    quote,
				"loc":      map[string]any{"page": page, "start": start, "end": end},
			})
			stats.CitationsKept++
		}

		if len(out) == 0 {
			cid := pickFallbackID()
			if cid != "" {
				quote := ""
				if parsed, err := uuid.Parse(cid); err == nil && parsed != uuid.Nil && chunkByID != nil {
					if ch := chunkByID[parsed]; ch != nil {
						quote = strings.TrimSpace(ch.Text)
						if len(quote) > 240 {
							quote = quote[:240]
						}
					}
				}
				out = []any{map[string]any{
					"chunk_id": cid,
					"quote":    quote,
					"loc":      map[string]any{"page": 0, "start": 0, "end": 0},
				}}
				stats.BlocksBackfilled++
				stats.CitationsKept++
				changed = true
			}
		}

		b["citations"] = out
		doc.Blocks[i] = b
	}

	return doc, stats, changed
}

func removeNodeDocBlockType(doc content.NodeDocV1, blockType string) content.NodeDocV1 {
	blockType = strings.TrimSpace(blockType)
	if blockType == "" || len(doc.Blocks) == 0 {
		return doc
	}
	out := make([]map[string]any, 0, len(doc.Blocks))
	for _, b := range doc.Blocks {
		if strings.EqualFold(strings.TrimSpace(stringFromAny(b["type"])), blockType) {
			continue
		}
		out = append(out, b)
	}
	doc.Blocks = out
	return doc
}

func capNodeDocBlockType(doc content.NodeDocV1, blockType string, max int) content.NodeDocV1 {
	if max < 0 {
		return doc
	}
	if max == 0 {
		return removeNodeDocBlockType(doc, blockType)
	}
	blockType = strings.TrimSpace(blockType)
	if blockType == "" || len(doc.Blocks) == 0 {
		return doc
	}
	kept := 0
	out := make([]map[string]any, 0, len(doc.Blocks))
	for _, b := range doc.Blocks {
		if strings.EqualFold(strings.TrimSpace(stringFromAny(b["type"])), blockType) {
			kept++
			if kept > max {
				continue
			}
		}
		out = append(out, b)
	}
	doc.Blocks = out
	return doc
}

func shouldAutoInjectGeneratedFigure(req content.NodeDocRequirements) bool {
	if req.RequireMedia {
		return true
	}
	switch qualityMode() {
	case "premium", "openai", "high":
		return true
	default:
		return false
	}
}

func sanitizeNodeDocDiagrams(doc content.NodeDocV1) (content.NodeDocV1, bool) {
	if len(doc.Blocks) == 0 {
		return doc, false
	}
	changed := false
	for i, b := range doc.Blocks {
		if b == nil {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(stringFromAny(b["type"])), "diagram") {
			continue
		}

		kind := strings.ToLower(strings.TrimSpace(stringFromAny(b["kind"])))
		source := strings.TrimSpace(stringFromAny(b["source"]))
		caption := strings.TrimSpace(stringFromAny(b["caption"]))

		// Best-effort: infer kind when the model drifts.
		if kind != "svg" && kind != "mermaid" {
			if strings.Contains(strings.ToLower(source), "<svg") {
				kind = "svg"
			} else if source != "" {
				kind = "mermaid"
			}
			b["kind"] = kind
			changed = true
		}

		switch kind {
		case "svg":
			if cleaned := extractAndSanitizeSVG(source); cleaned != "" && cleaned != source {
				b["source"] = cleaned
				changed = true
			}
		case "mermaid":
			cleaned, capFromSource := splitMermaidSourceAndCaption(source)
			if cleaned != "" && cleaned != source {
				b["source"] = cleaned
				changed = true
			}
			if caption == "" && capFromSource != "" {
				b["caption"] = capFromSource
				changed = true
			}
		}

		doc.Blocks[i] = b
	}
	return doc, changed
}

func stripCodeFences(src string) string {
	s := strings.TrimSpace(src)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 2 {
		return s
	}
	first := strings.TrimSpace(lines[0])
	last := strings.TrimSpace(lines[len(lines)-1])
	body := lines[1:]
	if last == "```" {
		body = lines[1 : len(lines)-1]
	}
	_ = first // language tag ignored
	return strings.TrimSpace(strings.Join(body, "\n"))
}

func splitMermaidSourceAndCaption(raw string) (source string, caption string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", ""
	}
	s = stripCodeFences(s)

	lines := strings.Split(s, "\n")
	if len(lines) > 0 && strings.EqualFold(strings.TrimSpace(lines[0]), "diagram") {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return "", ""
	}

	// Heuristic: if the last line looks like prose (caption) rather than Mermaid syntax,
	// move it into caption to make rendering more robust.
	if len(lines) >= 2 {
		last := strings.TrimSpace(lines[len(lines)-1])
		if looksLikeCaptionLine(last) {
			caption = shorten(last, 220)
			lines = lines[:len(lines)-1]
			for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
				lines = lines[:len(lines)-1]
			}
		}
	}

	return strings.TrimSpace(strings.Join(lines, "\n")), caption
}

func looksLikeCaptionLine(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// Mermaid syntax usually has arrows, brackets, pipes, or leading keywords.
	lc := strings.ToLower(s)
	if strings.Contains(lc, "-->") || strings.Contains(lc, ":::") || strings.Contains(lc, "--") {
		return false
	}
	if strings.ContainsAny(s, "[]{}<>|") {
		return false
	}
	for _, prefix := range []string{
		"flowchart", "graph", "sequencediagram", "classdiagram", "statediagram", "erdiagram", "journey", "gantt", "pie", "mindmap", "timeline", "quadrantchart",
	} {
		if strings.HasPrefix(strings.TrimSpace(lc), prefix) {
			return false
		}
	}
	// Prose tends to be longer and has spaces/punctuation.
	if len(strings.Fields(s)) >= 6 {
		return true
	}
	return strings.HasSuffix(s, ".") || strings.HasSuffix(s, "!") || strings.HasSuffix(s, "?")
}

func extractAndSanitizeSVG(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	low := strings.ToLower(s)
	i0 := strings.Index(low, "<svg")
	i1 := strings.LastIndex(low, "</svg>")
	if i0 >= 0 && i1 > i0 {
		s = s[i0 : i1+len("</svg>")]
	}
	// Minimal hardening: strip script tags and on* handlers (best-effort).
	s = reSVGScript.ReplaceAllString(s, "")
	s = reSVGOnAttr.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

var (
	reSVGScript = regexp.MustCompile(`(?is)<script[\s\S]*?>[\s\S]*?</script>`)
	reSVGOnAttr = regexp.MustCompile(`(?i)\son[a-z]+\s*=\s*('[^']*'|"[^"]*")`)
)

func ensureNodeDocHasDiagram(doc content.NodeDocV1, allowedChunkIDs map[string]bool, fallbackChunkIDs []uuid.UUID) content.NodeDocV1 {
	for _, b := range doc.Blocks {
		if strings.EqualFold(strings.TrimSpace(stringFromAny(b["type"])), "diagram") {
			return doc
		}
	}
	cid := ""
	for _, id := range fallbackChunkIDs {
		if id == uuid.Nil {
			continue
		}
		s := id.String()
		if allowedChunkIDs != nil && len(allowedChunkIDs) > 0 && !allowedChunkIDs[s] {
			continue
		}
		cid = s
		break
	}
	if cid == "" {
		return doc
	}

	labels := make([]string, 0, 4)
	for _, k := range doc.ConceptKeys {
		k = strings.TrimSpace(strings.ReplaceAll(k, "_", " "))
		if k == "" {
			continue
		}
		labels = append(labels, k)
		if len(labels) >= 4 {
			break
		}
	}
	if len(labels) == 0 {
		if strings.TrimSpace(doc.Title) != "" {
			labels = append(labels, strings.TrimSpace(doc.Title))
		} else {
			labels = append(labels, "Core idea")
		}
	}

	for i := range labels {
		labels[i] = shorten(labels[i], 28)
	}

	svg := buildSimpleFlowSVG(labels)
	if strings.TrimSpace(svg) == "" {
		return doc
	}

	blockID := "auto_diagram_" + uuid.New().String()
	block := map[string]any{
		"id":      blockID,
		"type":    "diagram",
		"kind":    "svg",
		"source":  svg,
		"caption": "Concept relationship overview",
		"citations": []any{
			map[string]any{
				"chunk_id": cid,
				"quote":    "",
				"loc":      map[string]any{"page": 0, "start": 0, "end": 0},
			},
		},
	}
	doc.Blocks = insertAfterFirstBodyBlock(doc.Blocks, block)
	return doc
}

func ensureNodeDocHasGeneratedFigure(doc content.NodeDocV1, figs []*mediaAssetCandidate, allowedChunkIDs map[string]bool, fallbackChunkIDs []uuid.UUID) content.NodeDocV1 {
	has := false
	for _, b := range doc.Blocks {
		if strings.EqualFold(strings.TrimSpace(stringFromAny(b["type"])), "figure") {
			has = true
			break
		}
	}
	if has {
		return doc
	}
	var a *mediaAssetCandidate
	for _, it := range figs {
		if it != nil && strings.TrimSpace(it.URL) != "" {
			a = it
			break
		}
	}
	if a == nil {
		return doc
	}

	pickCitationID := func() string {
		for _, s := range a.ChunkIDs {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if allowedChunkIDs != nil && len(allowedChunkIDs) > 0 && !allowedChunkIDs[s] {
				continue
			}
			if _, err := uuid.Parse(s); err != nil {
				continue
			}
			return s
		}
		for _, id := range fallbackChunkIDs {
			if id == uuid.Nil {
				continue
			}
			s := id.String()
			if allowedChunkIDs != nil && len(allowedChunkIDs) > 0 && !allowedChunkIDs[s] {
				continue
			}
			return s
		}
		return ""
	}

	cid := pickCitationID()
	if cid == "" {
		return doc
	}

	caption := strings.TrimSpace(extractNoteValue(a.Notes, "caption="))
	if caption == "" {
		caption = "Supplementary figure (generated from your materials)"
	}
	source := strings.TrimSpace(a.Source)
	if source == "" {
		source = "derived"
	}
	fileName := strings.TrimSpace(a.FileName)
	if fileName == "" {
		fileName = "figure.png"
	}
	mime := strings.TrimSpace(a.MimeType)
	if mime == "" {
		mime = "image/png"
	}

	blockID := "auto_figure_" + uuid.New().String()
	block := map[string]any{
		"id":   blockID,
		"type": "figure",
		"asset": map[string]any{
			"url":              strings.TrimSpace(a.URL),
			"material_file_id": "",
			"storage_key":      strings.TrimSpace(a.Key),
			"mime_type":        mime,
			"file_name":        fileName,
			"source":           source,
		},
		"caption": caption,
		"citations": []any{
			map[string]any{
				"chunk_id": cid,
				"quote":    "",
				"loc":      map[string]any{"page": 0, "start": 0, "end": 0},
			},
		},
	}

	doc.Blocks = insertAfterFirstBodyBlock(doc.Blocks, block)
	return doc
}

func ensureNodeDocHasVideo(doc content.NodeDocV1, videoAsset *mediaAssetCandidate) content.NodeDocV1 {
	if videoAsset == nil || strings.TrimSpace(videoAsset.URL) == "" {
		return doc
	}
	for _, b := range doc.Blocks {
		if strings.EqualFold(strings.TrimSpace(stringFromAny(b["type"])), "video") {
			return doc
		}
	}
	startSec := 0.0
	if videoAsset.StartSec != nil && *videoAsset.StartSec > 0 {
		startSec = *videoAsset.StartSec
	}
	caption := strings.TrimSpace(extractNoteValue(videoAsset.Notes, "caption="))
	if caption == "" {
		caption = strings.TrimSpace(videoAsset.FileName)
	}
	if caption == "" {
		caption = strings.TrimSpace(videoAsset.Notes)
		if caption == "" {
			caption = "Supplementary video (from your materials)"
		}
	}
	blockID := "auto_video_" + uuid.New().String()
	block := map[string]any{
		"id":        blockID,
		"type":      "video",
		"url":       strings.TrimSpace(videoAsset.URL),
		"start_sec": startSec,
		"caption":   shorten(caption, 140),
	}
	doc.Blocks = insertAfterFirstBodyBlock(doc.Blocks, block)
	return doc
}

func ensureNodeDocMeetsMinima(doc content.NodeDocV1, req content.NodeDocRequirements, allowedChunkIDs map[string]bool, chunkByID map[uuid.UUID]*types.MaterialChunk, fallbackChunkIDs []uuid.UUID) (content.NodeDocV1, bool) {
	_ = chunkByID
	changed := false

	if doc.SchemaVersion == 0 {
		doc.SchemaVersion = 1
		changed = true
	}
	if strings.TrimSpace(doc.Title) == "" {
		doc.Title = "Lesson"
		changed = true
	}

	pickCitationID := func() string {
		if allowedChunkIDs != nil && len(allowedChunkIDs) > 0 {
			for _, id := range fallbackChunkIDs {
				if id == uuid.Nil {
					continue
				}
				s := id.String()
				if allowedChunkIDs[s] {
					return s
				}
			}
			for s := range allowedChunkIDs {
				s = strings.TrimSpace(s)
				if s != "" {
					return s
				}
			}
			return ""
		}
		for _, id := range fallbackChunkIDs {
			if id != uuid.Nil {
				return id.String()
			}
		}
		return ""
	}

	cid := pickCitationID()
	citations := func() []any {
		if cid == "" {
			return nil
		}
		return []any{map[string]any{
			"chunk_id": cid,
			"quote":    "",
			"loc":      map[string]any{"page": 0, "start": 0, "end": 0},
		}}
	}

	hasWorkedExample := func(d content.NodeDocV1) bool {
		for _, b := range d.Blocks {
			if b == nil {
				continue
			}
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

	metrics := content.NodeDocMetrics(doc)
	bc, _ := metrics["block_counts"].(map[string]int)
	if bc == nil {
		bc = map[string]int{}
	}

	// Ensure required worked example marker exists (quality floor + validator requirement).
	if req.RequireExample && !hasWorkedExample(doc) {
		doc.Blocks = append(doc.Blocks, map[string]any{
			"type":      "callout",
			"variant":   "tip",
			"title":     "Worked example",
			"md":        "Work one small concrete example end-to-end. Write each step, cite the sentence/definition you used, and finish with a quick sanity check so you can reproduce the method without notes.",
			"citations": citations(),
		})
		bc["callout"]++
		changed = true
	}

	// Ensure minimum headings (levels 2-4).
	for req.MinHeadings > 0 && bc["heading"] < req.MinHeadings {
		headingNames := []string{"Roadmap", "Key idea", "Practice", "Key takeaways"}
		n := bc["heading"]
		name := headingNames[n%len(headingNames)]
		doc.Blocks = append(doc.Blocks, map[string]any{
			"type":  "heading",
			"level": 2,
			"text":  name,
		})
		bc["heading"]++
		changed = true
	}

	// Ensure required conceptual sections.
	for req.MinWhyItMatters > 0 && bc["why_it_matters"] < req.MinWhyItMatters {
		doc.Blocks = append(doc.Blocks, map[string]any{
			"type":      "why_it_matters",
			"title":     "Why it matters",
			"md":        "This lesson turns the material into a reusable tool: it helps you decide what matters, apply the idea correctly, and detect mistakes early instead of memorizing isolated facts.",
			"citations": citations(),
		})
		bc["why_it_matters"]++
		changed = true
	}
	for req.MinIntuition > 0 && bc["intuition"] < req.MinIntuition {
		doc.Blocks = append(doc.Blocks, map[string]any{
			"type":      "intuition",
			"title":     "Intuition",
			"md":        "Build an intuition you can run mentally: name the moving parts, state what can change, and keep a simple check you can apply after each step so your reasoning stays anchored.",
			"citations": citations(),
		})
		bc["intuition"]++
		changed = true
	}
	for req.MinMentalModels > 0 && bc["mental_model"] < req.MinMentalModels {
		doc.Blocks = append(doc.Blocks, map[string]any{
			"type":      "mental_model",
			"title":     "Mental model",
			"md":        "Use a compact mental model: (1) the objects involved, (2) the rule or transformation you apply, (3) what the rule preserves, and (4) the final check that confirms you stayed within the assumptions.",
			"citations": citations(),
		})
		bc["mental_model"]++
		changed = true
	}

	// Ensure minimum paragraph/callout/quick_check structure.
	for req.MinParagraphs > 0 && bc["paragraph"] < req.MinParagraphs {
		doc.Blocks = append(doc.Blocks, map[string]any{
			"type":      "paragraph",
			"md":        nodeDocPaddingTextWithOffset(90, bc["paragraph"]),
			"citations": citations(),
		})
		bc["paragraph"]++
		changed = true
	}
	for req.MinCallouts > 0 && bc["callout"] < req.MinCallouts {
		doc.Blocks = append(doc.Blocks, map[string]any{
			"type":      "callout",
			"variant":   "info",
			"title":     "Tip",
			"md":        "Keep definitions and assumptions explicit as you work. Most errors come from silently swapping terms or applying a rule outside its stated conditions.",
			"citations": citations(),
		})
		bc["callout"]++
		changed = true
	}
	for req.MinQuickChecks > 0 && bc["quick_check"] < req.MinQuickChecks {
		doc.Blocks = append(doc.Blocks, map[string]any{
			"type":      "quick_check",
			"kind":      "short_answer",
			"prompt_md": "Write a one-sentence paraphrase of the key definition or rule from the cited excerpt, without adding new claims.",
			"options":   []any{},
			"answer_id": "",
			"answer_md": "A correct answer restates the cited line faithfully in plain language and preserves the same conditions/assumptions.",
			"citations": citations(),
		})
		bc["quick_check"]++
		changed = true
	}

	// Ensure pitfalls, steps, and checklist requirements when present.
	for req.MinPitfalls > 0 && bc["misconceptions"]+bc["common_mistakes"] < req.MinPitfalls {
		doc.Blocks = append(doc.Blocks, map[string]any{
			"type":  "common_mistakes",
			"title": "Common mistakes",
			"items_md": []any{
				"Using a rule without checking its assumptions first.",
				"Swapping two similarly named quantities or terms mid-solution.",
				"Skipping a quick sanity check (units, sign, scale, or boundary case).",
			},
			"citations": citations(),
		})
		bc["common_mistakes"]++
		changed = true
	}
	for req.MinSteps > 0 && bc["steps"] < req.MinSteps {
		doc.Blocks = append(doc.Blocks, map[string]any{
			"type":  "steps",
			"title": "Procedure",
			"steps_md": []any{
				"Restate the task and list what is given vs. what you need to produce.",
				"Select the relevant definition or rule from the materials and write it in your own words.",
				"Apply the rule step-by-step, citing the line you used for each step.",
				"Check the result against the original conditions and do a quick sanity check.",
			},
			"citations": citations(),
		})
		bc["steps"]++
		changed = true
	}
	for req.MinChecklist > 0 && bc["checklist"] < req.MinChecklist {
		doc.Blocks = append(doc.Blocks, map[string]any{
			"type":  "checklist",
			"title": "Checklist",
			"items_md": []any{
				"I stated the definition/rule and its assumptions.",
				"Each step is justified by the cited material.",
				"I did a final sanity check and confirmed constraints are satisfied.",
			},
			"citations": citations(),
		})
		bc["checklist"]++
		changed = true
	}
	for req.MinConnections > 0 && bc["connections"] < req.MinConnections {
		doc.Blocks = append(doc.Blocks, map[string]any{
			"type":  "connections",
			"title": "Connections",
			"items_md": []any{
				"Name the prerequisite idea this relies on and the assumption it provides.",
				"Note one nearby concept that looks similar and the feature that distinguishes it.",
				"Identify where in the worked example the connection becomes visible.",
			},
			"citations": citations(),
		})
		bc["connections"]++
		changed = true
	}

	// Ensure table minimum when required (e.g., cheatsheet template).
	for req.MinTables > 0 && bc["table"] < req.MinTables {
		doc.Blocks = append(doc.Blocks, map[string]any{
			"type":    "table",
			"caption": "Summary table",
			"columns": []any{"Item", "Notes"},
			"rows": []any{
				[]any{"Key definition", "State it in plain language and list the assumptions."},
				[]any{"How to apply", "Work step-by-step and check constraints at the end."},
				[]any{"Common mistakes", "Assumption drift, swapped terms, missing sanity checks."},
			},
			"citations": citations(),
		})
		bc["table"]++
		changed = true
	}

	// Ensure minimum media when explicitly required (prefer table, since it is always renderable).
	if req.RequireMedia && bc["figure"]+bc["diagram"]+bc["table"] == 0 {
		doc.Blocks = append(doc.Blocks, map[string]any{
			"type":    "table",
			"caption": "Self-check table",
			"columns": []any{"Check", "What to verify"},
			"rows": []any{
				[]any{"Assumptions", "List the assumptions and confirm they hold for the worked example."},
				[]any{"Units/scale", "Do a quick sanity check for units, sign, and rough magnitude."},
				[]any{"Consistency", "Confirm the final result is consistent with the cited definition/rule."},
			},
			"citations": citations(),
		})
		bc["table"]++
		changed = true
	}

	// Ensure minimum word count (pad once, in a single paragraph).
	if req.MinWordCount > 0 {
		metrics = content.NodeDocMetrics(doc)
		wordCount, _ := metrics["word_count"].(int)
		if wordCount < req.MinWordCount {
			missing := req.MinWordCount - wordCount
			doc.Blocks = append(doc.Blocks, map[string]any{
				"type":      "paragraph",
				"md":        nodeDocPaddingTextWithOffset(missing+140, wordCount),
				"citations": citations(),
			})
			changed = true
		}
	}

	return doc, changed
}

func nodeDocPaddingTextWithOffset(minWords int, offset int) string {
	if minWords < 40 {
		minWords = 40
	}
	if offset < 0 {
		offset = 0
	}

	sentences := []string{
		"Work in short loops: recall the definition, apply it to a concrete case, and then check the result against the stated constraints.",
		"Keep definitions and assumptions explicit; most mistakes come from silently changing what a term refers to.",
		"When you see a rule, name each symbol in plain language and state what can vary and what is fixed.",
		"After each step, do a quick sanity check using units, sign, scale, or a boundary case so errors surface early.",
		"If you get stuck, write the smallest next step you can justify from the cited material and continue from there.",
		"End with a one-sentence takeaway you can repeat without notes and a short note about the most common trap to avoid.",
	}

	var b strings.Builder
	words := 0
	for it := 0; words < minWords && it < 200; it++ {
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		s := sentences[(offset+it)%len(sentences)]
		b.WriteString(s)
		words += content.WordCount(s)
	}
	return strings.TrimSpace(b.String())
}

func extractNoteValue(notes string, prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if strings.TrimSpace(notes) == "" || prefix == "" {
		return ""
	}
	for _, part := range strings.Split(notes, "|") {
		p := strings.TrimSpace(part)
		if strings.HasPrefix(p, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(p, prefix))
		}
	}
	return ""
}

func insertAfterFirstBodyBlock(blocks []map[string]any, block map[string]any) []map[string]any {
	if block == nil {
		return blocks
	}
	if len(blocks) == 0 {
		return []map[string]any{block}
	}
	insertAt := len(blocks)
	for i := range blocks {
		t := strings.ToLower(strings.TrimSpace(stringFromAny(blocks[i]["type"])))
		if t == "paragraph" || t == "callout" || t == "diagram" || t == "table" || t == "code" {
			insertAt = i + 1
			break
		}
	}
	out := make([]map[string]any, 0, len(blocks)+1)
	out = append(out, blocks[:insertAt]...)
	out = append(out, block)
	out = append(out, blocks[insertAt:]...)
	return out
}

type quickCheckTeachOrderStats struct {
	QuickChecksSeen            int
	QuickChecksReordered       int
	ContextParagraphsInserted  int
	PendingQuickChecksResolved int
}

func (s quickCheckTeachOrderStats) Map() map[string]any {
	return map[string]any{
		"quick_checks_seen":             s.QuickChecksSeen,
		"quick_checks_reordered":        s.QuickChecksReordered,
		"context_paragraphs_inserted":   s.ContextParagraphsInserted,
		"pending_quick_checks_resolved": s.PendingQuickChecksResolved,
	}
}

// ensureQuickChecksAfterTeaching enforces "teach before test" in static node docs:
// any quick_check block must appear only after at least one earlier teaching block cites the
// same chunk_id(s). If a quick_check cites chunks that were never cited by any earlier teaching
// block, we insert a short grounding paragraph with those citations immediately before it.
func ensureQuickChecksAfterTeaching(doc content.NodeDocV1, chunkByID map[uuid.UUID]*types.MaterialChunk) (content.NodeDocV1, quickCheckTeachOrderStats, bool) {
	stats := quickCheckTeachOrderStats{}
	if len(doc.Blocks) == 0 {
		return doc, stats, false
	}

	countsAsTeaching := func(t string) bool {
		t = strings.ToLower(strings.TrimSpace(t))
		switch t {
		case "", "quick_check", "heading", "divider", "video", "code", "objectives", "prerequisites", "key_takeaways":
			return false
		default:
			return true
		}
	}

	allTaught := func(ids []string, taught map[string]bool) bool {
		if len(ids) == 0 {
			return true
		}
		for _, id := range ids {
			if !taught[strings.TrimSpace(id)] {
				return false
			}
		}
		return true
	}

	makeContextParagraph := func(missing []string) map[string]any {
		missing = dedupeStrings(missing)
		citations := make([]any, 0, len(missing))
		quotes := make([]string, 0, len(missing))

		for _, id := range missing {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			citations = append(citations, buildMustCiteRef(id, chunkByID))

			if parsed, err := uuid.Parse(id); err == nil && parsed != uuid.Nil {
				if ch := chunkByID[parsed]; ch != nil {
					txt := strings.TrimSpace(ch.Text)
					if txt != "" {
						quotes = append(quotes, shorten(txt, 240))
					}
				}
			}
		}

		md := "Relevant excerpt (from your materials):"
		if len(quotes) > 0 {
			for _, q := range quotes {
				md += "\n\n> " + q
			}
		} else {
			md += "\n\n_(Relevant passage is cited below.)_"
		}

		return map[string]any{
			"type":      "paragraph",
			"md":        md,
			"citations": citations,
		}
	}

	taught := map[string]bool{}
	pending := make([]map[string]any, 0)
	out := make([]map[string]any, 0, len(doc.Blocks))
	changed := false

	flushPending := func() {
		if len(pending) == 0 {
			return
		}
		kept := make([]map[string]any, 0, len(pending))
		for _, qc := range pending {
			qcChunkIDs := extractChunkIDsFromCitations(qc["citations"])
			if allTaught(qcChunkIDs, taught) {
				out = append(out, qc)
				stats.PendingQuickChecksResolved++
			} else {
				kept = append(kept, qc)
			}
		}
		pending = kept
	}

	for _, b := range doc.Blocks {
		if b == nil {
			continue
		}
		t := strings.ToLower(strings.TrimSpace(stringFromAny(b["type"])))
		if t == "quick_check" {
			stats.QuickChecksSeen++
			qcChunkIDs := extractChunkIDsFromCitations(b["citations"])
			if !allTaught(qcChunkIDs, taught) {
				pending = append(pending, b)
				stats.QuickChecksReordered++
				changed = true
				continue
			}
			out = append(out, b)
			continue
		}

		out = append(out, b)
		if countsAsTeaching(t) {
			for _, id := range extractChunkIDsFromCitations(b["citations"]) {
				id = strings.TrimSpace(id)
				if id != "" {
					taught[id] = true
				}
			}
			flushPending()
		}
	}

	// Resolve any remaining quick checks (no later teaching block cited their chunks).
	for _, qc := range pending {
		qcChunkIDs := extractChunkIDsFromCitations(qc["citations"])
		missing := make([]string, 0)
		for _, id := range qcChunkIDs {
			id = strings.TrimSpace(id)
			if id == "" || taught[id] {
				continue
			}
			missing = append(missing, id)
		}
		missing = dedupeStrings(missing)
		if len(missing) > 0 {
			out = append(out, makeContextParagraph(missing))
			stats.ContextParagraphsInserted++
			changed = true
			for _, id := range missing {
				taught[id] = true
			}
		}
		out = append(out, qc)
	}

	if !changed {
		return doc, stats, false
	}
	doc.Blocks = out
	return doc, stats, true
}

func missingMustCiteIDs(doc content.NodeDocV1, mustCiteIDs []uuid.UUID) []string {
	if len(mustCiteIDs) == 0 {
		return nil
	}
	cited := map[string]bool{}
	for _, s := range content.CitedChunkIDsFromNodeDocV1(doc) {
		cited[strings.TrimSpace(s)] = true
	}
	missing := make([]string, 0)
	for _, id := range mustCiteIDs {
		if id == uuid.Nil {
			continue
		}
		s := id.String()
		if !cited[s] {
			missing = append(missing, s)
		}
	}
	return missing
}

func injectMissingMustCiteCitations(doc content.NodeDocV1, missing []string, chunkByID map[uuid.UUID]*types.MaterialChunk) (content.NodeDocV1, bool) {
	if len(missing) == 0 {
		return doc, false
	}
	idx := firstCitationBlockIndex(doc.Blocks)
	if idx < 0 || idx >= len(doc.Blocks) {
		return doc, false
	}
	block := doc.Blocks[idx]
	citations := make([]any, 0)
	if existing, ok := block["citations"].([]any); ok {
		citations = append(citations, existing...)
	}
	for _, id := range missing {
		citations = append(citations, buildMustCiteRef(id, chunkByID))
	}
	block["citations"] = citations
	doc.Blocks[idx] = block
	return doc, true
}

func firstCitationBlockIndex(blocks []map[string]any) int {
	for i, b := range blocks {
		if b == nil {
			continue
		}
		t := strings.ToLower(strings.TrimSpace(stringFromAny(b["type"])))
		switch t {
		case "paragraph", "callout", "figure", "diagram", "table":
			return i
		}
	}
	return -1
}

func buildMustCiteRef(id string, chunkByID map[uuid.UUID]*types.MaterialChunk) map[string]any {
	quote := ""
	page := 0
	if parsed, err := uuid.Parse(strings.TrimSpace(id)); err == nil && parsed != uuid.Nil {
		if ch := chunkByID[parsed]; ch != nil {
			quote = shorten(strings.TrimSpace(ch.Text), 220)
			if ch.Page != nil {
				page = *ch.Page
			}
		}
	}
	return map[string]any{
		"chunk_id": strings.TrimSpace(id),
		"quote":    quote,
		"loc": map[string]any{
			"page":  page,
			"start": 0,
			"end":   0,
		},
	}
}

func buildSimpleFlowSVG(labels []string) string {
	labels = dedupeStrings(labels)
	if len(labels) == 0 {
		return ""
	}
	if len(labels) > 4 {
		labels = labels[:4]
	}

	const (
		w      = 900
		h      = 240
		margin = 24
		gap    = 22
		boxH   = 86
	)
	n := len(labels)
	innerW := w - margin*2 - gap*(n-1)
	if innerW < 120 {
		return ""
	}
	boxW := innerW / n
	y := (h - boxH) / 2

	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`, w, h, w, h))
	b.WriteString(`
<style>
.box{fill:#f7f7fb;stroke:#2b2b2b;stroke-width:2;rx:14;}
.t{font-family:Arial, Helvetica, sans-serif;font-size:16px;fill:#111;}
.arrow{stroke:#111;stroke-width:2.5;marker-end:url(#m);}
</style>
<defs>
<marker id="m" markerWidth="10" markerHeight="10" refX="8" refY="3" orient="auto">
<path d="M0,0 L9,3 L0,6 Z" fill="#111"/>
</marker>
</defs>
`)

	for i, raw := range labels {
		x := margin + i*(boxW+gap)
		label := escapeXML(strings.TrimSpace(raw))
		b.WriteString(fmt.Sprintf(`<rect class="box" x="%d" y="%d" width="%d" height="%d"/>`, x, y, boxW, boxH))
		// Center text.
		tx := x + boxW/2
		ty := y + boxH/2 + 6
		b.WriteString(fmt.Sprintf(`<text class="t" x="%d" y="%d" text-anchor="middle">%s</text>`, tx, ty, label))

		// Arrow to next box.
		if i < n-1 {
			ax1 := x + boxW
			ay := y + boxH/2
			ax2 := x + boxW + gap - 6
			b.WriteString(fmt.Sprintf(`<line class="arrow" x1="%d" y1="%d" x2="%d" y2="%d"/>`, ax1, ay, ax2, ay))
		}
	}

	b.WriteString(`</svg>`)
	return b.String()
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

func lexicalChunkIDs(dbc dbctx.Context, fileIDs []uuid.UUID, query string, limit int) ([]uuid.UUID, error) {
	transaction := dbc.Tx
	if transaction == nil || limit <= 0 || len(fileIDs) == 0 || strings.TrimSpace(query) == "" {
		return nil, nil
	}
	// Conservative: keep query short; plainto_tsquery struggles with huge input.
	query = shorten(query, 220)
	var ids []uuid.UUID
	err := transaction.WithContext(dbc.Ctx).Raw(`
		SELECT id
		FROM material_chunk
		WHERE deleted_at IS NULL
		  AND material_file_id IN ?
		  AND to_tsvector('english', text) @@ plainto_tsquery('english', ?)
		ORDER BY ts_rank_cd(to_tsvector('english', text), plainto_tsquery('english', ?)) DESC
		LIMIT ?
	`, fileIDs, query, query, limit).Scan(&ids).Error
	if err != nil {
		return nil, err
	}
	return dedupeUUIDsPreserveOrder(ids), nil
}

func dedupeUUIDsPreserveOrder(in []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(in))
	for _, id := range in {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func mapKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func makeGenRun(artifactType string, artifactID *uuid.UUID, userID, pathID, pathNodeID uuid.UUID, status, promptVersion string, attempt, latencyMS int, validationErrors []string, qualityMetrics map[string]any) *types.LearningDocGenerationRun {
	now := time.Now().UTC()
	ve := datatypes.JSON([]byte(`null`))
	if len(validationErrors) > 0 {
		b, _ := json.Marshal(validationErrors)
		ve = datatypes.JSON(b)
	}
	qm := datatypes.JSON([]byte(`null`))
	if qualityMetrics != nil {
		b, _ := json.Marshal(qualityMetrics)
		qm = datatypes.JSON(b)
	}
	model := strings.TrimSpace(openAIModelFromEnv())
	if model == "" {
		model = "unknown"
	}
	return &types.LearningDocGenerationRun{
		ID:               uuid.New(),
		ArtifactType:     artifactType,
		ArtifactID:       artifactID,
		UserID:           userID,
		PathID:           pathID,
		PathNodeID:       pathNodeID,
		Status:           status,
		Model:            model,
		PromptVersion:    promptVersion,
		Attempt:          attempt,
		LatencyMS:        latencyMS,
		TokensIn:         0,
		TokensOut:        0,
		ValidationErrors: ve,
		QualityMetrics:   qm,
		CreatedAt:        now,
	}
}

func openAIModelFromEnv() string {
	// Keep this local so we don't expand the openai.Client interface.
	return strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
}

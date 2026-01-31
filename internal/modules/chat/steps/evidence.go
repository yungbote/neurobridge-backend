package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
)

type EvidenceSource struct {
	ID    string
	Type  string
	Title string
	Text  string
	Meta  map[string]any
}

type EvidenceCitation struct {
	SourceID   string `json:"source_id"`
	SourceType string `json:"source_type"`
	Title      string `json:"title"`
	Locator    string `json:"locator"`
	Quote      string `json:"quote,omitempty"`
}

type evidenceSelectResult struct {
	SelectedIDs []string `json:"selected_ids"`
	Confidence  float64  `json:"confidence"`
	Reason      string   `json:"reason"`
}

func resolveEvidenceSelectModel() string {
	model := strings.TrimSpace(os.Getenv("CHAT_EVIDENCE_SELECT_MODEL"))
	if model == "" {
		model = resolveContextRouteModel()
	}
	return strings.TrimSpace(model)
}

func resolveEvidenceAnswerModel() string {
	return strings.TrimSpace(os.Getenv("CHAT_EVIDENCE_ANSWER_MODEL"))
}

func evidenceFromChatDoc(d *types.ChatDoc) *EvidenceSource {
	if d == nil || d.ID == uuid.Nil {
		return nil
	}
	text := strings.TrimSpace(d.Text)
	if text == "" {
		text = strings.TrimSpace(d.ContextualText)
	}
	if text == "" {
		return nil
	}
	id := "doc:" + d.ID.String()
	typeLabel := strings.TrimSpace(d.DocType)
	title := ""
	locator := ""
	if typeLabel == DocTypePathUnitBlock {
		if bid := parseBlockIDFromText(text); bid != "" {
			locator = "block:" + bid
		}
		if t := parseTitleFromText(text); t != "" {
			title = t
		}
	}
	if title == "" {
		title = typeLabel
	}
	meta := map[string]any{}
	if locator != "" {
		meta["locator"] = locator
	}
	if d.SourceID != nil && *d.SourceID != uuid.Nil {
		meta["source_id"] = d.SourceID.String()
	}
	return &EvidenceSource{
		ID:    id,
		Type:  typeLabel,
		Title: title,
		Text:  text,
		Meta:  meta,
	}
}

func parseTitleFromText(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "title:") {
			return strings.TrimSpace(trimmed[len("title:"):])
		}
	}
	return ""
}

func renderEvidenceSources(sources []EvidenceSource, maxTokens int) string {
	if len(sources) == 0 {
		return ""
	}
	var b strings.Builder
	used := 0
	for _, s := range sources {
		if strings.TrimSpace(s.ID) == "" || strings.TrimSpace(s.Text) == "" {
			continue
		}
		header := "[source_id=" + s.ID + "]"
		if s.Type != "" {
			header += " (type=" + s.Type + ")"
		}
		if s.Title != "" {
			header += " " + s.Title
		}
		if loc := stringFromAnyCtx(s.Meta["locator"]); loc != "" {
			header += " — " + loc
		}
		header += "\n"
		body := strings.TrimSpace(s.Text)
		block := header + body + "\n\n"
		blockTokens := estimateTokens(block)
		if maxTokens > 0 && used+blockTokens > maxTokens {
			remain := maxTokens - used - estimateTokens(header) - 6
			if remain <= 0 {
				break
			}
			body = trimToTokens(body, remain)
			if strings.TrimSpace(body) == "" {
				break
			}
			block = header + body + "\n\n"
			blockTokens = estimateTokens(block)
			if used+blockTokens > maxTokens {
				break
			}
		}
		b.WriteString(block)
		used += blockTokens
	}
	return strings.TrimSpace(b.String())
}

func evidenceSelectionCandidates(sources []EvidenceSource, maxCandidates int) []EvidenceSource {
	if len(sources) == 0 {
		return nil
	}
	sorted := make([]EvidenceSource, 0, len(sources))
	sorted = append(sorted, sources...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return evidenceTypeRank(sorted[i].Type) < evidenceTypeRank(sorted[j].Type)
	})
	if maxCandidates > 0 && len(sorted) > maxCandidates {
		sorted = sorted[:maxCandidates]
	}
	return sorted
}

func evidenceTypeRank(t string) int {
	switch strings.TrimSpace(strings.ToLower(t)) {
	case DocTypePathUnitBlock:
		return 1
	case "lesson_index":
		return 2
	case DocTypePathOverview:
		return 3
	case DocTypePathConcepts:
		return 4
	case "material_chunk":
		return 5
	case DocTypePathMaterials:
		return 6
	case DocTypeMessageChunk, DocTypeMemory, DocTypeSummary:
		return 7
	default:
		return 8
	}
}

func filterQuoteSources(sources []EvidenceSource, preference string) []EvidenceSource {
	if len(sources) == 0 {
		return nil
	}
	pref := strings.ToLower(strings.TrimSpace(preference))
	out := make([]EvidenceSource, 0, len(sources))
	for _, s := range sources {
		typ := strings.ToLower(strings.TrimSpace(s.Type))
		switch pref {
		case "materials":
			if typ == "material_chunk" {
				out = append(out, s)
			}
		default:
			if typ == "material_chunk" || typ == strings.ToLower(DocTypePathUnitBlock) {
				out = append(out, s)
			}
		}
	}
	return out
}

func selectEvidenceSources(ctx context.Context, ai openai.Client, query string, sources []EvidenceSource) ([]EvidenceSource, map[string]any) {
	trace := map[string]any{}
	if ai == nil || strings.TrimSpace(query) == "" || len(sources) == 0 {
		return nil, trace
	}
	cands := evidenceSelectionCandidates(sources, 32)
	if len(cands) == 0 {
		return nil, trace
	}

	var sb strings.Builder
	for _, s := range cands {
		if s.ID == "" || s.Text == "" {
			continue
		}
		excerpt := trimToChars(strings.TrimSpace(s.Text), 420)
		line := fmt.Sprintf("- id: %s\n  type: %s\n  title: %s\n  locator: %s\n  excerpt: %s\n", s.ID, s.Type, s.Title, stringFromAnyCtx(s.Meta["locator"]), excerpt)
		sb.WriteString(line)
	}
	candidatesText := strings.TrimSpace(sb.String())
	if candidatesText == "" {
		return nil, trace
	}

	system := strings.TrimSpace(strings.Join([]string{
		"You select the minimum evidence sources needed to answer a user question.",
		"Return only JSON matching the schema.",
		"Select the smallest set that fully supports the answer.",
		"If unsure, prefer unit blocks and materials.",
		"Summary sources are paraphrases; avoid them when verbatim quotes are requested.",
	}, "\n"))

	user := strings.TrimSpace(strings.Join([]string{
		"QUESTION:",
		strings.TrimSpace(query),
		"",
		"CANDIDATES:",
		candidatesText,
	}, "\n"))

	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"selected_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"confidence":  map[string]any{"type": "number", "minimum": 0, "maximum": 1},
			"reason":      map[string]any{"type": "string"},
		},
		"required": []any{"selected_ids", "confidence", "reason"},
	}

	model := resolveEvidenceSelectModel()
	client := ai
	if model != "" {
		client = openai.WithModel(ai, model)
		trace["model"] = model
	}

	start := time.Now()
	selectCtx, cancel := context.WithTimeout(ctx, resolveContextRouteTimeout())
	obj, err := client.GenerateJSON(selectCtx, system, user, "chat_evidence_select_v1", schema)
	cancel()
	trace["ms"] = time.Since(start).Milliseconds()
	if err != nil {
		trace["error"] = err.Error()
		return fallbackEvidenceSelection(sources), trace
	}
	b, _ := json.Marshal(obj)
	var dec evidenceSelectResult
	_ = json.Unmarshal(b, &dec)
	trace["confidence"] = dec.Confidence
	trace["reason"] = strings.TrimSpace(dec.Reason)

	selected := map[string]bool{}
	for _, id := range dec.SelectedIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			selected[id] = true
		}
	}
	if len(selected) == 0 {
		return fallbackEvidenceSelection(sources), trace
	}

	out := make([]EvidenceSource, 0, len(selected))
	for _, s := range sources {
		if selected[s.ID] {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return fallbackEvidenceSelection(sources), trace
	}
	return out, trace
}

func fallbackEvidenceSelection(sources []EvidenceSource) []EvidenceSource {
	if len(sources) == 0 {
		return nil
	}
	cands := evidenceSelectionCandidates(sources, 8)
	out := make([]EvidenceSource, 0, len(cands))
	seen := map[string]bool{}
	for _, s := range cands {
		if s.ID == "" || seen[s.ID] {
			continue
		}
		seen[s.ID] = true
		out = append(out, s)
		if len(out) >= 6 {
			break
		}
	}
	if len(out) == 0 {
		return cands
	}
	return out
}

func parseCitationMarkers(text string) ([]string, string) {
	if strings.TrimSpace(text) == "" {
		return nil, text
	}
	re := regexp.MustCompile(`\[\[source:([^\]]+)\]\]`)
	ids := []string{}
	_ = re.ReplaceAllStringFunc(text, func(m string) string {
		match := re.FindStringSubmatch(m)
		if len(match) == 2 {
			id := strings.TrimSpace(match[1])
			if id != "" {
				ids = append(ids, id)
			}
		}
		return m
	})
	return ids, strings.TrimSpace(text)
}

func stripCitationMarkers(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	re := regexp.MustCompile(`\[\[source:([^\]]+)\]\]`)
	return strings.TrimSpace(re.ReplaceAllString(text, ""))
}

func buildCitations(ids []string, sources []EvidenceSource) []EvidenceCitation {
	if len(ids) == 0 || len(sources) == 0 {
		return nil
	}
	srcByID := map[string]EvidenceSource{}
	for _, s := range sources {
		srcByID[s.ID] = s
	}
	out := make([]EvidenceCitation, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		s, ok := srcByID[id]
		if !ok {
			continue
		}
		loc := stringFromAnyCtx(s.Meta["locator"])
		out = append(out, EvidenceCitation{
			SourceID:   s.ID,
			SourceType: s.Type,
			Title:      s.Title,
			Locator:    loc,
		})
	}
	return out
}

func applyCitationReplacements(text string, citations []EvidenceCitation, sources []EvidenceSource) string {
	if strings.TrimSpace(text) == "" || len(citations) == 0 {
		return text
	}
	srcByID := map[string]EvidenceSource{}
	for _, s := range sources {
		srcByID[s.ID] = s
	}
	for _, c := range citations {
		label := citationLabel(c, srcByID[c.SourceID])
		if label == "" {
			continue
		}
		text = strings.ReplaceAll(text, "[[source:"+c.SourceID+"]]", " (Source: "+label+")")
	}
	return text
}

func citationLabel(c EvidenceCitation, src EvidenceSource) string {
	label := ""
	if src.Type == "material_chunk" {
		file := stringFromAnyCtx(src.Meta["file_name"])
		loc := stringFromAnyCtx(src.Meta["locator"])
		if file != "" {
			label = file
		}
		if loc != "" {
			if label != "" {
				label += ", " + loc
			} else {
				label = loc
			}
		}
		return label
	}
	if c.Title != "" {
		label = c.Title
	}
	if label == "" {
		label = c.SourceType
	}
	return label
}

func extractQuotedStrings(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	re := regexp.MustCompile(`["“”]([^"“”]{6,})["“”]`)
	matches := re.FindAllStringSubmatch(text, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		q := strings.TrimSpace(m[1])
		if q != "" {
			out = append(out, q)
		}
	}
	return out
}

func normalizeForQuoteMatch(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "“", "\"")
	s = strings.ReplaceAll(s, "”", "\"")
	s = strings.ReplaceAll(s, "’", "'")
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

func verifyQuotesInEvidence(quotes []string, sources []EvidenceSource) (bool, []string) {
	if len(quotes) == 0 {
		return true, nil
	}
	fails := []string{}
	if len(sources) == 0 {
		return false, quotes
	}
	srcTexts := make([]string, 0, len(sources))
	for _, s := range sources {
		if strings.TrimSpace(s.Text) != "" {
			srcTexts = append(srcTexts, normalizeForQuoteMatch(s.Text))
		}
	}
	for _, q := range quotes {
		nq := normalizeForQuoteMatch(q)
		if nq == "" {
			continue
		}
		found := false
		for _, st := range srcTexts {
			if strings.Contains(st, nq) {
				found = true
				break
			}
		}
		if !found {
			fails = append(fails, q)
		}
	}
	return len(fails) == 0, fails
}

func repairQuotedAnswer(ctx context.Context, ai openai.Client, answer string, evidenceText string) (string, error) {
	if ai == nil || strings.TrimSpace(answer) == "" || strings.TrimSpace(evidenceText) == "" {
		return answer, nil
	}
	system := strings.TrimSpace(strings.Join([]string{
		"You correct answers so that quoted text matches the evidence exactly.",
		"If exact wording is unavailable, remove the quote and say you don't have the exact wording.",
		"Return only the corrected answer with citation markers ([[source:ID]]).",
	}, "\n"))

	user := strings.TrimSpace(strings.Join([]string{
		"ANSWER:",
		answer,
		"",
		"EVIDENCE:",
		evidenceText,
	}, "\n"))

	client := ai
	if model := resolveEvidenceAnswerModel(); model != "" {
		client = openai.WithModel(ai, model)
	}

	fixCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	text, err := client.GenerateText(fixCtx, system, user)
	cancel()
	if err != nil {
		return answer, err
	}
	return strings.TrimSpace(text), nil
}

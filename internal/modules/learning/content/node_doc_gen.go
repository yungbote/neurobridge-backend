package content

import (
	"encoding/json"
	"strings"
)

func stripMarkdownCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 2 {
		return s
	}
	// Drop first fence line (``` or ```mermaid) and last fence line if present.
	first := strings.TrimSpace(lines[0])
	last := strings.TrimSpace(lines[len(lines)-1])
	if strings.HasPrefix(first, "```") {
		if last == "```" {
			return strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
		}
		return strings.TrimSpace(strings.Join(lines[1:], "\n"))
	}
	return s
}

func looksLikeMermaidCaption(line string) bool {
	l := strings.TrimSpace(line)
	if l == "" {
		return false
	}
	if len(l) < 24 {
		return false
	}
	lo := strings.ToLower(l)
	for _, p := range []string{
		"flowchart", "graph", "sequencediagram", "classdiagram", "statediagram", "erdiagram", "journey", "gantt", "pie", "mindmap", "timeline",
	} {
		if strings.HasPrefix(lo, p) {
			return false
		}
	}
	// Common Mermaid syntax tokens: if present, treat as code not caption.
	if strings.Contains(l, "-->") || strings.Contains(l, "---") || strings.Contains(l, "==>") || strings.Contains(l, ":::") {
		return false
	}
	for _, kw := range []string{"subgraph", "end", "classdef", "class ", "style ", "click ", "linkstyle", "%%"} {
		if strings.Contains(lo, kw) {
			return false
		}
	}
	if strings.ContainsAny(l, "[](){}|") {
		return false
	}
	if !strings.Contains(l, " ") {
		return false
	}
	return true
}

func sanitizeDiagram(d NodeDocGenDiagramV1) NodeDocGenDiagramV1 {
	d.Kind = strings.ToLower(strings.TrimSpace(d.Kind))
	d.Source = strings.TrimSpace(d.Source)
	d.Caption = strings.TrimSpace(d.Caption)

	if d.Kind == "mermaid" {
		d.Source = stripMarkdownCodeFences(d.Source)
		lines := strings.Split(d.Source, "\n")
		if len(lines) > 0 && strings.EqualFold(strings.TrimSpace(lines[0]), "diagram") {
			lines = lines[1:]
		}
		d.Source = strings.TrimSpace(strings.Join(lines, "\n"))

		// Best-effort: if the model appended explanatory prose into the source field,
		// move a trailing line (or paragraph) into caption when caption is empty.
		if d.Caption == "" && d.Source != "" {
			// Prefer a paragraph split first (blank line separator).
			if parts := strings.Split(d.Source, "\n\n"); len(parts) > 1 {
				tail := strings.TrimSpace(parts[len(parts)-1])
				head := strings.TrimSpace(strings.Join(parts[:len(parts)-1], "\n\n"))
				if head != "" && looksLikeMermaidCaption(tail) {
					d.Source = head
					d.Caption = tail
					return d
				}
			}
			// Otherwise try the last line.
			ll := strings.Split(d.Source, "\n")
			if len(ll) > 2 {
				tail := strings.TrimSpace(ll[len(ll)-1])
				head := strings.TrimSpace(strings.Join(ll[:len(ll)-1], "\n"))
				if head != "" && looksLikeMermaidCaption(tail) {
					d.Source = head
					d.Caption = tail
					return d
				}
			}
		}
	}
	return d
}

// NodeDocGenV1 is the compact "generation" shape used with OpenAI structured outputs.
// It is converted into NodeDocV1 (blocks array) for storage and rendering.
type NodeDocGenV1 struct {
	SchemaVersion    int      `json:"schema_version"`
	Title            string   `json:"title"`
	Summary          string   `json:"summary"`
	ConceptKeys      []string `json:"concept_keys"`
	EstimatedMinutes int      `json:"estimated_minutes"`

	Order []NodeDocGenOrderItemV1 `json:"order"`

	Headings    []NodeDocGenHeadingV1    `json:"headings"`
	Paragraphs  []NodeDocGenParagraphV1  `json:"paragraphs"`
	Callouts    []NodeDocGenCalloutV1    `json:"callouts"`
	Codes       []NodeDocGenCodeV1       `json:"codes"`
	Figures     []NodeDocGenFigureV1     `json:"figures"`
	Videos      []NodeDocGenVideoV1      `json:"videos"`
	Diagrams    []NodeDocGenDiagramV1    `json:"diagrams"`
	Tables      []NodeDocGenTableV1      `json:"tables"`
	Equations   []NodeDocGenEquationV1   `json:"equations"`
	QuickChecks []NodeDocGenQuickCheckV1 `json:"quick_checks"`
	Flashcards  []NodeDocGenFlashcardV1  `json:"flashcards"`
	Dividers    []NodeDocGenDividerV1    `json:"dividers"`

	Objectives     []NodeDocGenListBlockV1     `json:"objectives"`
	Prerequisites  []NodeDocGenListBlockV1     `json:"prerequisites"`
	KeyTakeaways   []NodeDocGenListBlockV1     `json:"key_takeaways"`
	Glossary       []NodeDocGenGlossaryBlockV1 `json:"glossary"`
	CommonMistakes []NodeDocGenListBlockV1     `json:"common_mistakes"`
	Misconceptions []NodeDocGenListBlockV1     `json:"misconceptions"`
	EdgeCases      []NodeDocGenListBlockV1     `json:"edge_cases"`
	Heuristics     []NodeDocGenListBlockV1     `json:"heuristics"`
	Steps          []NodeDocGenStepsBlockV1    `json:"steps"`
	Checklist      []NodeDocGenListBlockV1     `json:"checklist"`
	FAQ            []NodeDocGenFAQBlockV1      `json:"faq"`
	Intuition      []NodeDocGenMDSectionV1     `json:"intuition"`
	MentalModel    []NodeDocGenMDSectionV1     `json:"mental_model"`
	WhyItMatters   []NodeDocGenMDSectionV1     `json:"why_it_matters"`
	Connections    []NodeDocGenListBlockV1     `json:"connections"`
}

type NodeDocGenOrderItemV1 struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type NodeDocGenHeadingV1 struct {
	ID    string `json:"id"`
	Level int    `json:"level"`
	Text  string `json:"text"`
}

type NodeDocGenParagraphV1 struct {
	ID        string          `json:"id"`
	MD        string          `json:"md"`
	Citations []CitationRefV1 `json:"citations"`
}

type NodeDocGenCalloutV1 struct {
	ID        string          `json:"id"`
	Variant   string          `json:"variant"`
	Title     string          `json:"title"`
	MD        string          `json:"md"`
	Citations []CitationRefV1 `json:"citations"`
}

type NodeDocGenCodeV1 struct {
	ID       string `json:"id"`
	Language string `json:"language"`
	Filename string `json:"filename"`
	Code     string `json:"code"`
}

type NodeDocGenFigureV1 struct {
	ID        string          `json:"id"`
	Asset     MediaRefV1      `json:"asset"`
	Caption   string          `json:"caption"`
	Citations []CitationRefV1 `json:"citations"`
}

type NodeDocGenVideoV1 struct {
	ID       string  `json:"id"`
	URL      string  `json:"url"`
	StartSec float64 `json:"start_sec"`
	Caption  string  `json:"caption"`
}

type NodeDocGenDiagramV1 struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Source    string          `json:"source"`
	Caption   string          `json:"caption"`
	Citations []CitationRefV1 `json:"citations"`
}

type NodeDocGenTableV1 struct {
	ID        string          `json:"id"`
	Caption   string          `json:"caption"`
	Columns   []string        `json:"columns"`
	Rows      [][]string      `json:"rows"`
	Citations []CitationRefV1 `json:"citations"`
}

type NodeDocGenEquationV1 struct {
	ID        string          `json:"id"`
	Latex     string          `json:"latex"`
	Display   bool            `json:"display"`
	Caption   string          `json:"caption"`
	Citations []CitationRefV1 `json:"citations"`
}

type NodeDocGenQuickCheckV1 struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"` // short_answer|true_false|mcq|""
	PromptMD string `json:"prompt_md"`

	// MCQ support (optional for short_answer/true_false).
	Options  []DrillQuestionOptionV1 `json:"options"`
	AnswerID string                  `json:"answer_id"`

	AnswerMD             string          `json:"answer_md"`
	TriggerAfterBlockIDs []string        `json:"trigger_after_block_ids,omitempty"`
	Citations            []CitationRefV1 `json:"citations"`
}

type NodeDocGenFlashcardV1 struct {
	ID                   string          `json:"id"`
	FrontMD              string          `json:"front_md"`
	BackMD               string          `json:"back_md"`
	ConceptKeys          []string        `json:"concept_keys,omitempty"`
	TriggerAfterBlockIDs []string        `json:"trigger_after_block_ids,omitempty"`
	Citations            []CitationRefV1 `json:"citations"`
}

type NodeDocGenDividerV1 struct {
	ID string `json:"id"`
}

type NodeDocGenListBlockV1 struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	ItemsMD   []string        `json:"items_md"`
	Citations []CitationRefV1 `json:"citations"`
}

type NodeDocGenStepsBlockV1 struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	StepsMD   []string        `json:"steps_md"`
	Citations []CitationRefV1 `json:"citations"`
}

type NodeDocGenGlossaryTermV1 struct {
	Term         string `json:"term"`
	DefinitionMD string `json:"definition_md"`
}

type NodeDocGenGlossaryBlockV1 struct {
	ID        string                     `json:"id"`
	Title     string                     `json:"title"`
	Terms     []NodeDocGenGlossaryTermV1 `json:"terms"`
	Citations []CitationRefV1            `json:"citations"`
}

type NodeDocGenFAQItemV1 struct {
	QuestionMD string `json:"question_md"`
	AnswerMD   string `json:"answer_md"`
}

type NodeDocGenFAQBlockV1 struct {
	ID        string                `json:"id"`
	Title     string                `json:"title"`
	QAs       []NodeDocGenFAQItemV1 `json:"qas"`
	Citations []CitationRefV1       `json:"citations"`
}

type NodeDocGenMDSectionV1 struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	MD        string          `json:"md"`
	Citations []CitationRefV1 `json:"citations"`
}

func ConvertNodeDocGenV1ToV1(gen NodeDocGenV1) (NodeDocV1, []string) {
	errs := make([]string, 0)

	doc := NodeDocV1{
		SchemaVersion:    1,
		Title:            strings.TrimSpace(gen.Title),
		Summary:          strings.TrimSpace(gen.Summary),
		ConceptKeys:      NormalizeConceptKeys(gen.ConceptKeys),
		EstimatedMinutes: gen.EstimatedMinutes,
		Blocks:           make([]map[string]any, 0, len(gen.Order)),
	}

	// Helpers
	toAny := func(v any) any {
		b, _ := json.Marshal(v)
		var out any
		_ = json.Unmarshal(b, &out)
		return out
	}
	cleanTriggerIDs := func(ids []string) []string {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			out = append(out, id)
		}
		return dedupeStrings(out)
	}

	// Build maps for fast lookup by id.
	headings := map[string]NodeDocGenHeadingV1{}
	headingSeq := make([]NodeDocGenHeadingV1, 0, len(gen.Headings))
	paragraphs := map[string]NodeDocGenParagraphV1{}
	paragraphSeq := make([]NodeDocGenParagraphV1, 0, len(gen.Paragraphs))
	callouts := map[string]NodeDocGenCalloutV1{}
	calloutSeq := make([]NodeDocGenCalloutV1, 0, len(gen.Callouts))
	codes := map[string]NodeDocGenCodeV1{}
	codeSeq := make([]NodeDocGenCodeV1, 0, len(gen.Codes))
	figures := map[string]NodeDocGenFigureV1{}
	figureSeq := make([]NodeDocGenFigureV1, 0, len(gen.Figures))
	videos := map[string]NodeDocGenVideoV1{}
	videoSeq := make([]NodeDocGenVideoV1, 0, len(gen.Videos))
	diagrams := map[string]NodeDocGenDiagramV1{}
	diagramSeq := make([]NodeDocGenDiagramV1, 0, len(gen.Diagrams))
	tables := map[string]NodeDocGenTableV1{}
	tableSeq := make([]NodeDocGenTableV1, 0, len(gen.Tables))
	equations := map[string]NodeDocGenEquationV1{}
	equationSeq := make([]NodeDocGenEquationV1, 0, len(gen.Equations))
	qcs := map[string]NodeDocGenQuickCheckV1{}
	qcSeq := make([]NodeDocGenQuickCheckV1, 0, len(gen.QuickChecks))
	fcs := map[string]NodeDocGenFlashcardV1{}
	fcSeq := make([]NodeDocGenFlashcardV1, 0, len(gen.Flashcards))
	divs := map[string]NodeDocGenDividerV1{}
	dividerSeq := make([]NodeDocGenDividerV1, 0, len(gen.Dividers))

	objectives := map[string]NodeDocGenListBlockV1{}
	objectivesSeq := make([]NodeDocGenListBlockV1, 0, len(gen.Objectives))
	prereqs := map[string]NodeDocGenListBlockV1{}
	prereqSeq := make([]NodeDocGenListBlockV1, 0, len(gen.Prerequisites))
	keyTakeaways := map[string]NodeDocGenListBlockV1{}
	keyTakeawaysSeq := make([]NodeDocGenListBlockV1, 0, len(gen.KeyTakeaways))
	glossary := map[string]NodeDocGenGlossaryBlockV1{}
	glossarySeq := make([]NodeDocGenGlossaryBlockV1, 0, len(gen.Glossary))
	commonMistakes := map[string]NodeDocGenListBlockV1{}
	commonMistakesSeq := make([]NodeDocGenListBlockV1, 0, len(gen.CommonMistakes))
	misconceptions := map[string]NodeDocGenListBlockV1{}
	misconceptionsSeq := make([]NodeDocGenListBlockV1, 0, len(gen.Misconceptions))
	edgeCases := map[string]NodeDocGenListBlockV1{}
	edgeCasesSeq := make([]NodeDocGenListBlockV1, 0, len(gen.EdgeCases))
	heuristics := map[string]NodeDocGenListBlockV1{}
	heuristicsSeq := make([]NodeDocGenListBlockV1, 0, len(gen.Heuristics))
	steps := map[string]NodeDocGenStepsBlockV1{}
	stepsSeq := make([]NodeDocGenStepsBlockV1, 0, len(gen.Steps))
	checklists := map[string]NodeDocGenListBlockV1{}
	checklistsSeq := make([]NodeDocGenListBlockV1, 0, len(gen.Checklist))
	faq := map[string]NodeDocGenFAQBlockV1{}
	faqSeq := make([]NodeDocGenFAQBlockV1, 0, len(gen.FAQ))
	intuition := map[string]NodeDocGenMDSectionV1{}
	intuitionSeq := make([]NodeDocGenMDSectionV1, 0, len(gen.Intuition))
	mentalModel := map[string]NodeDocGenMDSectionV1{}
	mentalModelSeq := make([]NodeDocGenMDSectionV1, 0, len(gen.MentalModel))
	whyItMatters := map[string]NodeDocGenMDSectionV1{}
	whyItMattersSeq := make([]NodeDocGenMDSectionV1, 0, len(gen.WhyItMatters))
	connections := map[string]NodeDocGenListBlockV1{}
	connectionsSeq := make([]NodeDocGenListBlockV1, 0, len(gen.Connections))

	addUnique := func(kind string, id string, seen map[string]bool) (string, bool) {
		id = strings.TrimSpace(id)
		if id == "" {
			return "", false
		}
		if seen[id] {
			// Prefer first; drop duplicates.
			return id, false
		}
		seen[id] = true
		return id, true
	}

	{
		seen := map[string]bool{}
		for _, h := range gen.Headings {
			id, ok := addUnique("heading", h.ID, seen)
			if !ok {
				continue
			}
			h.ID = id
			headings[id] = h
			headingSeq = append(headingSeq, h)
		}
	}
	{
		seen := map[string]bool{}
		for _, p := range gen.Paragraphs {
			id, ok := addUnique("paragraph", p.ID, seen)
			if !ok {
				continue
			}
			p.ID = id
			paragraphs[id] = p
			paragraphSeq = append(paragraphSeq, p)
		}
	}
	{
		seen := map[string]bool{}
		for _, c := range gen.Callouts {
			id, ok := addUnique("callout", c.ID, seen)
			if !ok {
				continue
			}
			c.ID = id
			callouts[id] = c
			calloutSeq = append(calloutSeq, c)
		}
	}
	{
		seen := map[string]bool{}
		for _, c := range gen.Codes {
			id, ok := addUnique("code", c.ID, seen)
			if !ok {
				continue
			}
			c.ID = id
			codes[id] = c
			codeSeq = append(codeSeq, c)
		}
	}
	{
		seen := map[string]bool{}
		for _, f := range gen.Figures {
			id, ok := addUnique("figure", f.ID, seen)
			if !ok {
				continue
			}
			f.ID = id
			figures[id] = f
			figureSeq = append(figureSeq, f)
		}
	}
	{
		seen := map[string]bool{}
		for _, v := range gen.Videos {
			id, ok := addUnique("video", v.ID, seen)
			if !ok {
				continue
			}
			v.ID = id
			videos[id] = v
			videoSeq = append(videoSeq, v)
		}
	}
	{
		seen := map[string]bool{}
		for _, d := range gen.Diagrams {
			d = sanitizeDiagram(d)
			id, ok := addUnique("diagram", d.ID, seen)
			if !ok {
				continue
			}
			d.ID = id
			diagrams[id] = d
			diagramSeq = append(diagramSeq, d)
		}
	}
	{
		seen := map[string]bool{}
		for _, t := range gen.Tables {
			id, ok := addUnique("table", t.ID, seen)
			if !ok {
				continue
			}
			t.ID = id
			tables[id] = t
			tableSeq = append(tableSeq, t)
		}
	}
	{
		seen := map[string]bool{}
		for _, e := range gen.Equations {
			id, ok := addUnique("equation", e.ID, seen)
			if !ok {
				continue
			}
			e.ID = id
			equations[id] = e
			equationSeq = append(equationSeq, e)
		}
	}
	{
		seen := map[string]bool{}
		for _, q := range gen.QuickChecks {
			id, ok := addUnique("quick_check", q.ID, seen)
			if !ok {
				continue
			}
			q.ID = id
			qcs[id] = q
			qcSeq = append(qcSeq, q)
		}
	}
	{
		seen := map[string]bool{}
		for _, f := range gen.Flashcards {
			id, ok := addUnique("flashcard", f.ID, seen)
			if !ok {
				continue
			}
			f.ID = id
			fcs[id] = f
			fcSeq = append(fcSeq, f)
		}
	}
	{
		seen := map[string]bool{}
		for _, d := range gen.Dividers {
			id, ok := addUnique("divider", d.ID, seen)
			if !ok {
				continue
			}
			d.ID = id
			divs[id] = d
			dividerSeq = append(dividerSeq, d)
		}
	}

	{
		seen := map[string]bool{}
		for _, b := range gen.Objectives {
			id, ok := addUnique("objectives", b.ID, seen)
			if !ok {
				continue
			}
			b.ID = id
			objectives[id] = b
			objectivesSeq = append(objectivesSeq, b)
		}
	}
	{
		seen := map[string]bool{}
		for _, b := range gen.Prerequisites {
			id, ok := addUnique("prerequisites", b.ID, seen)
			if !ok {
				continue
			}
			b.ID = id
			prereqs[id] = b
			prereqSeq = append(prereqSeq, b)
		}
	}
	{
		seen := map[string]bool{}
		for _, b := range gen.KeyTakeaways {
			id, ok := addUnique("key_takeaways", b.ID, seen)
			if !ok {
				continue
			}
			b.ID = id
			keyTakeaways[id] = b
			keyTakeawaysSeq = append(keyTakeawaysSeq, b)
		}
	}
	{
		seen := map[string]bool{}
		for _, b := range gen.Glossary {
			id, ok := addUnique("glossary", b.ID, seen)
			if !ok {
				continue
			}
			b.ID = id
			glossary[id] = b
			glossarySeq = append(glossarySeq, b)
		}
	}
	{
		seen := map[string]bool{}
		for _, b := range gen.CommonMistakes {
			id, ok := addUnique("common_mistakes", b.ID, seen)
			if !ok {
				continue
			}
			b.ID = id
			commonMistakes[id] = b
			commonMistakesSeq = append(commonMistakesSeq, b)
		}
	}
	{
		seen := map[string]bool{}
		for _, b := range gen.Misconceptions {
			id, ok := addUnique("misconceptions", b.ID, seen)
			if !ok {
				continue
			}
			b.ID = id
			misconceptions[id] = b
			misconceptionsSeq = append(misconceptionsSeq, b)
		}
	}
	{
		seen := map[string]bool{}
		for _, b := range gen.EdgeCases {
			id, ok := addUnique("edge_cases", b.ID, seen)
			if !ok {
				continue
			}
			b.ID = id
			edgeCases[id] = b
			edgeCasesSeq = append(edgeCasesSeq, b)
		}
	}
	{
		seen := map[string]bool{}
		for _, b := range gen.Heuristics {
			id, ok := addUnique("heuristics", b.ID, seen)
			if !ok {
				continue
			}
			b.ID = id
			heuristics[id] = b
			heuristicsSeq = append(heuristicsSeq, b)
		}
	}
	{
		seen := map[string]bool{}
		for _, b := range gen.Steps {
			id, ok := addUnique("steps", b.ID, seen)
			if !ok {
				continue
			}
			b.ID = id
			steps[id] = b
			stepsSeq = append(stepsSeq, b)
		}
	}
	{
		seen := map[string]bool{}
		for _, b := range gen.Checklist {
			id, ok := addUnique("checklist", b.ID, seen)
			if !ok {
				continue
			}
			b.ID = id
			checklists[id] = b
			checklistsSeq = append(checklistsSeq, b)
		}
	}
	{
		seen := map[string]bool{}
		for _, b := range gen.FAQ {
			id, ok := addUnique("faq", b.ID, seen)
			if !ok {
				continue
			}
			b.ID = id
			faq[id] = b
			faqSeq = append(faqSeq, b)
		}
	}
	{
		seen := map[string]bool{}
		for _, b := range gen.Intuition {
			id, ok := addUnique("intuition", b.ID, seen)
			if !ok {
				continue
			}
			b.ID = id
			intuition[id] = b
			intuitionSeq = append(intuitionSeq, b)
		}
	}
	{
		seen := map[string]bool{}
		for _, b := range gen.MentalModel {
			id, ok := addUnique("mental_model", b.ID, seen)
			if !ok {
				continue
			}
			b.ID = id
			mentalModel[id] = b
			mentalModelSeq = append(mentalModelSeq, b)
		}
	}
	{
		seen := map[string]bool{}
		for _, b := range gen.WhyItMatters {
			id, ok := addUnique("why_it_matters", b.ID, seen)
			if !ok {
				continue
			}
			b.ID = id
			whyItMatters[id] = b
			whyItMattersSeq = append(whyItMattersSeq, b)
		}
	}
	{
		seen := map[string]bool{}
		for _, b := range gen.Connections {
			id, ok := addUnique("connections", b.ID, seen)
			if !ok {
				continue
			}
			b.ID = id
			connections[id] = b
			connectionsSeq = append(connectionsSeq, b)
		}
	}

	refHeading := map[string]bool{}
	refParagraph := map[string]bool{}
	refCallout := map[string]bool{}
	refCode := map[string]bool{}
	refFigure := map[string]bool{}
	refVideo := map[string]bool{}
	refDiagram := map[string]bool{}
	refTable := map[string]bool{}
	refEquations := map[string]bool{}
	refQC := map[string]bool{}
	refFlashcard := map[string]bool{}
	refDivider := map[string]bool{}

	refObjectives := map[string]bool{}
	refPrerequisites := map[string]bool{}
	refKeyTakeaways := map[string]bool{}
	refGlossary := map[string]bool{}
	refCommonMistakes := map[string]bool{}
	refMisconceptions := map[string]bool{}
	refEdgeCases := map[string]bool{}
	refHeuristics := map[string]bool{}
	refSteps := map[string]bool{}
	refChecklist := map[string]bool{}
	refFAQ := map[string]bool{}
	refIntuition := map[string]bool{}
	refMentalModel := map[string]bool{}
	refWhyItMatters := map[string]bool{}
	refConnections := map[string]bool{}

	orderSeen := map[string]bool{}
	for _, item := range gen.Order {
		kind := strings.ToLower(strings.TrimSpace(item.Kind))
		id := strings.TrimSpace(item.ID)
		if kind == "" {
			continue
		}
		if id == "" {
			continue
		}
		key := kind + ":" + id
		if orderSeen[key] {
			continue
		}
		orderSeen[key] = true

		switch kind {
		case "heading":
			h, ok := headings[id]
			if !ok {
				continue
			}
			refHeading[id] = true
			level := h.Level
			if level < 2 {
				level = 2
			} else if level > 4 {
				level = 4
			}
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":    id,
				"type":  "heading",
				"level": level,
				"text":  h.Text,
			})
		case "paragraph":
			p, ok := paragraphs[id]
			if !ok {
				continue
			}
			refParagraph[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "paragraph",
				"md":        p.MD,
				"citations": toAny(p.Citations),
			})
		case "callout":
			c, ok := callouts[id]
			if !ok {
				continue
			}
			refCallout[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "callout",
				"variant":   c.Variant,
				"title":     c.Title,
				"md":        c.MD,
				"citations": toAny(c.Citations),
			})
		case "code":
			c, ok := codes[id]
			if !ok {
				continue
			}
			refCode[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":       id,
				"type":     "code",
				"language": c.Language,
				"filename": c.Filename,
				"code":     c.Code,
			})
		case "figure":
			f, ok := figures[id]
			if !ok {
				continue
			}
			refFigure[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "figure",
				"asset":     toAny(f.Asset),
				"caption":   f.Caption,
				"citations": toAny(f.Citations),
			})
		case "video":
			v, ok := videos[id]
			if !ok {
				continue
			}
			refVideo[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "video",
				"url":       v.URL,
				"start_sec": v.StartSec,
				"caption":   v.Caption,
			})
		case "diagram":
			d, ok := diagrams[id]
			if !ok {
				continue
			}
			refDiagram[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "diagram",
				"kind":      d.Kind,
				"source":    d.Source,
				"caption":   d.Caption,
				"citations": toAny(d.Citations),
			})
		case "table":
			t, ok := tables[id]
			if !ok {
				continue
			}
			refTable[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "table",
				"caption":   t.Caption,
				"columns":   toAny(t.Columns),
				"rows":      toAny(t.Rows),
				"citations": toAny(t.Citations),
			})
		case "equation":
			eq, ok := equations[id]
			if !ok {
				continue
			}
			refEquations[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "equation",
				"latex":     eq.Latex,
				"display":   eq.Display,
				"caption":   eq.Caption,
				"citations": toAny(eq.Citations),
			})
		case "quick_check":
			q, ok := qcs[id]
			if !ok {
				continue
			}
			refQC[id] = true
			triggerIDs := cleanTriggerIDs(q.TriggerAfterBlockIDs)
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":                     id,
				"type":                   "quick_check",
				"kind":                   q.Kind,
				"prompt_md":              q.PromptMD,
				"options":                toAny(q.Options),
				"answer_id":              q.AnswerID,
				"answer_md":              q.AnswerMD,
				"trigger_after_block_ids": toAny(triggerIDs),
				"citations":              toAny(q.Citations),
			})
		case "flashcard":
			f, ok := fcs[id]
			if !ok {
				continue
			}
			refFlashcard[id] = true
			triggerIDs := cleanTriggerIDs(f.TriggerAfterBlockIDs)
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":                     id,
				"type":                   "flashcard",
				"front_md":               f.FrontMD,
				"back_md":                f.BackMD,
				"concept_keys":           toAny(f.ConceptKeys),
				"trigger_after_block_ids": toAny(triggerIDs),
				"citations":              toAny(f.Citations),
			})
		case "divider":
			refDivider[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{"id": id, "type": "divider"})
		case "objectives":
			b, ok := objectives[id]
			if !ok {
				continue
			}
			refObjectives[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "objectives",
				"title":     b.Title,
				"items_md":  toAny(b.ItemsMD),
				"citations": toAny(b.Citations),
			})
		case "prerequisites":
			b, ok := prereqs[id]
			if !ok {
				continue
			}
			refPrerequisites[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "prerequisites",
				"title":     b.Title,
				"items_md":  toAny(b.ItemsMD),
				"citations": toAny(b.Citations),
			})
		case "key_takeaways":
			b, ok := keyTakeaways[id]
			if !ok {
				continue
			}
			refKeyTakeaways[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "key_takeaways",
				"title":     b.Title,
				"items_md":  toAny(b.ItemsMD),
				"citations": toAny(b.Citations),
			})
		case "glossary":
			b, ok := glossary[id]
			if !ok {
				continue
			}
			refGlossary[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "glossary",
				"title":     b.Title,
				"terms":     toAny(b.Terms),
				"citations": toAny(b.Citations),
			})
		case "common_mistakes":
			b, ok := commonMistakes[id]
			if !ok {
				continue
			}
			refCommonMistakes[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "common_mistakes",
				"title":     b.Title,
				"items_md":  toAny(b.ItemsMD),
				"citations": toAny(b.Citations),
			})
		case "misconceptions":
			b, ok := misconceptions[id]
			if !ok {
				continue
			}
			refMisconceptions[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "misconceptions",
				"title":     b.Title,
				"items_md":  toAny(b.ItemsMD),
				"citations": toAny(b.Citations),
			})
		case "edge_cases":
			b, ok := edgeCases[id]
			if !ok {
				continue
			}
			refEdgeCases[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "edge_cases",
				"title":     b.Title,
				"items_md":  toAny(b.ItemsMD),
				"citations": toAny(b.Citations),
			})
		case "heuristics":
			b, ok := heuristics[id]
			if !ok {
				continue
			}
			refHeuristics[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "heuristics",
				"title":     b.Title,
				"items_md":  toAny(b.ItemsMD),
				"citations": toAny(b.Citations),
			})
		case "steps":
			b, ok := steps[id]
			if !ok {
				continue
			}
			refSteps[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "steps",
				"title":     b.Title,
				"steps_md":  toAny(b.StepsMD),
				"citations": toAny(b.Citations),
			})
		case "checklist":
			b, ok := checklists[id]
			if !ok {
				continue
			}
			refChecklist[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "checklist",
				"title":     b.Title,
				"items_md":  toAny(b.ItemsMD),
				"citations": toAny(b.Citations),
			})
		case "faq":
			b, ok := faq[id]
			if !ok {
				continue
			}
			refFAQ[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "faq",
				"title":     b.Title,
				"qas":       toAny(b.QAs),
				"citations": toAny(b.Citations),
			})
		case "intuition":
			b, ok := intuition[id]
			if !ok {
				continue
			}
			refIntuition[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "intuition",
				"title":     b.Title,
				"md":        b.MD,
				"citations": toAny(b.Citations),
			})
		case "mental_model":
			b, ok := mentalModel[id]
			if !ok {
				continue
			}
			refMentalModel[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "mental_model",
				"title":     b.Title,
				"md":        b.MD,
				"citations": toAny(b.Citations),
			})
		case "why_it_matters":
			b, ok := whyItMatters[id]
			if !ok {
				continue
			}
			refWhyItMatters[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "why_it_matters",
				"title":     b.Title,
				"md":        b.MD,
				"citations": toAny(b.Citations),
			})
		case "connections":
			b, ok := connections[id]
			if !ok {
				continue
			}
			refConnections[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "connections",
				"title":     b.Title,
				"items_md":  toAny(b.ItemsMD),
				"citations": toAny(b.Citations),
			})
		default:
			continue
		}
	}

	// Append any orphan blocks that weren't referenced in order (keeps docs usable without regeneration).
	for _, h := range headingSeq {
		if refHeading[h.ID] {
			continue
		}
		level := h.Level
		if level < 2 {
			level = 2
		} else if level > 4 {
			level = 4
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": h.ID, "type": "heading", "level": level, "text": h.Text})
	}
	for _, p := range paragraphSeq {
		if refParagraph[p.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": p.ID, "type": "paragraph", "md": p.MD, "citations": toAny(p.Citations)})
	}
	for _, c := range calloutSeq {
		if refCallout[c.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": c.ID, "type": "callout", "variant": c.Variant, "title": c.Title, "md": c.MD, "citations": toAny(c.Citations)})
	}
	for _, c := range codeSeq {
		if refCode[c.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": c.ID, "type": "code", "language": c.Language, "filename": c.Filename, "code": c.Code})
	}
	for _, f := range figureSeq {
		if refFigure[f.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": f.ID, "type": "figure", "asset": toAny(f.Asset), "caption": f.Caption, "citations": toAny(f.Citations)})
	}
	for _, v := range videoSeq {
		if refVideo[v.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": v.ID, "type": "video", "url": v.URL, "start_sec": v.StartSec, "caption": v.Caption})
	}
	for _, d := range diagramSeq {
		if refDiagram[d.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": d.ID, "type": "diagram", "kind": d.Kind, "source": d.Source, "caption": d.Caption, "citations": toAny(d.Citations)})
	}
	for _, t := range tableSeq {
		if refTable[t.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": t.ID, "type": "table", "caption": t.Caption, "columns": toAny(t.Columns), "rows": toAny(t.Rows), "citations": toAny(t.Citations)})
	}
	for _, e := range equationSeq {
		if refEquations[e.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": e.ID, "type": "equation", "latex": e.Latex, "display": e.Display, "caption": e.Caption, "citations": toAny(e.Citations)})
	}
	for _, q := range qcSeq {
		if refQC[q.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{
			"id":        q.ID,
			"type":      "quick_check",
			"kind":      q.Kind,
			"prompt_md": q.PromptMD,
			"options":   toAny(q.Options),
			"answer_id": q.AnswerID,
			"answer_md": q.AnswerMD,
			"citations": toAny(q.Citations),
		})
	}
	for _, f := range fcSeq {
		if refFlashcard[f.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{
			"id":           f.ID,
			"type":         "flashcard",
			"front_md":     f.FrontMD,
			"back_md":      f.BackMD,
			"concept_keys": toAny(f.ConceptKeys),
			"citations":    toAny(f.Citations),
		})
	}
	for _, d := range dividerSeq {
		if refDivider[d.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": d.ID, "type": "divider"})
	}
	for _, b := range objectivesSeq {
		if refObjectives[b.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": b.ID, "type": "objectives", "title": b.Title, "items_md": toAny(b.ItemsMD), "citations": toAny(b.Citations)})
	}
	for _, b := range prereqSeq {
		if refPrerequisites[b.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": b.ID, "type": "prerequisites", "title": b.Title, "items_md": toAny(b.ItemsMD), "citations": toAny(b.Citations)})
	}
	for _, b := range keyTakeawaysSeq {
		if refKeyTakeaways[b.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": b.ID, "type": "key_takeaways", "title": b.Title, "items_md": toAny(b.ItemsMD), "citations": toAny(b.Citations)})
	}
	for _, b := range glossarySeq {
		if refGlossary[b.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": b.ID, "type": "glossary", "title": b.Title, "terms": toAny(b.Terms), "citations": toAny(b.Citations)})
	}
	for _, b := range commonMistakesSeq {
		if refCommonMistakes[b.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": b.ID, "type": "common_mistakes", "title": b.Title, "items_md": toAny(b.ItemsMD), "citations": toAny(b.Citations)})
	}
	for _, b := range misconceptionsSeq {
		if refMisconceptions[b.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": b.ID, "type": "misconceptions", "title": b.Title, "items_md": toAny(b.ItemsMD), "citations": toAny(b.Citations)})
	}
	for _, b := range edgeCasesSeq {
		if refEdgeCases[b.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": b.ID, "type": "edge_cases", "title": b.Title, "items_md": toAny(b.ItemsMD), "citations": toAny(b.Citations)})
	}
	for _, b := range heuristicsSeq {
		if refHeuristics[b.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": b.ID, "type": "heuristics", "title": b.Title, "items_md": toAny(b.ItemsMD), "citations": toAny(b.Citations)})
	}
	for _, b := range stepsSeq {
		if refSteps[b.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": b.ID, "type": "steps", "title": b.Title, "steps_md": toAny(b.StepsMD), "citations": toAny(b.Citations)})
	}
	for _, b := range checklistsSeq {
		if refChecklist[b.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": b.ID, "type": "checklist", "title": b.Title, "items_md": toAny(b.ItemsMD), "citations": toAny(b.Citations)})
	}
	for _, b := range faqSeq {
		if refFAQ[b.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": b.ID, "type": "faq", "title": b.Title, "qas": toAny(b.QAs), "citations": toAny(b.Citations)})
	}
	for _, b := range intuitionSeq {
		if refIntuition[b.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": b.ID, "type": "intuition", "title": b.Title, "md": b.MD, "citations": toAny(b.Citations)})
	}
	for _, b := range mentalModelSeq {
		if refMentalModel[b.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": b.ID, "type": "mental_model", "title": b.Title, "md": b.MD, "citations": toAny(b.Citations)})
	}
	for _, b := range whyItMattersSeq {
		if refWhyItMatters[b.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": b.ID, "type": "why_it_matters", "title": b.Title, "md": b.MD, "citations": toAny(b.Citations)})
	}
	for _, b := range connectionsSeq {
		if refConnections[b.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": b.ID, "type": "connections", "title": b.Title, "items_md": toAny(b.ItemsMD), "citations": toAny(b.Citations)})
	}

	return doc, dedupeStrings(errs)
}

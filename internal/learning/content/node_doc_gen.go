package content

import (
	"encoding/json"
	"strings"
)

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
	QuickChecks []NodeDocGenQuickCheckV1 `json:"quick_checks"`
	Dividers    []NodeDocGenDividerV1    `json:"dividers"`
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

type NodeDocGenQuickCheckV1 struct {
	ID        string          `json:"id"`
	PromptMD  string          `json:"prompt_md"`
	AnswerMD  string          `json:"answer_md"`
	Citations []CitationRefV1 `json:"citations"`
}

type NodeDocGenDividerV1 struct {
	ID string `json:"id"`
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
	qcs := map[string]NodeDocGenQuickCheckV1{}
	qcSeq := make([]NodeDocGenQuickCheckV1, 0, len(gen.QuickChecks))
	divs := map[string]NodeDocGenDividerV1{}
	dividerSeq := make([]NodeDocGenDividerV1, 0, len(gen.Dividers))

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

	refHeading := map[string]bool{}
	refParagraph := map[string]bool{}
	refCallout := map[string]bool{}
	refCode := map[string]bool{}
	refFigure := map[string]bool{}
	refVideo := map[string]bool{}
	refDiagram := map[string]bool{}
	refTable := map[string]bool{}
	refQC := map[string]bool{}
	refDivider := map[string]bool{}

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
		case "quick_check":
			q, ok := qcs[id]
			if !ok {
				continue
			}
			refQC[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{
				"id":        id,
				"type":      "quick_check",
				"prompt_md": q.PromptMD,
				"answer_md": q.AnswerMD,
				"citations": toAny(q.Citations),
			})
		case "divider":
			refDivider[id] = true
			doc.Blocks = append(doc.Blocks, map[string]any{"id": id, "type": "divider"})
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
	for _, q := range qcSeq {
		if refQC[q.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": q.ID, "type": "quick_check", "prompt_md": q.PromptMD, "answer_md": q.AnswerMD, "citations": toAny(q.Citations)})
	}
	for _, d := range dividerSeq {
		if refDivider[d.ID] {
			continue
		}
		doc.Blocks = append(doc.Blocks, map[string]any{"id": d.ID, "type": "divider"})
	}

	return doc, dedupeStrings(errs)
}

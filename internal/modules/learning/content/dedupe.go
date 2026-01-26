package content

import (
	"fmt"
	"strings"
)

// DedupNodeDocV1 removes obviously duplicated blocks that occasionally slip through generation
// (e.g., repeated paragraphs/summaries). This is a best-effort quality pass; the doc is still
// validated after de-duplication.
func DedupNodeDocV1(doc NodeDocV1) (NodeDocV1, []string) {
	if len(doc.Blocks) == 0 {
		return doc, nil
	}

	normalize := func(s string) string {
		s = strings.TrimSpace(s)
		if s == "" {
			return ""
		}
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.ReplaceAll(s, "\r", "\n")
		s = stripMD(s)
		s = strings.ToLower(strings.TrimSpace(s))
		// wsRE is defined in sanitize.go; it collapses all whitespace (including newlines).
		s = wsRE.ReplaceAllString(s, " ")
		return strings.TrimSpace(s)
	}

	summaryNorm := normalize(doc.Summary)

	removed := make([]string, 0)
	kept := make([]map[string]any, 0, len(doc.Blocks))

	seen := map[string]bool{}
	lastHeadingKey := ""
	lastWasDivider := false

	for i := 0; i < len(doc.Blocks); i++ {
		b := doc.Blocks[i]
		if b == nil {
			continue
		}
		t := strings.ToLower(strings.TrimSpace(stringFromAny(b["type"])))

		switch t {
		case "heading":
			text := strings.TrimSpace(stringFromAny(b["text"]))
			if text == "" {
				removed = append(removed, "empty_heading")
				continue
			}

			// If the document already has a summary field, drop a "Summary" section that merely repeats it.
			if summaryNorm != "" && strings.EqualFold(text, "summary") && i+1 < len(doc.Blocks) {
				nb := doc.Blocks[i+1]
				nt := strings.ToLower(strings.TrimSpace(stringFromAny(nb["type"])))
				switch nt {
				case "paragraph":
					if normalize(stringFromAny(nb["md"])) == summaryNorm {
						removed = append(removed, "summary_section_dup")
						i++ // also skip the repeated summary paragraph
						continue
					}
				case "callout":
					if normalize(stringFromAny(nb["md"])) == summaryNorm {
						removed = append(removed, "summary_section_dup")
						i++ // also skip the repeated summary callout
						continue
					}
				}
			}

			level := intFromAny(b["level"], 2)
			key := fmt.Sprintf("heading:%d:%s", level, normalize(text))
			if key == lastHeadingKey {
				removed = append(removed, "duplicate_heading")
				continue
			}
			lastHeadingKey = key
			lastWasDivider = false

		case "divider":
			if lastWasDivider {
				removed = append(removed, "duplicate_divider")
				continue
			}
			lastWasDivider = true
			lastHeadingKey = ""

		case "paragraph":
			md := strings.TrimSpace(stringFromAny(b["md"]))
			if md == "" {
				removed = append(removed, "empty_paragraph")
				continue
			}
			norm := normalize(md)
			if norm == "" {
				removed = append(removed, "empty_paragraph")
				continue
			}
			if summaryNorm != "" && norm == summaryNorm {
				removed = append(removed, "summary_dup_paragraph")
				continue
			}
			key := "paragraph:" + norm
			if seen[key] {
				removed = append(removed, "duplicate_paragraph")
				continue
			}
			seen[key] = true
			lastWasDivider = false

		case "callout":
			title := strings.TrimSpace(stringFromAny(b["title"]))
			md := strings.TrimSpace(stringFromAny(b["md"]))
			variant := strings.ToLower(strings.TrimSpace(stringFromAny(b["variant"])))
			norm := normalize(title + "\n" + md)
			if norm == "" {
				removed = append(removed, "empty_callout")
				continue
			}
			if summaryNorm != "" && normalize(md) == summaryNorm && strings.EqualFold(title, "summary") {
				removed = append(removed, "summary_dup_callout")
				continue
			}
			key := "callout:" + variant + ":" + norm
			if seen[key] {
				removed = append(removed, "duplicate_callout")
				continue
			}
			seen[key] = true
			lastWasDivider = false

		case "equation":
			latex := strings.TrimSpace(stringFromAny(b["latex"]))
			caption := strings.TrimSpace(stringFromAny(b["caption"]))
			norm := normalize(latex + "\n" + caption)
			if norm == "" {
				removed = append(removed, "empty_equation")
				continue
			}
			key := "equation:" + norm
			if seen[key] {
				removed = append(removed, "duplicate_equation")
				continue
			}
			seen[key] = true
			lastWasDivider = false

		case "quick_check":
			prompt := strings.TrimSpace(stringFromAny(b["prompt_md"]))
			answer := strings.TrimSpace(stringFromAny(b["answer_md"]))
			norm := normalize(prompt + "\n" + answer)
			if norm == "" {
				removed = append(removed, "empty_quick_check")
				continue
			}
			key := "quick_check:" + norm
			if seen[key] {
				removed = append(removed, "duplicate_quick_check")
				continue
			}
			seen[key] = true
			lastWasDivider = false

		case "objectives", "prerequisites", "key_takeaways", "common_mistakes", "misconceptions", "edge_cases", "heuristics", "checklist", "connections":
			title := strings.TrimSpace(stringFromAny(b["title"]))
			items := stringSliceFromAny(b["items_md"])
			norm := normalize(title + "\n" + strings.Join(items, "\n"))
			if norm == "" {
				removed = append(removed, "empty_list_block")
				continue
			}
			key := t + ":" + norm
			if seen[key] {
				removed = append(removed, "duplicate_"+t)
				continue
			}
			seen[key] = true
			lastWasDivider = false

		case "steps":
			title := strings.TrimSpace(stringFromAny(b["title"]))
			steps := stringSliceFromAny(b["steps_md"])
			norm := normalize(title + "\n" + strings.Join(steps, "\n"))
			if norm == "" {
				removed = append(removed, "empty_steps")
				continue
			}
			key := "steps:" + norm
			if seen[key] {
				removed = append(removed, "duplicate_steps")
				continue
			}
			seen[key] = true
			lastWasDivider = false

		case "glossary":
			title := strings.TrimSpace(stringFromAny(b["title"]))
			var joined strings.Builder
			joined.WriteString(title)
			joined.WriteString("\n")
			if arr, ok := b["terms"].([]any); ok {
				for _, it := range arr {
					m, ok := it.(map[string]any)
					if !ok {
						continue
					}
					joined.WriteString(stringFromAny(m["term"]))
					joined.WriteString(" ")
					joined.WriteString(stringFromAny(m["definition_md"]))
					joined.WriteString("\n")
				}
			}
			norm := normalize(joined.String())
			if norm == "" {
				removed = append(removed, "empty_glossary")
				continue
			}
			key := "glossary:" + norm
			if seen[key] {
				removed = append(removed, "duplicate_glossary")
				continue
			}
			seen[key] = true
			lastWasDivider = false

		case "faq":
			title := strings.TrimSpace(stringFromAny(b["title"]))
			var joined strings.Builder
			joined.WriteString(title)
			joined.WriteString("\n")
			if arr, ok := b["qas"].([]any); ok {
				for _, it := range arr {
					m, ok := it.(map[string]any)
					if !ok {
						continue
					}
					joined.WriteString(stringFromAny(m["question_md"]))
					joined.WriteString(" ")
					joined.WriteString(stringFromAny(m["answer_md"]))
					joined.WriteString("\n")
				}
			}
			norm := normalize(joined.String())
			if norm == "" {
				removed = append(removed, "empty_faq")
				continue
			}
			key := "faq:" + norm
			if seen[key] {
				removed = append(removed, "duplicate_faq")
				continue
			}
			seen[key] = true
			lastWasDivider = false

		case "intuition", "mental_model", "why_it_matters":
			title := strings.TrimSpace(stringFromAny(b["title"]))
			md := strings.TrimSpace(stringFromAny(b["md"]))
			norm := normalize(title + "\n" + md)
			if norm == "" {
				removed = append(removed, "empty_"+t)
				continue
			}
			key := t + ":" + norm
			if seen[key] {
				removed = append(removed, "duplicate_"+t)
				continue
			}
			seen[key] = true
			lastWasDivider = false

		default:
			lastWasDivider = false
		}

		kept = append(kept, b)
	}

	if len(kept) == len(doc.Blocks) {
		return doc, nil
	}
	doc.Blocks = kept
	return doc, dedupeStringsLocal(removed)
}

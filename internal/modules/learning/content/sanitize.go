package content

import (
	"fmt"
	"regexp"
	"strings"
)

type scrubRule struct {
	Label       string
	Re          *regexp.Regexp
	Replacement string
}

var wsRE = regexp.MustCompile(`\s{2,}`)

var nodeDocMetaScrubRules = []scrubRule{
	{Label: "quick check-in", Re: regexp.MustCompile(`(?i)quick check-in`), Replacement: "quick check"},
	{Label: "here's the plan", Re: regexp.MustCompile(`(?i)here's the plan`), Replacement: "overview"},
	{Label: "here is the plan", Re: regexp.MustCompile(`(?i)here is the plan`), Replacement: "overview"},
	{Label: "plan:", Re: regexp.MustCompile(`(?i)\bplan:`), Replacement: "overview:"},
	{Label: "i can tailor this", Re: regexp.MustCompile(`(?i)i can tailor this`), Replacement: ""},
	{Label: "before we dive in", Re: regexp.MustCompile(`(?i)before we dive in`), Replacement: ""},
	{Label: "answer these", Re: regexp.MustCompile(`(?i)\banswer these\b`), Replacement: ""},
	{Label: "pick one", Re: regexp.MustCompile(`(?i)\bpick\s+one\b\s*:?\s*`), Replacement: ""},
	{Label: "if you want to go deeper", Re: regexp.MustCompile(`(?i)if you want to go deeper`), Replacement: ""},
	{Label: "if you'd like to go deeper", Re: regexp.MustCompile(`(?i)if you'd like to go deeper`), Replacement: ""},
	{Label: "let me know if you want", Re: regexp.MustCompile(`(?i)let me know if you want`), Replacement: ""},
}

func scrubMetaText(s string) (string, []string) {
	if strings.TrimSpace(s) == "" {
		return s, nil
	}
	orig := s
	hit := make([]string, 0)
	for _, r := range nodeDocMetaScrubRules {
		if r.Re == nil {
			continue
		}
		if r.Re.MatchString(s) {
			s = r.Re.ReplaceAllString(s, r.Replacement)
			hit = append(hit, r.Label)
		}
	}
	if s != orig {
		s = wsRE.ReplaceAllString(s, " ")
		s = strings.ReplaceAll(s, " \n", "\n")
		s = strings.ReplaceAll(s, "\n ", "\n")
		s = strings.TrimSpace(s)
	}
	return s, dedupeStringsLocal(hit)
}

func ScrubNodeDocV1(doc NodeDocV1) (NodeDocV1, []string) {
	hit := make([]string, 0)

	var h []string
	doc.Title, h = scrubMetaText(doc.Title)
	hit = append(hit, h...)
	doc.Summary, h = scrubMetaText(doc.Summary)
	hit = append(hit, h...)

	for i := range doc.Blocks {
		b := doc.Blocks[i]
		if b == nil {
			continue
		}

		// Only scrub learner-facing text fields. Do NOT modify code/diagram sources.
		for _, key := range []string{
			"text",
			"md",
			"title",
			"caption",
			"prompt_md",
			"answer_md",
			"language",
			"filename",
		} {
			v, ok := b[key]
			if !ok || v == nil {
				continue
			}
			s, ok := v.(string)
			if !ok {
				continue
			}
			if strings.TrimSpace(s) == "" {
				continue
			}
			ns, hh := scrubMetaText(s)
			if ns != s {
				b[key] = ns
			}
			hit = append(hit, hh...)
		}

		// Scrub structured list blocks.
		if raw, ok := b["items_md"]; ok && raw != nil {
			if arr, ok := raw.([]any); ok && len(arr) > 0 {
				changed := false
				for j := range arr {
					s, ok := arr[j].(string)
					if !ok || strings.TrimSpace(s) == "" {
						continue
					}
					ns, hh := scrubMetaText(s)
					if ns != s {
						arr[j] = ns
						changed = true
					}
					hit = append(hit, hh...)
				}
				if changed {
					b["items_md"] = arr
				}
			}
		}
		if raw, ok := b["steps_md"]; ok && raw != nil {
			if arr, ok := raw.([]any); ok && len(arr) > 0 {
				changed := false
				for j := range arr {
					s, ok := arr[j].(string)
					if !ok || strings.TrimSpace(s) == "" {
						continue
					}
					ns, hh := scrubMetaText(s)
					if ns != s {
						arr[j] = ns
						changed = true
					}
					hit = append(hit, hh...)
				}
				if changed {
					b["steps_md"] = arr
				}
			}
		}
		if raw, ok := b["terms"]; ok && raw != nil {
			if arr, ok := raw.([]any); ok && len(arr) > 0 {
				changed := false
				for j := range arr {
					m, ok := arr[j].(map[string]any)
					if !ok || m == nil {
						continue
					}
					if s, ok := m["term"].(string); ok && strings.TrimSpace(s) != "" {
						ns, hh := scrubMetaText(s)
						if ns != s {
							m["term"] = ns
							changed = true
						}
						hit = append(hit, hh...)
					}
					if s, ok := m["definition_md"].(string); ok && strings.TrimSpace(s) != "" {
						ns, hh := scrubMetaText(s)
						if ns != s {
							m["definition_md"] = ns
							changed = true
						}
						hit = append(hit, hh...)
					}
					arr[j] = m
				}
				if changed {
					b["terms"] = arr
				}
			}
		}
		if raw, ok := b["qas"]; ok && raw != nil {
			if arr, ok := raw.([]any); ok && len(arr) > 0 {
				changed := false
				for j := range arr {
					m, ok := arr[j].(map[string]any)
					if !ok || m == nil {
						continue
					}
					if s, ok := m["question_md"].(string); ok && strings.TrimSpace(s) != "" {
						ns, hh := scrubMetaText(s)
						if ns != s {
							m["question_md"] = ns
							changed = true
						}
						hit = append(hit, hh...)
					}
					if s, ok := m["answer_md"].(string); ok && strings.TrimSpace(s) != "" {
						ns, hh := scrubMetaText(s)
						if ns != s {
							m["answer_md"] = ns
							changed = true
						}
						hit = append(hit, hh...)
					}
					arr[j] = m
				}
				if changed {
					b["qas"] = arr
				}
			}
		}
		if raw, ok := b["options"]; ok && raw != nil {
			if arr, ok := raw.([]any); ok && len(arr) > 0 {
				changed := false
				for j := range arr {
					m, ok := arr[j].(map[string]any)
					if !ok || m == nil {
						continue
					}
					if s, ok := m["text"].(string); ok && strings.TrimSpace(s) != "" {
						ns, hh := scrubMetaText(s)
						if ns != s {
							m["text"] = ns
							changed = true
						}
						hit = append(hit, hh...)
					}
					arr[j] = m
				}
				if changed {
					b["options"] = arr
				}
			}
		}

		doc.Blocks[i] = b
	}

	return doc, dedupeStringsLocal(hit)
}

func ScrubDrillPayloadV1(p DrillPayloadV1) (DrillPayloadV1, []string) {
	hit := make([]string, 0)

	var h []string
	p.Kind, h = scrubMetaText(p.Kind)
	hit = append(hit, h...)

	for i := range p.Cards {
		p.Cards[i].FrontMD, h = scrubMetaText(p.Cards[i].FrontMD)
		hit = append(hit, h...)
		p.Cards[i].BackMD, h = scrubMetaText(p.Cards[i].BackMD)
		hit = append(hit, h...)
	}

	for i := range p.Questions {
		p.Questions[i].ID, h = scrubMetaText(p.Questions[i].ID)
		hit = append(hit, h...)
		p.Questions[i].PromptMD, h = scrubMetaText(p.Questions[i].PromptMD)
		hit = append(hit, h...)
		p.Questions[i].ExplanationMD, h = scrubMetaText(p.Questions[i].ExplanationMD)
		hit = append(hit, h...)
		p.Questions[i].AnswerID, h = scrubMetaText(p.Questions[i].AnswerID)
		hit = append(hit, h...)

		for j := range p.Questions[i].Options {
			p.Questions[i].Options[j].ID, h = scrubMetaText(p.Questions[i].Options[j].ID)
			hit = append(hit, h...)
			p.Questions[i].Options[j].Text, h = scrubMetaText(p.Questions[i].Options[j].Text)
			hit = append(hit, h...)
		}
	}

	return p, dedupeStringsLocal(hit)
}

// PruneNodeDocMetaBlocks removes obviously meta / onboarding blocks that sometimes slip into
// learner-facing docs (e.g., "Entry Check" sections that ask the learner questions).
//
// This is a best-effort scrub to improve build reliability without weakening hard validation rules.
func PruneNodeDocMetaBlocks(doc NodeDocV1) (NodeDocV1, []string) {
	if len(doc.Blocks) == 0 {
		return doc, nil
	}

	isMetaHeading := func(s string) bool {
		l := strings.ToLower(strings.TrimSpace(s))
		if l == "" {
			return false
		}
		if strings.Contains(l, "entry check") {
			return true
		}
		if strings.Contains(l, "format preference") || strings.Contains(l, "format preferences") {
			return true
		}
		if strings.Contains(l, "your goal") && strings.Contains(l, "level") {
			return true
		}
		if strings.Contains(l, "goal, level") {
			return true
		}
		if strings.Contains(l, "check-in") {
			return true
		}
		return false
	}

	isMetaBody := func(s string) bool {
		l := strings.ToLower(strings.TrimSpace(s))
		if l == "" {
			return false
		}
		meta := []string{
			"before we dive in",
			"answer these",
			"so i can",
			"to tailor",
			"what are you using this for",
			"what's your current",
			"what is your current",
			"do you prefer",
			"any constraints",
			"while you think about that",
			"tell me",
		}
		for _, m := range meta {
			if strings.Contains(l, m) {
				return true
			}
		}
		return false
	}

	removed := []string{}
	kept := make([]map[string]any, 0, len(doc.Blocks))

	for _, b := range doc.Blocks {
		if b == nil {
			continue
		}
		t := strings.ToLower(strings.TrimSpace(fmt.Sprint(b["type"])))
		switch t {
		case "heading":
			if isMetaHeading(fmt.Sprint(b["text"])) {
				removed = append(removed, "meta_heading")
				continue
			}
		case "paragraph":
			if isMetaBody(fmt.Sprint(b["md"])) {
				removed = append(removed, "meta_paragraph")
				continue
			}
		case "callout":
			if isMetaHeading(fmt.Sprint(b["title"])) || isMetaBody(fmt.Sprint(b["md"])) {
				removed = append(removed, "meta_callout")
				continue
			}
		case "intuition", "mental_model", "why_it_matters":
			if isMetaHeading(fmt.Sprint(b["title"])) || isMetaBody(fmt.Sprint(b["md"])) {
				removed = append(removed, "meta_section")
				continue
			}
		case "objectives", "prerequisites", "key_takeaways", "common_mistakes", "misconceptions", "edge_cases", "heuristics", "checklist", "connections":
			title := fmt.Sprint(b["title"])
			if isMetaHeading(title) {
				removed = append(removed, "meta_list")
				continue
			}
			if arr, ok := b["items_md"].([]any); ok {
				parts := make([]string, 0, len(arr))
				for _, it := range arr {
					parts = append(parts, fmt.Sprint(it))
				}
				if isMetaBody(strings.Join(parts, "\n")) {
					removed = append(removed, "meta_list")
					continue
				}
			} else if isMetaBody(fmt.Sprint(b["items_md"])) {
				removed = append(removed, "meta_list")
				continue
			}
		case "steps":
			title := fmt.Sprint(b["title"])
			if isMetaHeading(title) {
				removed = append(removed, "meta_steps")
				continue
			}
			if arr, ok := b["steps_md"].([]any); ok {
				parts := make([]string, 0, len(arr))
				for _, it := range arr {
					parts = append(parts, fmt.Sprint(it))
				}
				if isMetaBody(strings.Join(parts, "\n")) {
					removed = append(removed, "meta_steps")
					continue
				}
			} else if isMetaBody(fmt.Sprint(b["steps_md"])) {
				removed = append(removed, "meta_steps")
				continue
			}
		case "glossary":
			if isMetaHeading(fmt.Sprint(b["title"])) {
				removed = append(removed, "meta_glossary")
				continue
			}
			if arr, ok := b["terms"].([]any); ok {
				var joined strings.Builder
				for _, it := range arr {
					m, ok := it.(map[string]any)
					if !ok {
						continue
					}
					joined.WriteString(fmt.Sprint(m["term"]))
					joined.WriteString(" ")
					joined.WriteString(fmt.Sprint(m["definition_md"]))
					joined.WriteString("\n")
				}
				if isMetaBody(joined.String()) {
					removed = append(removed, "meta_glossary")
					continue
				}
			}
		case "faq":
			if isMetaHeading(fmt.Sprint(b["title"])) {
				removed = append(removed, "meta_faq")
				continue
			}
			if arr, ok := b["qas"].([]any); ok {
				var joined strings.Builder
				for _, it := range arr {
					m, ok := it.(map[string]any)
					if !ok {
						continue
					}
					joined.WriteString(fmt.Sprint(m["question_md"]))
					joined.WriteString(" ")
					joined.WriteString(fmt.Sprint(m["answer_md"]))
					joined.WriteString("\n")
				}
				if isMetaBody(joined.String()) {
					removed = append(removed, "meta_faq")
					continue
				}
			}
		}
		kept = append(kept, b)
	}

	if len(kept) == len(doc.Blocks) {
		return doc, nil
	}
	doc.Blocks = kept
	return doc, dedupeStringsLocal(removed)
}

func dedupeStringsLocal(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(fmt.Sprint(s))
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

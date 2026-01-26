package extractor

import (
	"fmt"
	"strings"
	"unicode"
)

type EquationMatch struct {
	Placeholder string `json:"placeholder"`
	Latex       string `json:"latex"`
	Display     bool   `json:"display"`
}

// ExtractLatexEquations finds $...$ and $$...$$ math, replaces them with placeholders,
// and returns extracted equations with display hints.
func ExtractLatexEquations(text string) (string, []EquationMatch) {
	if !strings.Contains(text, "$") {
		return text, nil
	}
	out := text
	var eqs []EquationMatch
	out, eqs = extractDelimitedEquations(out, "$$", true, eqs)
	out, eqs = extractDelimitedEquations(out, "$", false, eqs)
	return out, eqs
}

func extractDelimitedEquations(text string, delim string, display bool, existing []EquationMatch) (string, []EquationMatch) {
	if strings.TrimSpace(text) == "" {
		return text, existing
	}
	var b strings.Builder
	b.Grow(len(text))

	isDelim := func(s string, i int) bool {
		if i < 0 || i+len(delim) > len(s) {
			return false
		}
		if s[i:i+len(delim)] != delim {
			return false
		}
		// escaped delimiter
		slashes := 0
		for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
			slashes++
		}
		if slashes%2 == 1 {
			return false
		}
		return true
	}

	for i := 0; i < len(text); {
		if !isDelim(text, i) {
			b.WriteByte(text[i])
			i++
			continue
		}
		// avoid treating $$ as $ when parsing inline
		if delim == "$" && i+1 < len(text) && text[i+1] == '$' {
			b.WriteByte(text[i])
			i++
			continue
		}
		start := i + len(delim)
		j := start
		for j < len(text) {
			if isDelim(text, j) {
				break
			}
			j++
		}
		if j >= len(text) || !isDelim(text, j) {
			// no closing delim; emit literal
			b.WriteString(text[i : i+len(delim)])
			i += len(delim)
			continue
		}
		content := text[start:j]
		trimmed := strings.TrimSpace(content)
		if !looksLikeMath(trimmed) {
			// emit literal unchanged
			b.WriteString(text[i : j+len(delim)])
			i = j + len(delim)
			continue
		}
		idx := len(existing) + 1
		label := "EQ"
		if display {
			label = "EQD"
		}
		placeholder := fmt.Sprintf("[[%s%d]]", label, idx)
		b.WriteString(placeholder)
		existing = append(existing, EquationMatch{Placeholder: placeholder, Latex: trimmed, Display: display})
		i = j + len(delim)
	}

	return b.String(), existing
}

func looksLikeMath(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	// Quick currency guard: $12.99, $1,000
	onlyDigits := true
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		if unicode.IsDigit(r) || r == ',' || r == '.' {
			continue
		}
		onlyDigits = false
		break
	}
	if onlyDigits {
		return false
	}
	// Must contain a mathy signal
	mathy := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			mathy = true
			break
		}
		switch r {
		case '\\', '^', '_', '=', '+', '-', '*', '/', '{', '}', '(', ')', '[', ']':
			mathy = true
			break
		}
	}
	return mathy
}

func equationsForChunk(text string, eqs []EquationMatch) []EquationMatch {
	if len(eqs) == 0 || strings.TrimSpace(text) == "" {
		return nil
	}
	out := make([]EquationMatch, 0)
	for _, eq := range eqs {
		if eq.Placeholder == "" {
			continue
		}
		if strings.Contains(text, eq.Placeholder) {
			out = append(out, eq)
		}
	}
	if len(out) == 0 {
		return nil
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

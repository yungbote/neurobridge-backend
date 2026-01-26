package services

import (
	"strings"
	"unicode"
)

func isLikelyStructureSelectionMessage(content string) bool {
	s := strings.ToLower(strings.TrimSpace(content))
	if s == "" {
		return false
	}
	if strings.Contains(s, "?") || looksLikeStructureQuestion(s) {
		return false
	}

	s = stripLeadingFiller(s)
	if s == "" {
		return false
	}
	if looksLikeNumericChoice(s) {
		return true
	}
	if strings.Contains(s, "confirm") {
		return true
	}
	if looksLikeKeepTogether(s) {
		return true
	}
	if strings.Contains(s, "split") || strings.Contains(s, "separate") {
		return true
	}
	if strings.Contains(s, "merge") || strings.Contains(s, "combine") {
		return true
	}
	return false
}

func looksLikeStructureQuestion(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return false
	}
	for _, lead := range []string{
		"what ", "which ", "why ", "how ", "can you ", "could you ", "would you ", "do you ",
	} {
		if strings.HasPrefix(s, lead) {
			return true
		}
	}
	if strings.Contains(s, "recommended options") {
		return true
	}
	if strings.Contains(s, "what are") && strings.Contains(s, "options") {
		return true
	}
	if strings.Contains(s, "what are") && strings.Contains(s, "recommend") {
		return true
	}
	return false
}

func looksLikeNumericChoice(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	runes := []rune(s)
	start := 0
	for start < len(runes) {
		r := runes[start]
		if unicode.IsSpace(r) || r == '#' || r == '-' || r == '*' || r == '(' {
			start++
			continue
		}
		break
	}
	if start >= len(runes) || !unicode.IsDigit(runes[start]) {
		return false
	}
	end := start
	for end < len(runes) && unicode.IsDigit(runes[end]) {
		end++
	}
	if end == start {
		return false
	}
	return true
}

package extractor

import (
	"regexp"
	"strconv"
	"strings"
)

type BibliographyEntry struct {
	Label   string   `json:"label"`
	Raw     string   `json:"raw"`
	Authors []string `json:"authors"`
	Title   string   `json:"title"`
	Year    *int     `json:"year,omitempty"`
	DOI     string   `json:"doi"`
}

type CitationLink struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Key   string `json:"key"`
	Match string `json:"match"`
}

var (
	reRefHeading = regexp.MustCompile(`(?i)^\s*(references|bibliography|works cited|literature cited)\s*$`)
	reRefNumeric = regexp.MustCompile(`^\s*\[?(\d{1,3})\]?\.?\s+`)
	reYear       = regexp.MustCompile(`\b(19|20)\d{2}\b`)
	reDOI        = regexp.MustCompile(`(?i)\b10\.\d{4,9}/[-._;()/:A-Z0-9]+\b`)
	reAuthorYear = regexp.MustCompile(`\(([A-Z][A-Za-z'\-]+)(?:\s+et\s+al\.)?,\s*(\d{4})\)`)
	reNumericCite = regexp.MustCompile(`\[(\d{1,3})(?:\s*,\s*\d{1,3})*\]`)
)

func ParseBibliography(text string) []BibliographyEntry {
	lines := splitLines(text)
	start := -1
	for i, line := range lines {
		if reRefHeading.MatchString(strings.TrimSpace(line)) {
			start = i
		}
	}
	if start == -1 || start >= len(lines)-1 {
		return nil
	}
	candidates := make([]string, 0)
	var current string
	for i := start + 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if isNewReferenceLine(line) {
			if strings.TrimSpace(current) != "" {
				candidates = append(candidates, strings.TrimSpace(current))
			}
			current = line
			continue
		}
		if current == "" {
			current = line
		} else {
			current = current + " " + line
		}
	}
	if strings.TrimSpace(current) != "" {
		candidates = append(candidates, strings.TrimSpace(current))
	}
	if len(candidates) == 0 {
		return nil
	}

	out := make([]BibliographyEntry, 0, len(candidates))
	for _, raw := range candidates {
		label := ""
		line := strings.TrimSpace(raw)
		if m := reRefNumeric.FindStringSubmatch(line); len(m) > 1 {
			label = m[1]
			line = strings.TrimSpace(line[len(m[0]):])
		}
		doi := ""
		if m := reDOI.FindString(line); m != "" {
			doi = strings.TrimSpace(m)
		}
		year := (*int)(nil)
		if m := reYear.FindString(line); m != "" {
			if v, err := strconv.Atoi(m); err == nil {
				year = &v
			}
		}
		authors, title := parseAuthorsAndTitle(line)
		out = append(out, BibliographyEntry{
			Label:   label,
			Raw:     raw,
			Authors: authors,
			Title:   title,
			Year:    year,
			DOI:     doi,
		})
	}
	return out
}

func BuildBibliographyIndex(entries []BibliographyEntry) (map[string]bool, map[string]bool) {
	byLabel := map[string]bool{}
	byAuthorYear := map[string]bool{}
	for _, e := range entries {
		if e.Label != "" {
			byLabel[e.Label] = true
		}
		if key := authorYearKey(e); key != "" {
			byAuthorYear[key] = true
		}
	}
	return byLabel, byAuthorYear
}

func ExtractCitationLinks(text string, byLabel map[string]bool, byAuthorYear map[string]bool) []CitationLink {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	seen := map[string]bool{}
	out := make([]CitationLink, 0)

	for _, match := range reNumericCite.FindAllStringSubmatch(text, -1) {
		if len(match) == 0 {
			continue
		}
		label := match[1]
		if label == "" {
			continue
		}
		if !byLabel[label] {
			continue
		}
		key := "num:" + label
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, CitationLink{Kind: "numeric", Label: label, Key: key, Match: match[0]})
	}

	for _, match := range reAuthorYear.FindAllStringSubmatch(text, -1) {
		if len(match) < 3 {
			continue
		}
		surname := strings.TrimSpace(match[1])
		year := strings.TrimSpace(match[2])
		key := strings.ToLower(surname) + "|" + year
		if !byAuthorYear[key] {
			continue
		}
		seenKey := "ay:" + key
		if seen[seenKey] {
			continue
		}
		seen[seenKey] = true
		out = append(out, CitationLink{Kind: "author_year", Key: key, Match: match[0]})
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.Split(text, "\n")
}

func isNewReferenceLine(line string) bool {
	l := strings.TrimSpace(line)
	if l == "" {
		return false
	}
	if reRefNumeric.MatchString(l) {
		return true
	}
	if len(l) > 2 && strings.Index(l, ",") > 0 {
		first := l[:strings.Index(l, ",")]
		if len(first) > 0 && isCapitalized(first) {
			return true
		}
	}
	return false
}

func parseAuthorsAndTitle(line string) ([]string, string) {
	clean := strings.TrimSpace(line)
	yearIdx := reYear.FindStringIndex(clean)
	prefix := clean
	suffix := ""
	if yearIdx != nil {
		prefix = strings.TrimSpace(clean[:yearIdx[0]])
		suffix = strings.TrimSpace(clean[yearIdx[1]:])
	}
	prefix = strings.Trim(prefix, ".,;:")

	authors := splitAuthors(prefix)
	title := ""
	if suffix != "" {
		// Title is often the first sentence after the year.
		parts := strings.SplitN(strings.TrimLeft(suffix, ". ,;:"), ".", 2)
		if len(parts) > 0 {
			title = strings.TrimSpace(parts[0])
		}
	}
	return authors, title
}

func splitAuthors(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "&", "and")
	parts := strings.Split(s, " and ")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// If comma-separated authors, take each segment.
		if strings.Contains(p, ";") {
			for _, sub := range strings.Split(p, ";") {
				sub = strings.TrimSpace(sub)
				if sub != "" {
					out = append(out, sub)
				}
			}
			continue
		}
		out = append(out, p)
	}
	return out
}

func authorYearKey(e BibliographyEntry) string {
	if e.Year == nil || len(e.Authors) == 0 {
		return ""
	}
	first := strings.TrimSpace(e.Authors[0])
	if first == "" {
		return ""
	}
	surname := first
	if idx := strings.Index(first, ","); idx > 0 {
		surname = strings.TrimSpace(first[:idx])
	} else {
		parts := strings.Fields(first)
		if len(parts) > 0 {
			surname = parts[len(parts)-1]
		}
	}
	if surname == "" {
		return ""
	}
	return strings.ToLower(surname) + "|" + strconv.Itoa(*e.Year)
}

func isCapitalized(s string) bool {
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			return true
		}
		if r >= 'a' && r <= 'z' {
			return false
		}
	}
	return false
}

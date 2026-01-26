package outline

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

// Section represents a lightweight outline node extracted during ingestion.
type Section struct {
	Title     string
	Path      string
	StartPage *int
	EndPage   *int
	StartSec  *float64
	EndSec    *float64
	Children  []Section
}

// Outline is a best-effort structure hint for later signature building.
type Outline struct {
	Title      string
	Sections   []Section
	Source     string
	Confidence float64
}

// MaxSections reads FILE_SIGNATURE_MAX_SECTIONS to keep hints aligned with signature defaults.
func MaxSections() int {
	raw := strings.TrimSpace(os.Getenv("FILE_SIGNATURE_MAX_SECTIONS"))
	if raw == "" {
		return 48
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 48
	}
	return n
}

// ApplyHint stores an outline hint in the diagnostics map, preferring higher confidence.
func ApplyHint(diag map[string]any, hint *Outline) {
	if diag == nil || hint == nil || len(hint.Sections) == 0 {
		return
	}
	existingConf := floatFromAny(diag["outline_confidence"], 0)
	if existing, ok := diag["outline_hint"].(map[string]any); ok && existing != nil {
		if v, ok := existing["confidence"]; ok {
			existingConf = floatFromAny(v, existingConf)
		}
	}
	if existingConf >= hint.Confidence && existingConf > 0 {
		return
	}
	diag["outline_hint"] = hint.HintMap()
	diag["outline_confidence"] = hint.Confidence
	diag["outline_source"] = strings.TrimSpace(hint.Source)
}

// HintMap converts the outline to a JSON-friendly map.
func (o *Outline) HintMap() map[string]any {
	if o == nil {
		return nil
	}
	sections := make([]map[string]any, 0, len(o.Sections))
	for i, s := range o.Sections {
		sections = append(sections, map[string]any{
			"title":      strings.TrimSpace(s.Title),
			"path":       strings.TrimSpace(s.Path),
			"start_page": s.StartPage,
			"end_page":   s.EndPage,
			"start_sec":  s.StartSec,
			"end_sec":    s.EndSec,
			"children":   []map[string]any{},
		})
		if i+1 >= MaxSections() {
			break
		}
	}
	title := strings.TrimSpace(o.Title)
	if title == "" {
		title = "Document"
	}
	return map[string]any{
		"title":      title,
		"sections":   sections,
		"source":     strings.TrimSpace(o.Source),
		"confidence": o.Confidence,
	}
}

// FromSegments builds a heuristic outline from extracted segments.
func FromSegments(name string, segs []types.Segment, maxSections int) *Outline {
	if len(segs) == 0 {
		return nil
	}
	if maxSections <= 0 {
		maxSections = MaxSections()
	}
	title := strings.TrimSpace(filepath.Base(name))
	if title == "" {
		title = "Document"
	}

	if toc := extractTOCSections(segs, maxSections); len(toc) > 0 {
		return &Outline{
			Title:      title,
			Sections:   toc,
			Source:     "toc",
			Confidence: 0.78,
		}
	}

	headings := extractHeadingCandidates(segs, maxSections)
	if len(headings) > 0 {
		return &Outline{
			Title:      title,
			Sections:   headings,
			Source:     "headings",
			Confidence: 0.6,
		}
	}

	sections := buildTimeSections(segs, maxSections)
	if len(sections) > 0 {
		return &Outline{
			Title:      title,
			Sections:   sections,
			Source:     "transcript",
			Confidence: 0.35,
		}
	}

	return nil
}

type tocLine struct {
	Text      string
	StartPage *int
}

func extractTOCSections(segs []types.Segment, maxSections int) []Section {
	if len(segs) == 0 || maxSections <= 0 {
		return nil
	}
	lines := collectTOCLines(segs, 220)
	if len(lines) == 0 {
		return nil
	}

	start := -1
	for i, ln := range lines {
		if isTOCHeader(ln.Text) {
			start = i + 1
			break
		}
	}

	parseFrom := func(idx int) []Section {
		sections := make([]Section, 0, maxSections)
		misses := 0
		for i := idx; i < len(lines) && len(sections) < maxSections; i++ {
			txt := strings.TrimSpace(lines[i].Text)
			if txt == "" {
				if len(sections) > 0 {
					misses++
				}
				if misses >= 3 {
					break
				}
				continue
			}
			title, path, page, ok := parseTOCLine(txt)
			if !ok {
				if len(sections) > 0 {
					misses++
					if misses >= 3 {
						break
					}
				}
				continue
			}
			if path == "" {
				path = strconv.Itoa(len(sections) + 1)
			}
			if page == nil {
				page = lines[i].StartPage
			}
			sections = append(sections, Section{
				Title:     title,
				Path:      path,
				StartPage: page,
				EndPage:   page,
			})
		}
		if len(sections) < 4 {
			return nil
		}
		return sections
	}

	if start >= 0 {
		if sections := parseFrom(start); len(sections) > 0 {
			return sections
		}
	}

	// Fallback: look for dense dotted-leader patterns in early pages.
	denseStart := firstDenseTOCWindow(lines, 4)
	if denseStart >= 0 {
		if sections := parseFrom(denseStart); len(sections) > 0 {
			return sections
		}
	}

	return nil
}

func collectTOCLines(segs []types.Segment, maxLines int) []tocLine {
	if maxLines <= 0 {
		maxLines = 200
	}
	out := make([]tocLine, 0, maxLines)

	ordered := make([]types.Segment, 0, len(segs))
	for _, seg := range segs {
		if strings.TrimSpace(seg.Text) == "" {
			continue
		}
		ordered = append(ordered, seg)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		pi := ordered[i].Page
		pj := ordered[j].Page
		if pi != nil && pj != nil {
			if *pi == *pj {
				return i < j
			}
			return *pi < *pj
		}
		if pi != nil && pj == nil {
			return true
		}
		if pi == nil && pj != nil {
			return false
		}
		return i < j
	})

	for _, seg := range ordered {
		lines := strings.Split(seg.Text, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			out = append(out, tocLine{
				Text:      line,
				StartPage: seg.Page,
			})
			if len(out) >= maxLines {
				return out
			}
		}
	}
	return out
}

func firstDenseTOCWindow(lines []tocLine, minMatches int) int {
	if len(lines) == 0 || minMatches <= 0 {
		return -1
	}
	window := 24
	if len(lines) < window {
		window = len(lines)
	}
	for i := 0; i+window <= len(lines); i++ {
		matches := 0
		for j := i; j < i+window; j++ {
			if tocLinePattern.MatchString(lines[j].Text) {
				matches++
			}
		}
		if matches >= minMatches {
			return i
		}
	}
	return -1
}

func isTOCHeader(line string) bool {
	l := strings.ToLower(strings.TrimSpace(line))
	if l == "contents" || l == "table of contents" {
		return true
	}
	return strings.Contains(l, "table of contents")
}

func parseTOCLine(line string) (title string, path string, page *int, ok bool) {
	if line == "" {
		return "", "", nil, false
	}

	if m := tocLinePattern.FindStringSubmatch(line); len(m) > 0 {
		path = strings.TrimSpace(m[tocNumIdx])
		title = strings.TrimSpace(m[tocTitleIdx])
		pageNum := strings.TrimSpace(m[tocPageIdx])
		if n, err := strconv.Atoi(pageNum); err == nil {
			page = &n
		}
		if title != "" {
			return title, path, page, true
		}
	}

	return "", "", nil, false
}

func extractHeadingCandidates(segs []types.Segment, maxSections int) []Section {
	if len(segs) == 0 || maxSections <= 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]Section, 0, maxSections)

	for _, seg := range segs {
		text := strings.TrimSpace(seg.Text)
		if text == "" {
			continue
		}
		lines := strings.Split(text, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if !looksLikeHeading(line) {
				continue
			}
			key := strings.ToLower(line)
			if seen[key] {
				continue
			}
			seen[key] = true
			path := strconv.Itoa(len(out) + 1)
			out = append(out, Section{
				Title:     line,
				Path:      path,
				StartPage: seg.Page,
				EndPage:   seg.Page,
				StartSec:  seg.StartSec,
				EndSec:    seg.EndSec,
			})
			if len(out) >= maxSections {
				return out
			}
		}
	}
	return out
}

func buildTimeSections(segs []types.Segment, maxSections int) []Section {
	type timedSeg struct {
		Text     string
		StartSec float64
		EndSec   float64
	}
	timed := make([]timedSeg, 0, len(segs))
	for _, seg := range segs {
		if seg.StartSec == nil {
			continue
		}
		start := *seg.StartSec
		end := start
		if seg.EndSec != nil {
			end = *seg.EndSec
		}
		timed = append(timed, timedSeg{
			Text:     seg.Text,
			StartSec: start,
			EndSec:   end,
		})
	}
	if len(timed) == 0 {
		return nil
	}
	sort.Slice(timed, func(i, j int) bool { return timed[i].StartSec < timed[j].StartSec })

	if maxSections <= 0 {
		maxSections = MaxSections()
	}
	step := len(timed) / maxSections
	if step <= 0 {
		step = 1
	}

	sections := make([]Section, 0, maxSections)
	for i := 0; i < len(timed) && len(sections) < maxSections; i += step {
		seg := timed[i]
		title := shortTitle(seg.Text, len(sections)+1)
		start := seg.StartSec
		end := seg.EndSec
		if i+step < len(timed) {
			end = timed[i+step].StartSec
		}
		path := strconv.Itoa(len(sections) + 1)
		sections = append(sections, Section{
			Title:    title,
			Path:     path,
			StartSec: &start,
			EndSec:   &end,
		})
	}
	return sections
}

func shortTitle(text string, idx int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "Section " + strconv.Itoa(idx)
	}
	line := text
	if i := strings.Index(line, "\n"); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "Section " + strconv.Itoa(idx)
	}
	if len(line) > 80 {
		line = line[:80]
	}
	return line
}

func looksLikeHeading(line string) bool {
	if line == "" {
		return false
	}
	if len(line) < 4 || len(line) > 80 {
		return false
	}
	if strings.HasSuffix(line, ".") && len(line) < 10 {
		return false
	}
	upper := 0
	letters := 0
	for _, r := range line {
		if unicode.IsLetter(r) {
			letters++
			if unicode.IsUpper(r) {
				upper++
			}
		}
	}
	if letters == 0 {
		return false
	}
	if upper == letters {
		return true
	}
	if upper > 0 && float64(upper)/float64(letters) >= 0.6 {
		return true
	}
	if headingNumPrefix.MatchString(line) {
		return true
	}
	return false
}

var headingNumPrefix = regexp.MustCompile(`^(\d+(\.\d+)*|[IVX]+)\s+`)
var tocLinePattern = regexp.MustCompile(`^(?:(\d+(?:\.\d+)*|[IVXLC]+)[\.\)]?\s+)?(.+?)\s*\.{2,}\s*(\d+)\s*$`)

const (
	tocNumIdx   = 1
	tocTitleIdx = 2
	tocPageIdx  = 3
)

func floatFromAny(v any, def float64) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return def
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
	}
	return def
}

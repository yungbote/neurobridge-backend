package steps

import (
	"math"
	"strings"
	"unicode"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

type patchDocSignals struct {
	SampleChunks int
	SampleChars  int

	UniqueSections  int
	SectionDepthMax int
	SectionDepthAvg float64

	TokenCount       int
	UniqueTokenCount int
	LexicalDiversity float64

	CodeLineRatio float64
	SymbolDensity float64
}

const (
	patchSignalSampleMaxChunks  = 80
	patchSignalSampleMaxChars   = 120000
	patchSignalMaxUniqueTokens  = 8000
	patchSignalMaxSectionSample = 200
)

func computePatchDocSignals(chunks []*types.MaterialChunk) patchDocSignals {
	out := patchDocSignals{}
	if len(chunks) == 0 {
		return out
	}

	step := 1
	if len(chunks) > patchSignalSampleMaxChunks {
		step = int(math.Ceil(float64(len(chunks)) / float64(patchSignalSampleMaxChunks)))
		if step < 1 {
			step = 1
		}
	}

	sectionSet := map[string]struct{}{}
	uniqueTokens := map[string]struct{}{}
	var sectionDepthSum int
	var sectionDepthCount int
	var codeLines int
	var totalLines int
	var symbolChars int
	var totalChars int
	var totalTokens int

	for i := 0; i < len(chunks); i += step {
		ch := chunks[i]
		if ch == nil {
			continue
		}
		out.SampleChunks++

		meta := chunkMetaMap(ch)
		sec := strings.TrimSpace(stringFromAny(meta["section_path"]))
		if sec == "" {
			sec = strings.TrimSpace(stringFromAny(meta["section_title"]))
		}
		if sec != "" && len(sectionSet) < patchSignalMaxSectionSample {
			if _, ok := sectionSet[sec]; !ok {
				sectionSet[sec] = struct{}{}
			}
			depth := intFromAny(meta["section_depth"], 0)
			if depth <= 0 {
				depth = sectionDepthFromPathLabel(sec)
			}
			if depth > out.SectionDepthMax {
				out.SectionDepthMax = depth
			}
			if depth > 0 {
				sectionDepthSum += depth
				sectionDepthCount++
			}
		}

		text := ch.Text
		if text == "" {
			continue
		}
		if out.SampleChars+len(text) > patchSignalSampleMaxChars {
			remaining := patchSignalSampleMaxChars - out.SampleChars
			if remaining <= 0 {
				break
			}
			if remaining < len(text) {
				text = text[:remaining]
			}
		}
		out.SampleChars += len(text)

		lines := strings.Split(text, "\n")
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			totalLines++
			if isCodeLine(line) {
				codeLines++
			}
		}

		for _, r := range text {
			if unicode.IsSpace(r) {
				continue
			}
			totalChars++
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				symbolChars++
			}
		}

		for _, tok := range strings.Fields(text) {
			totalTokens++
			if len(uniqueTokens) < patchSignalMaxUniqueTokens {
				norm := patchNormalizeToken(tok)
				if norm != "" {
					uniqueTokens[norm] = struct{}{}
				}
			}
		}

		if out.SampleChars >= patchSignalSampleMaxChars {
			break
		}
	}

	out.UniqueSections = len(sectionSet)
	if sectionDepthCount > 0 {
		out.SectionDepthAvg = float64(sectionDepthSum) / float64(sectionDepthCount)
	}
	out.TokenCount = totalTokens
	out.UniqueTokenCount = len(uniqueTokens)
	if totalTokens > 0 {
		out.LexicalDiversity = float64(len(uniqueTokens)) / float64(totalTokens)
	}
	if totalLines > 0 {
		out.CodeLineRatio = float64(codeLines) / float64(totalLines)
	}
	if totalChars > 0 {
		out.SymbolDensity = float64(symbolChars) / float64(totalChars)
	}

	return out
}

func patchBreadthScale(signals AdaptiveSignals, doc patchDocSignals) float64 {
	breadth := float64(maxInt(signals.SectionCount, doc.UniqueSections))
	depth := float64(doc.SectionDepthMax)
	scale := 1.0
	switch {
	case breadth >= 50:
		scale += 0.45
	case breadth >= 35:
		scale += 0.35
	case breadth >= 20:
		scale += 0.25
	case breadth >= 10:
		scale += 0.15
	}
	if depth >= 4 {
		scale += 0.2
	} else if depth >= 3 {
		scale += 0.1
	}
	if signals.FileCount >= 4 {
		scale += 0.15
	} else if signals.FileCount >= 2 {
		scale += 0.1
	}
	if signals.PageCount >= 800 {
		scale += 0.2
	} else if signals.PageCount >= 400 {
		scale += 0.1
	}
	return clampFloatCeiling(scale, 1.0, 2.2)
}

func patchComplexityScale(doc patchDocSignals) float64 {
	scale := 1.0
	if doc.LexicalDiversity >= 0.35 {
		scale += 0.2
	} else if doc.LexicalDiversity >= 0.25 {
		scale += 0.12
	}
	if doc.CodeLineRatio >= 0.35 {
		scale += 0.3
	} else if doc.CodeLineRatio >= 0.2 {
		scale += 0.2
	} else if doc.CodeLineRatio >= 0.12 {
		scale += 0.1
	}
	if doc.SymbolDensity >= 0.2 {
		scale += 0.2
	} else if doc.SymbolDensity >= 0.12 {
		scale += 0.1
	}
	return clampFloatCeiling(scale, 1.0, 2.0)
}

func sectionDepthFromPathLabel(label string) int {
	raw := strings.TrimSpace(label)
	if raw == "" {
		return 1
	}
	head := raw
	if idx := strings.IndexAny(raw, " \t"); idx >= 0 {
		head = raw[:idx]
	}
	head = strings.TrimSpace(head)
	if head == "" {
		return 1
	}
	if strings.Contains(head, ".") {
		parts := strings.Split(head, ".")
		return len(parts)
	}
	return 1
}

func patchNormalizeToken(tok string) string {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return ""
	}
	tok = strings.TrimFunc(tok, func(r rune) bool {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
		return r != '_' && r != ':'
	})
	return strings.ToLower(strings.TrimSpace(tok))
}

func isCodeLine(line string) bool {
	l := strings.TrimSpace(line)
	if l == "" {
		return false
	}
	if strings.HasPrefix(l, "//") || strings.HasPrefix(l, "/*") || strings.HasPrefix(l, "*") {
		return true
	}
	if strings.Contains(l, "#include") || strings.Contains(l, "#define") {
		return true
	}
	if strings.ContainsAny(l, "{};") {
		return true
	}
	if strings.Contains(l, "::") || strings.Contains(l, "->") || strings.Contains(l, "std::") {
		return true
	}
	if strings.HasPrefix(l, "template ") || strings.HasPrefix(l, "class ") || strings.HasPrefix(l, "struct ") {
		return true
	}
	if strings.Contains(l, "operator") {
		return true
	}
	return false
}

func scaleCeiling(base int, scale float64, maxScale float64) int {
	if base <= 0 {
		return base
	}
	if scale <= 1.0 {
		return base
	}
	if maxScale <= 1.0 {
		maxScale = 1.0
	}
	adj := scale
	if adj > maxScale {
		adj = maxScale
	}
	scaled := int(math.Round(float64(base) * adj))
	if scaled < base {
		return base
	}
	return scaled
}

func scaleInt(base int, scale float64, min int) int {
	if base < min {
		base = min
	}
	if scale <= 1.0 {
		return base
	}
	scaled := int(math.Round(float64(base) * scale))
	if scaled < min {
		return min
	}
	return scaled
}

func roundTo(v float64, places int) float64 {
	if places <= 0 {
		return math.Round(v)
	}
	pow := math.Pow(10, float64(places))
	return math.Round(v*pow) / pow
}

package outline

import (
	"strings"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

// AnnotateSegmentsWithOutline tags segments with section metadata when page ranges are available.
// This preserves hierarchical structure without changing existing extraction behavior.
func AnnotateSegmentsWithOutline(segs []types.Segment, hint *Outline) []types.Segment {
	if hint == nil || len(hint.Sections) == 0 || len(segs) == 0 {
		return segs
	}

	sections := normalizeSectionRanges(hint.Sections)
	if len(sections) == 0 {
		return segs
	}

	for i := range segs {
		p := segs[i].Page
		if p == nil {
			continue
		}
		best := -1
		bestDepth := -1
		for si, sec := range sections {
			if sec.StartPage == nil || sec.EndPage == nil {
				continue
			}
			if *p < *sec.StartPage || *p > *sec.EndPage {
				continue
			}
			d := sectionDepth(sec.Path)
			if d > bestDepth {
				bestDepth = d
				best = si
			}
		}
		if best < 0 {
			continue
		}
		sec := sections[best]
		if segs[i].Metadata == nil {
			segs[i].Metadata = map[string]any{}
		}
		segs[i].Metadata["section_title"] = strings.TrimSpace(sec.Title)
		segs[i].Metadata["section_path"] = sectionPathLabel(sec)
		segs[i].Metadata["section_depth"] = sectionDepth(sec.Path)
		segs[i].Metadata["section_index"] = best + 1
		if strings.TrimSpace(hint.Source) != "" {
			segs[i].Metadata["section_source"] = strings.TrimSpace(hint.Source)
		}
	}
	return segs
}

func normalizeSectionRanges(in []Section) []Section {
	sections := make([]Section, 0, len(in))
	for _, s := range in {
		sec := s
		if sec.StartPage == nil && sec.EndPage == nil {
			continue
		}
		sections = append(sections, sec)
	}
	if len(sections) == 0 {
		return nil
	}

	for i := range sections {
		if sections[i].StartPage == nil {
			continue
		}
		if sections[i].EndPage != nil {
			continue
		}
		for j := i + 1; j < len(sections); j++ {
			if sections[j].StartPage == nil {
				continue
			}
			end := *sections[j].StartPage - 1
			if end < *sections[i].StartPage {
				end = *sections[i].StartPage
			}
			sections[i].EndPage = &end
			break
		}
		if sections[i].EndPage == nil {
			sections[i].EndPage = sections[i].StartPage
		}
	}
	return sections
}

func sectionDepth(path string) int {
	p := strings.TrimSpace(path)
	if p == "" {
		return 1
	}
	return len(strings.Split(p, "."))
}

func sectionPathLabel(sec Section) string {
	title := strings.TrimSpace(sec.Title)
	path := strings.TrimSpace(sec.Path)
	if path == "" {
		return title
	}
	if title == "" {
		return path
	}
	return strings.TrimSpace(path + " " + title)
}

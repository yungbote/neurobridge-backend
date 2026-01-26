package outline

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type docxParagraph struct {
	Style string
	Text  string
}

// FromDocxFile extracts heading-style paragraphs as an outline hint.
func FromDocxFile(path string, maxSections int) (*Outline, error) {
	rc, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	body, err := readZipFile(rc.File, "word/document.xml")
	if err != nil {
		return nil, err
	}

	paras := extractDocxParagraphs(body)
	if len(paras) == 0 {
		return nil, nil
	}

	sections := make([]Section, 0, maxSections)
	for _, p := range paras {
		if len(sections) >= maxSections {
			break
		}
		if !isHeadingStyle(p.Style) {
			continue
		}
		title := strings.TrimSpace(p.Text)
		if title == "" {
			continue
		}
		sections = append(sections, Section{
			Title: title,
			Path:  strconv.Itoa(len(sections) + 1),
		})
	}
	if len(sections) == 0 {
		return nil, nil
	}

	return &Outline{
		Title:      strings.TrimSpace(filepath.Base(path)),
		Sections:   sections,
		Source:     "docx_headings",
		Confidence: 0.7,
	}, nil
}

// FromPptxFile extracts slide titles (first text per slide) as an outline hint.
func FromPptxFile(path string, maxSections int) (*Outline, error) {
	rc, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	slideFiles := findZipFiles(rc.File, "ppt/slides/slide", ".xml")
	if len(slideFiles) == 0 {
		return nil, nil
	}
	sort.Strings(slideFiles)

	sections := make([]Section, 0, maxSections)
	for i, name := range slideFiles {
		if len(sections) >= maxSections {
			break
		}
		raw, err := readZipFile(rc.File, name)
		if err != nil {
			continue
		}
		title := strings.TrimSpace(extractFirstText(raw))
		if title == "" {
			title = fmt.Sprintf("Slide %d", i+1)
		}
		page := i + 1
		sections = append(sections, Section{
			Title:     title,
			Path:      strconv.Itoa(len(sections) + 1),
			StartPage: &page,
			EndPage:   &page,
		})
	}
	if len(sections) == 0 {
		return nil, nil
	}
	return &Outline{
		Title:      strings.TrimSpace(filepath.Base(path)),
		Sections:   sections,
		Source:     "pptx_slides",
		Confidence: 0.5,
	}, nil
}

func readZipFile(files []*zip.File, target string) ([]byte, error) {
	for _, f := range files {
		if f == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(f.Name), strings.TrimSpace(target)) {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("file not found: %s", target)
}

func findZipFiles(files []*zip.File, prefix string, suffix string) []string {
	out := []string{}
	for _, f := range files {
		if f == nil {
			continue
		}
		name := strings.TrimSpace(f.Name)
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) && strings.HasSuffix(strings.ToLower(name), strings.ToLower(suffix)) {
			out = append(out, name)
		}
	}
	return out
}

func extractDocxParagraphs(body []byte) []docxParagraph {
	if len(body) == 0 {
		return nil
	}
	dec := xml.NewDecoder(strings.NewReader(string(body)))
	var (
		inParagraph bool
		inText      bool
		style       string
		text        strings.Builder
		out         []docxParagraph
	)

	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return out
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				inParagraph = true
				inText = false
				style = ""
				text.Reset()
			case "pStyle":
				if !inParagraph {
					continue
				}
				for _, a := range t.Attr {
					if strings.EqualFold(a.Name.Local, "val") {
						style = strings.TrimSpace(a.Value)
					}
				}
			case "t":
				if inParagraph {
					inText = true
				}
			}
		case xml.CharData:
			if inParagraph && inText {
				text.WriteString(string(t))
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				if inParagraph {
					txt := strings.TrimSpace(text.String())
					out = append(out, docxParagraph{
						Style: style,
						Text:  txt,
					})
				}
				inParagraph = false
				inText = false
				style = ""
				text.Reset()
			}
		}
	}
	return out
}

func extractFirstText(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	dec := xml.NewDecoder(strings.NewReader(string(body)))
	inText := false
	var b strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" {
				inText = true
			}
		case xml.CharData:
			if inText {
				b.WriteString(string(t))
				if strings.TrimSpace(b.String()) != "" {
					return strings.TrimSpace(b.String())
				}
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inText = false
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func isHeadingStyle(style string) bool {
	style = strings.ToLower(strings.TrimSpace(style))
	if style == "" {
		return false
	}
	if strings.HasPrefix(style, "heading") {
		return true
	}
	if style == "title" || style == "subtitle" {
		return true
	}
	return false
}

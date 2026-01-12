package handlers

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func buildChunkExcerpts(byID map[uuid.UUID]*types.MaterialChunk, ids []uuid.UUID, maxLines int, maxChars int) string {
	if maxLines <= 0 {
		maxLines = 12
	}
	if maxChars <= 0 {
		maxChars = 700
	}
	var b strings.Builder
	n := 0
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		ch := byID[id]
		if ch == nil {
			continue
		}
		txt := strings.TrimSpace(ch.Text)
		if txt == "" {
			continue
		}
		if len(txt) > maxChars {
			txt = txt[:maxChars] + "..."
		}
		b.WriteString("[chunk_id=")
		b.WriteString(id.String())
		b.WriteString("] ")
		b.WriteString(txt)
		b.WriteString("\n")
		n++
		if n >= maxLines {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

func extractChunkIDsFromNodeDocJSON(raw datatypes.JSON) []uuid.UUID {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	blocks, _ := obj["blocks"].([]any)
	out := make([]uuid.UUID, 0)
	seen := map[uuid.UUID]bool{}
	for _, b := range blocks {
		m, ok := b.(map[string]any)
		if !ok {
			continue
		}
		for _, c := range stringSliceFromAny(extractChunkIDsFromCitations(m["citations"])) {
			if id, err := uuid.Parse(strings.TrimSpace(c)); err == nil && id != uuid.Nil && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

func extractChunkIDsFromCitations(raw any) []string {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(stringFromAny(m["chunk_id"]))
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

func dedupeUUIDsLocal(in []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(in))
	for _, id := range in {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func uuidStrings(in []uuid.UUID) []string {
	out := make([]string, 0, len(in))
	for _, id := range in {
		if id != uuid.Nil {
			out = append(out, id.String())
		}
	}
	return out
}

func contentJSONToMarkdownAndCitations(raw []byte) (string, string) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", ""
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", ""
	}

	// citations is optional in ContentJSONSchema but is expected in generated outputs.
	citations := []string{}
	if v, ok := obj["citations"]; ok {
		citations = append(citations, stringSliceFromAny(v)...)
	}

	blocksAny, _ := obj["blocks"].([]any)
	var b strings.Builder
	for _, rawBlock := range blocksAny {
		m, ok := rawBlock.(map[string]any)
		if !ok || m == nil {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(stringFromAny(m["kind"])))
		content := strings.TrimSpace(stringFromAny(m["content_md"]))
		items := stringSliceFromAny(m["items"])
		assetRefs := stringSliceFromAny(m["asset_refs"])

		switch kind {
		case "heading":
			if content != "" {
				b.WriteString("## ")
				b.WriteString(content)
				b.WriteString("\n\n")
			}
		case "paragraph", "callout":
			if content != "" {
				b.WriteString(content)
				b.WriteString("\n\n")
			}
		case "bullets":
			for _, it := range items {
				it = strings.TrimSpace(it)
				if it == "" {
					continue
				}
				b.WriteString("- ")
				b.WriteString(it)
				b.WriteString("\n")
			}
			if len(items) > 0 {
				b.WriteString("\n")
			}
		case "steps":
			n := 0
			for _, it := range items {
				it = strings.TrimSpace(it)
				if it == "" {
					continue
				}
				n++
				b.WriteString(fmt.Sprintf("%d. %s\n", n, it))
			}
			if n > 0 {
				b.WriteString("\n")
			}
		case "divider":
			b.WriteString("\n---\n\n")
		case "image":
			if len(assetRefs) > 0 {
				b.WriteString(fmt.Sprintf("[image: %s]\n\n", assetRefs[0]))
			}
		case "video_embed":
			if len(assetRefs) > 0 {
				b.WriteString(fmt.Sprintf("[video: %s]\n\n", assetRefs[0]))
			}
		default:
			if content != "" {
				b.WriteString(content)
				b.WriteString("\n\n")
			}
		}
	}

	md := strings.TrimSpace(b.String())
	csv := strings.Join(dedupeStrings(citations), ", ")
	return md, csv
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(v)
	}
}

func stringSliceFromAny(v any) []string {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, it := range t {
			s := strings.TrimSpace(stringFromAny(it))
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]bool, len(in))
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

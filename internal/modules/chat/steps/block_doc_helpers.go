package steps

import (
	"fmt"
	"strings"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func blockTextForContext(block map[string]any) (string, string) {
	if block == nil {
		return "", ""
	}
	blockType := strings.ToLower(stringFromAnyCtx(block["type"]))
	switch blockType {
	case "heading":
		return blockType, strings.TrimSpace(stringFromAnyCtx(block["text"]))
	case "paragraph":
		return blockType, strings.TrimSpace(stringFromAnyCtx(block["md"]))
	case "callout":
		return blockType, strings.TrimSpace(stringFromAnyCtx(block["title"]) + " " + stringFromAnyCtx(block["md"]))
	case "code":
		return blockType, strings.TrimSpace(stringFromAnyCtx(block["code"]))
	case "figure":
		return blockType, strings.TrimSpace(stringFromAnyCtx(block["caption"]))
	case "video":
		return blockType, strings.TrimSpace(stringFromAnyCtx(block["caption"]))
	case "diagram":
		return blockType, strings.TrimSpace(stringFromAnyCtx(block["caption"]) + " " + stringFromAnyCtx(block["source"]))
	case "table":
		return blockType, strings.TrimSpace(stringFromAnyCtx(block["caption"]))
	case "quick_check":
		return blockType, strings.TrimSpace(stringFromAnyCtx(block["prompt_md"]) + " " + stringFromAnyCtx(block["answer_md"]))
	case "objectives", "prerequisites", "key_takeaways", "common_mistakes", "misconceptions", "edge_cases", "heuristics", "checklist", "connections":
		items, _ := block["items_md"].([]any)
		parts := make([]string, 0, len(items))
		for _, it := range items {
			parts = append(parts, stringFromAnyCtx(it))
		}
		return blockType, strings.TrimSpace(stringFromAnyCtx(block["title"]) + " " + strings.Join(parts, " "))
	case "steps":
		items, _ := block["steps_md"].([]any)
		parts := make([]string, 0, len(items))
		for _, it := range items {
			parts = append(parts, stringFromAnyCtx(it))
		}
		return blockType, strings.TrimSpace(stringFromAnyCtx(block["title"]) + " " + strings.Join(parts, " "))
	case "glossary":
		var b strings.Builder
		b.WriteString(strings.TrimSpace(stringFromAnyCtx(block["title"])))
		b.WriteString(" ")
		if arr, ok := block["terms"].([]any); ok {
			for _, it := range arr {
				m, ok := it.(map[string]any)
				if !ok {
					continue
				}
				b.WriteString(stringFromAnyCtx(m["term"]))
				b.WriteString(" ")
				b.WriteString(stringFromAnyCtx(m["definition_md"]))
				b.WriteString(" ")
			}
		}
		return blockType, strings.TrimSpace(b.String())
	case "faq":
		var b strings.Builder
		b.WriteString(strings.TrimSpace(stringFromAnyCtx(block["title"])))
		b.WriteString(" ")
		if arr, ok := block["qas"].([]any); ok {
			for _, it := range arr {
				m, ok := it.(map[string]any)
				if !ok {
					continue
				}
				b.WriteString(stringFromAnyCtx(m["question_md"]))
				b.WriteString(" ")
				b.WriteString(stringFromAnyCtx(m["answer_md"]))
				b.WriteString(" ")
			}
		}
		return blockType, strings.TrimSpace(b.String())
	case "intuition", "mental_model", "why_it_matters":
		return blockType, strings.TrimSpace(stringFromAnyCtx(block["title"]) + " " + stringFromAnyCtx(block["md"]))
	default:
		return blockType, ""
	}
}

func blockTitleForContext(block map[string]any) string {
	if block == nil {
		return ""
	}
	if title := strings.TrimSpace(stringFromAnyCtx(block["title"])); title != "" {
		return title
	}
	if text := strings.TrimSpace(stringFromAnyCtx(block["text"])); text != "" {
		return text
	}
	if label := strings.TrimSpace(stringFromAnyCtx(block["label"])); label != "" {
		return label
	}
	return ""
}

func buildBlockDocBody(node *types.PathNode, blockID string, block map[string]any) (string, string, string, string) {
	blockType, body := blockTextForContext(block)
	title := blockTitleForContext(block)
	body = strings.TrimSpace(body)
	if body == "" {
		return "", "", blockType, title
	}
	unitTitle := "Untitled unit"
	if node != nil && strings.TrimSpace(node.Title) != "" {
		unitTitle = strings.TrimSpace(node.Title)
	}
	unitIndex := 0
	if node != nil {
		unitIndex = node.Index
	}
	if strings.TrimSpace(blockID) == "" {
		blockID = "(unknown)"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Unit %d: %s\n", unitIndex, unitTitle))
	b.WriteString("Block ID: " + strings.TrimSpace(blockID) + "\n")
	if blockType != "" {
		b.WriteString("Block Type: " + blockType + "\n")
	}
	if title != "" {
		b.WriteString("Title: " + title + "\n")
	}
	b.WriteString("\n")
	b.WriteString(body)
	text := strings.TrimSpace(b.String())
	contextual := "Unit block (retrieval context):\n" + text
	return text, contextual, blockType, title
}

func parseBlockIDFromText(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "block id:") {
			return strings.TrimSpace(trimmed[len("block id:"):])
		}
		if strings.HasPrefix(lower, "block_id:") {
			return strings.TrimSpace(trimmed[len("block_id:"):])
		}
	}
	return ""
}

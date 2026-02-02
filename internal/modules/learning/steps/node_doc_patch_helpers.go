package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
)

func findBlockIndex(blocks []map[string]any, blockID string, blockIndex int) (int, string) {
	if blockID != "" {
		for i, b := range blocks {
			if b == nil {
				continue
			}
			if strings.TrimSpace(stringFromAny(b["id"])) == blockID {
				return i, blockID
			}
		}
	}
	if blockIndex >= 0 && blockIndex < len(blocks) {
		id := strings.TrimSpace(stringFromAny(blocks[blockIndex]["id"]))
		return blockIndex, id
	}
	// Fallback: allow 1-based indexing from callers.
	if blockIndex > 0 && blockIndex-1 < len(blocks) {
		idx := blockIndex - 1
		id := strings.TrimSpace(stringFromAny(blocks[idx]["id"]))
		return idx, id
	}
	return -1, ""
}

func buildBlockPatchPrompt(doc content.NodeDocV1, blockType string, blockID string, block map[string]any, in NodeDocPatchInput, policy string, allowed map[string]bool, excerpts string) string {
	blockJSON, _ := json.Marshal(block)

	allowedIDs := make([]string, 0, len(allowed))
	for id := range allowed {
		allowedIDs = append(allowedIDs, id)
	}
	sort.Strings(allowedIDs)

	selectionText := ""
	if strings.TrimSpace(in.Selection.Text) != "" || in.Selection.Start != 0 || in.Selection.End != 0 {
		selectionText = fmt.Sprintf("text=%q start=%d end=%d", strings.TrimSpace(in.Selection.Text), in.Selection.Start, in.Selection.End)
	}

	var b strings.Builder
	b.WriteString("DOC_TITLE: ")
	b.WriteString(strings.TrimSpace(doc.Title))
	b.WriteString("\nDOC_SUMMARY: ")
	b.WriteString(strings.TrimSpace(doc.Summary))
	b.WriteString("\nBLOCK_TYPE: ")
	b.WriteString(blockType)
	b.WriteString("\nBLOCK_ID: ")
	b.WriteString(blockID)
	b.WriteString("\nBLOCK_JSON:\n")
	b.Write(blockJSON)
	b.WriteString("\nINSTRUCTION:\n")
	b.WriteString(strings.TrimSpace(in.Instruction))
	b.WriteString("\nSELECTION:\n")
	b.WriteString(selectionText)
	b.WriteString("\nCITATION_POLICY: ")
	b.WriteString(policy)
	b.WriteString("\nALLOWED_CHUNK_IDS:\n")
	b.WriteString(strings.Join(allowedIDs, "\n"))
	b.WriteString("\nEXCERPTS:\n")
	b.WriteString(excerpts)
	return strings.TrimSpace(b.String())
}

func blockPatchSchema(blockType string) (map[string]any, error) {
	citationSchema := map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"chunk_id": map[string]any{"type": "string"},
				"quote":    map[string]any{"type": "string"},
				"loc": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"page":  map[string]any{"type": "integer"},
						"start": map[string]any{"type": "integer"},
						"end":   map[string]any{"type": "integer"},
					},
					"required":             []any{"page", "start", "end"},
					"additionalProperties": false,
				},
			},
			"required":             []any{"chunk_id", "quote", "loc"},
			"additionalProperties": false,
		},
	}

	base := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":   map[string]any{"type": "string"},
			"type": map[string]any{"type": "string"},
		},
		"required":             []any{"id", "type"},
		"additionalProperties": false,
	}

	switch blockType {
	case "heading":
		base["properties"].(map[string]any)["level"] = map[string]any{"type": "integer"}
		base["properties"].(map[string]any)["text"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "heading"}
		base["required"] = append(base["required"].([]any), "level", "text")
		return base, nil
	case "paragraph":
		base["properties"].(map[string]any)["md"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "paragraph"}
		base["required"] = append(base["required"].([]any), "md", "citations")
		return base, nil
	case "callout":
		base["properties"].(map[string]any)["variant"] = map[string]any{"type": "string", "enum": []any{"info", "tip", "warning"}}
		base["properties"].(map[string]any)["title"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["md"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "callout"}
		base["required"] = append(base["required"].([]any), "variant", "title", "md", "citations")
		return base, nil
	case "diagram":
		base["properties"].(map[string]any)["kind"] = map[string]any{"type": "string", "enum": []any{"svg", "mermaid"}}
		base["properties"].(map[string]any)["source"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["caption"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "diagram"}
		base["required"] = append(base["required"].([]any), "kind", "source", "caption", "citations")
		return base, nil
	case "table":
		base["properties"].(map[string]any)["caption"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["columns"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		base["properties"].(map[string]any)["rows"] = map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "table"}
		base["required"] = append(base["required"].([]any), "caption", "columns", "rows", "citations")
		return base, nil
	case "quick_check":
		base["properties"].(map[string]any)["prompt_md"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["answer_md"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "quick_check"}
		base["required"] = append(base["required"].([]any), "prompt_md", "answer_md", "citations")
		return base, nil
	case "flashcard":
		base["properties"].(map[string]any)["front_md"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["back_md"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["concept_keys"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "flashcard"}
		base["required"] = append(base["required"].([]any), "front_md", "back_md", "citations")
		return base, nil
	case "figure":
		base["properties"].(map[string]any)["caption"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "figure"}
		base["required"] = append(base["required"].([]any), "caption", "citations")
		return base, nil
	case "video":
		base["properties"].(map[string]any)["caption"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "video"}
		base["required"] = append(base["required"].([]any), "caption")
		return base, nil
	case "objectives", "prerequisites", "key_takeaways", "common_mistakes", "misconceptions", "edge_cases", "heuristics", "checklist", "connections":
		base["properties"].(map[string]any)["title"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["items_md"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": blockType}
		base["required"] = append(base["required"].([]any), "title", "items_md", "citations")
		return base, nil
	case "steps":
		base["properties"].(map[string]any)["title"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["steps_md"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "steps"}
		base["required"] = append(base["required"].([]any), "title", "steps_md", "citations")
		return base, nil
	case "glossary":
		base["properties"].(map[string]any)["title"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["terms"] = map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"term":          map[string]any{"type": "string"},
					"definition_md": map[string]any{"type": "string"},
				},
				"required":             []any{"term", "definition_md"},
				"additionalProperties": false,
			},
		}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "glossary"}
		base["required"] = append(base["required"].([]any), "title", "terms", "citations")
		return base, nil
	case "faq":
		base["properties"].(map[string]any)["title"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["qas"] = map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"question_md": map[string]any{"type": "string"},
					"answer_md":   map[string]any{"type": "string"},
				},
				"required":             []any{"question_md", "answer_md"},
				"additionalProperties": false,
			},
		}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": "faq"}
		base["required"] = append(base["required"].([]any), "title", "qas", "citations")
		return base, nil
	case "intuition", "mental_model", "why_it_matters":
		base["properties"].(map[string]any)["title"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["md"] = map[string]any{"type": "string"}
		base["properties"].(map[string]any)["citations"] = citationSchema
		base["properties"].(map[string]any)["type"] = map[string]any{"type": "string", "const": blockType}
		base["required"] = append(base["required"].([]any), "title", "md", "citations")
		return base, nil
	default:
		return nil, fmt.Errorf("node_doc_patch: unsupported block type %q", blockType)
	}
}

func applyBlockPatch(blockType, blockID string, existing map[string]any, obj map[string]any) (map[string]any, error) {
	if existing == nil {
		return nil, fmt.Errorf("node_doc_patch: missing block")
	}
	updated := map[string]any{}
	for k, v := range existing {
		updated[k] = v
	}
	updated["id"] = blockID
	updated["type"] = blockType

	switch blockType {
	case "heading":
		updated["text"] = strings.TrimSpace(stringFromAny(obj["text"]))
		updated["level"] = intFromAny(obj["level"], intFromAny(existing["level"], 2))
	case "paragraph":
		updated["md"] = strings.TrimSpace(stringFromAny(obj["md"]))
		updated["citations"] = normalizeCitations(obj["citations"])
	case "callout":
		updated["variant"] = strings.TrimSpace(stringFromAny(obj["variant"]))
		updated["title"] = strings.TrimSpace(stringFromAny(obj["title"]))
		updated["md"] = strings.TrimSpace(stringFromAny(obj["md"]))
		updated["citations"] = normalizeCitations(obj["citations"])
	case "diagram":
		updated["kind"] = strings.TrimSpace(stringFromAny(obj["kind"]))
		updated["source"] = strings.TrimSpace(stringFromAny(obj["source"]))
		updated["caption"] = strings.TrimSpace(stringFromAny(obj["caption"]))
		updated["citations"] = normalizeCitations(obj["citations"])
	case "table":
		updated["caption"] = strings.TrimSpace(stringFromAny(obj["caption"]))
		updated["columns"] = stringSliceFromAny(obj["columns"])
		updated["rows"] = normalizeRows(obj["rows"])
		updated["citations"] = normalizeCitations(obj["citations"])
	case "quick_check":
		updated["prompt_md"] = strings.TrimSpace(stringFromAny(obj["prompt_md"]))
		updated["answer_md"] = strings.TrimSpace(stringFromAny(obj["answer_md"]))
		updated["citations"] = normalizeCitations(obj["citations"])
	case "figure":
		updated["caption"] = strings.TrimSpace(stringFromAny(obj["caption"]))
		updated["citations"] = normalizeCitations(obj["citations"])
	case "video":
		updated["caption"] = strings.TrimSpace(stringFromAny(obj["caption"]))
	case "objectives", "prerequisites", "key_takeaways", "common_mistakes", "misconceptions", "edge_cases", "heuristics", "checklist", "connections":
		updated["title"] = strings.TrimSpace(stringFromAny(obj["title"]))
		updated["items_md"] = stringSliceFromAny(obj["items_md"])
		updated["citations"] = normalizeCitations(obj["citations"])
	case "steps":
		updated["title"] = strings.TrimSpace(stringFromAny(obj["title"]))
		updated["steps_md"] = stringSliceFromAny(obj["steps_md"])
		updated["citations"] = normalizeCitations(obj["citations"])
	case "glossary":
		updated["title"] = strings.TrimSpace(stringFromAny(obj["title"]))
		updated["terms"] = normalizeAnyArray(obj["terms"])
		updated["citations"] = normalizeCitations(obj["citations"])
	case "faq":
		updated["title"] = strings.TrimSpace(stringFromAny(obj["title"]))
		updated["qas"] = normalizeAnyArray(obj["qas"])
		updated["citations"] = normalizeCitations(obj["citations"])
	case "intuition", "mental_model", "why_it_matters":
		updated["title"] = strings.TrimSpace(stringFromAny(obj["title"]))
		updated["md"] = strings.TrimSpace(stringFromAny(obj["md"]))
		updated["citations"] = normalizeCitations(obj["citations"])
	default:
		return nil, fmt.Errorf("node_doc_patch: unsupported block type %q", blockType)
	}
	return updated, nil
}

func normalizeAnyArray(raw any) []any {
	if raw == nil {
		return []any{}
	}
	if arr, ok := raw.([]any); ok {
		return arr
	}
	return []any{}
}

func normalizeCitations(raw any) []any {
	arr, ok := raw.([]any)
	if !ok || arr == nil {
		return []any{}
	}
	return arr
}

func normalizeRows(raw any) []any {
	if raw == nil {
		return []any{}
	}
	if arr, ok := raw.([]any); ok {
		return arr
	}
	rows, ok := raw.([][]string)
	if !ok {
		return []any{}
	}
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		cell := make([]any, 0, len(row))
		for _, c := range row {
			cell = append(cell, c)
		}
		out = append(out, cell)
	}
	return out
}

func stringMatrixFromAny(v any) [][]string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([][]string, 0, len(arr))
	for _, row := range arr {
		out = append(out, stringSliceFromAny(row))
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
	return dedupeStrings(out)
}

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

func nodeGoalFromMeta(raw datatypes.JSON) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		return ""
	}
	return strings.TrimSpace(stringFromAny(meta["goal"]))
}

func nodeConceptKeysFromMeta(raw datatypes.JSON) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil
	}
	return dedupeStrings(stringSliceFromAny(meta["concept_keys"]))
}

func instructionText(s string) string {
	return strings.TrimSpace(s)
}

func extractTextItems(val any) []string {
	switch t := val.(type) {
	case nil:
		return nil
	case []string:
		out := make([]string, 0, len(t))
		for _, v := range t {
			if s := strings.TrimSpace(v); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(t))
		for _, it := range t {
			switch v := it.(type) {
			case string:
				if s := strings.TrimSpace(v); s != "" {
					out = append(out, s)
				}
			case map[string]any:
				for _, key := range []string{"md", "text", "value", "item", "label", "title"} {
					if s := strings.TrimSpace(stringFromAny(v[key])); s != "" {
						out = append(out, s)
						break
					}
				}
			default:
				if s := strings.TrimSpace(fmt.Sprint(v)); s != "" {
					out = append(out, s)
				}
			}
		}
		return out
	default:
		if s := strings.TrimSpace(fmt.Sprint(t)); s != "" {
			return []string{s}
		}
	}
	return nil
}

func extractListItems(block map[string]any, keys ...string) []string {
	for _, key := range keys {
		if items := extractTextItems(block[key]); len(items) > 0 {
			return items
		}
	}
	return nil
}

func blockTextForQuery(blockType string, block map[string]any) string {
	switch blockType {
	case "heading":
		return strings.TrimSpace(stringFromAny(block["text"]))
	case "paragraph":
		return strings.TrimSpace(stringFromAny(block["md"]))
	case "callout":
		return strings.TrimSpace(stringFromAny(block["title"]) + " " + stringFromAny(block["md"]))
	case "code":
		return strings.TrimSpace(stringFromAny(block["code"]))
	case "figure":
		return strings.TrimSpace(stringFromAny(block["caption"]))
	case "video":
		return strings.TrimSpace(stringFromAny(block["caption"]))
	case "diagram":
		return strings.TrimSpace(stringFromAny(block["caption"]) + " " + stringFromAny(block["source"]))
	case "table":
		return strings.TrimSpace(stringFromAny(block["caption"]))
	case "quick_check":
		return strings.TrimSpace(stringFromAny(block["prompt_md"]) + " " + stringFromAny(block["answer_md"]))
	case "objectives", "prerequisites", "key_takeaways", "common_mistakes", "misconceptions", "edge_cases", "heuristics", "checklist", "connections":
		items := extractListItems(block, "items_md", "items", "items_text", "items_md_list")
		return strings.TrimSpace(stringFromAny(block["title"]) + " " + strings.Join(items, " "))
	case "steps":
		items := extractListItems(block, "steps_md", "steps", "steps_text")
		return strings.TrimSpace(stringFromAny(block["title"]) + " " + strings.Join(items, " "))
	case "glossary":
		var b strings.Builder
		b.WriteString(strings.TrimSpace(stringFromAny(block["title"])))
		b.WriteString(" ")
		if arr, ok := block["terms"].([]any); ok {
			for _, it := range arr {
				m, ok := it.(map[string]any)
				if !ok {
					continue
				}
				b.WriteString(stringFromAny(m["term"]))
				b.WriteString(" ")
				b.WriteString(stringFromAny(m["definition_md"]))
				b.WriteString(" ")
			}
		}
		return strings.TrimSpace(b.String())
	case "faq":
		var b strings.Builder
		b.WriteString(strings.TrimSpace(stringFromAny(block["title"])))
		b.WriteString(" ")
		if arr, ok := block["qas"].([]any); ok {
			for _, it := range arr {
				m, ok := it.(map[string]any)
				if !ok {
					continue
				}
				b.WriteString(stringFromAny(m["question_md"]))
				b.WriteString(" ")
				b.WriteString(stringFromAny(m["answer_md"]))
				b.WriteString(" ")
			}
		}
		return strings.TrimSpace(b.String())
	case "intuition", "mental_model", "why_it_matters":
		return strings.TrimSpace(stringFromAny(block["title"]) + " " + stringFromAny(block["md"]))
	default:
		return ""
	}
}

func findFigureRow(ctx context.Context, deps NodeDocPatchDeps, nodeID uuid.UUID, block map[string]any) (*types.LearningNodeFigure, error) {
	if deps.Figures == nil {
		return nil, fmt.Errorf("node_doc_patch: figure repo missing")
	}
	rows, err := deps.Figures.GetByPathNodeIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{nodeID})
	if err != nil {
		return nil, err
	}
	asset, _ := block["asset"].(map[string]any)
	storageKey := strings.TrimSpace(stringFromAny(asset["storage_key"]))
	url := strings.TrimSpace(stringFromAny(asset["url"]))
	for _, r := range rows {
		if r == nil {
			continue
		}
		if storageKey != "" && strings.TrimSpace(r.AssetStorageKey) == storageKey {
			return r, nil
		}
		if url != "" && strings.TrimSpace(r.AssetURL) == url {
			return r, nil
		}
	}
	if len(rows) == 1 {
		return rows[0], nil
	}
	return nil, nil
}

func findVideoRow(ctx context.Context, deps NodeDocPatchDeps, nodeID uuid.UUID, block map[string]any) (*types.LearningNodeVideo, error) {
	if deps.Videos == nil {
		return nil, fmt.Errorf("node_doc_patch: video repo missing")
	}
	rows, err := deps.Videos.GetByPathNodeIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{nodeID})
	if err != nil {
		return nil, err
	}
	url := strings.TrimSpace(stringFromAny(block["url"]))
	for _, r := range rows {
		if r == nil {
			continue
		}
		if url != "" && strings.TrimSpace(r.AssetURL) == url {
			return r, nil
		}
	}
	if len(rows) == 1 {
		return rows[0], nil
	}
	return nil, nil
}

func regenerateFigure(ctx context.Context, deps NodeDocPatchDeps, row *types.LearningNodeFigure, instruction string) (*types.LearningNodeFigure, error) {
	var plan content.FigurePlanItemV1
	if len(row.PlanJSON) == 0 || string(row.PlanJSON) == "null" {
		return nil, fmt.Errorf("node_doc_patch: missing figure plan_json")
	}
	if err := json.Unmarshal(row.PlanJSON, &plan); err != nil {
		return nil, fmt.Errorf("node_doc_patch: invalid figure plan_json")
	}

	prompt := strings.TrimSpace(plan.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("node_doc_patch: empty figure prompt")
	}
	if strings.TrimSpace(instruction) != "" {
		prompt = prompt + "\n\nUser request: " + strings.TrimSpace(instruction)
	}
	prompt = prompt + "\n\nGenerate a fresh variation distinct from prior outputs."
	plan.Prompt = prompt

	img, err := deps.AI.GenerateImage(ctx, prompt)
	if err != nil {
		return nil, err
	}
	if len(img.Bytes) == 0 {
		return nil, fmt.Errorf("node_doc_patch: image_generate_empty")
	}

	storageKey := fmt.Sprintf("generated/node_figures/%s/%s/slot_%d_%s.png",
		row.PathID.String(),
		row.PathNodeID.String(),
		row.Slot,
		content.HashBytes([]byte(prompt)),
	)
	if err := deps.Bucket.UploadFile(dbctx.Context{Ctx: ctx}, gcp.BucketCategoryMaterial, storageKey, bytes.NewReader(img.Bytes)); err != nil {
		return nil, err
	}
	publicURL := deps.Bucket.GetPublicURL(gcp.BucketCategoryMaterial, storageKey)

	mime := strings.TrimSpace(img.MimeType)
	if mime == "" {
		mime = strings.TrimSpace(row.AssetMimeType)
	}
	if mime == "" {
		mime = "image/png"
	}

	var assetID *uuid.UUID
	if deps.Assets != nil {
		meta := map[string]any{
			"asset_kind":     "generated_figure",
			"caption":        strings.TrimSpace(plan.Caption),
			"alt_text":       strings.TrimSpace(plan.AltText),
			"placement_hint": strings.TrimSpace(plan.PlacementHint),
			"citations":      content.NormalizeConceptKeys(plan.Citations),
		}
		aid := uuid.New()
		a := &types.Asset{
			ID:         aid,
			Kind:       "image",
			StorageKey: storageKey,
			URL:        publicURL,
			OwnerType:  "learning_node_figure",
			OwnerID:    row.ID,
			Metadata:   mustJSON(meta),
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
		}
		if _, err := deps.Assets.Create(dbctx.Context{Ctx: ctx}, []*types.Asset{a}); err == nil {
			assetID = &aid
		}
	}

	planJSON, _ := json.Marshal(plan)
	now := time.Now().UTC()
	update := &types.LearningNodeFigure{
		ID:              row.ID,
		UserID:          row.UserID,
		PathID:          row.PathID,
		PathNodeID:      row.PathNodeID,
		Slot:            row.Slot,
		SchemaVersion:   row.SchemaVersion,
		PlanJSON:        datatypes.JSON(planJSON),
		PromptHash:      content.HashBytes([]byte(prompt)),
		SourcesHash:     row.SourcesHash,
		Status:          "rendered",
		AssetID:         assetID,
		AssetStorageKey: storageKey,
		AssetURL:        publicURL,
		AssetMimeType:   mime,
		Error:           "",
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       now,
	}
	if deps.Figures != nil {
		if err := deps.Figures.Upsert(dbctx.Context{Ctx: ctx}, update); err != nil {
			return nil, err
		}
	}
	return update, nil
}

func regenerateVideo(ctx context.Context, deps NodeDocPatchDeps, row *types.LearningNodeVideo, instruction string) (*types.LearningNodeVideo, error) {
	var plan content.VideoPlanItemV1
	if len(row.PlanJSON) == 0 || string(row.PlanJSON) == "null" {
		return nil, fmt.Errorf("node_doc_patch: missing video plan_json")
	}
	if err := json.Unmarshal(row.PlanJSON, &plan); err != nil {
		return nil, fmt.Errorf("node_doc_patch: invalid video plan_json")
	}

	prompt := strings.TrimSpace(plan.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("node_doc_patch: empty video prompt")
	}
	if strings.TrimSpace(instruction) != "" {
		prompt = prompt + "\n\nUser request: " + strings.TrimSpace(instruction)
	}
	prompt = prompt + "\n\nGenerate a fresh variation distinct from prior outputs."
	plan.Prompt = prompt

	dur := plan.DurationSec
	if dur <= 0 {
		dur = 8
	}

	vid, err := deps.AI.GenerateVideo(ctx, prompt, openai.VideoGenerationOptions{DurationSeconds: dur})
	if err != nil {
		return nil, err
	}
	if len(vid.Bytes) == 0 {
		return nil, fmt.Errorf("node_doc_patch: video_generate_empty")
	}

	mime := strings.TrimSpace(vid.MimeType)
	ext := "mp4"
	switch {
	case strings.Contains(strings.ToLower(mime), "webm"):
		ext = "webm"
	case strings.Contains(strings.ToLower(mime), "mp4"):
		ext = "mp4"
	}

	storageKey := fmt.Sprintf("generated/node_videos/%s/%s/slot_%d_%s.%s",
		row.PathID.String(),
		row.PathNodeID.String(),
		row.Slot,
		content.HashBytes([]byte(prompt)),
		ext,
	)
	if err := deps.Bucket.UploadFile(dbctx.Context{Ctx: ctx}, gcp.BucketCategoryMaterial, storageKey, bytes.NewReader(vid.Bytes)); err != nil {
		return nil, err
	}
	publicURL := deps.Bucket.GetPublicURL(gcp.BucketCategoryMaterial, storageKey)

	if mime == "" {
		if ext == "webm" {
			mime = "video/webm"
		} else {
			mime = "video/mp4"
		}
	}

	var assetID *uuid.UUID
	if deps.Assets != nil {
		meta := map[string]any{
			"asset_kind":     "generated_video",
			"caption":        strings.TrimSpace(plan.Caption),
			"alt_text":       strings.TrimSpace(plan.AltText),
			"placement_hint": strings.TrimSpace(plan.PlacementHint),
			"citations":      content.NormalizeConceptKeys(plan.Citations),
		}
		aid := uuid.New()
		a := &types.Asset{
			ID:         aid,
			Kind:       "video",
			StorageKey: storageKey,
			URL:        publicURL,
			OwnerType:  "learning_node_video",
			OwnerID:    row.ID,
			Metadata:   mustJSON(meta),
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
		}
		if _, err := deps.Assets.Create(dbctx.Context{Ctx: ctx}, []*types.Asset{a}); err == nil {
			assetID = &aid
		}
	}

	planJSON, _ := json.Marshal(plan)
	now := time.Now().UTC()
	update := &types.LearningNodeVideo{
		ID:              row.ID,
		UserID:          row.UserID,
		PathID:          row.PathID,
		PathNodeID:      row.PathNodeID,
		Slot:            row.Slot,
		SchemaVersion:   row.SchemaVersion,
		PlanJSON:        datatypes.JSON(planJSON),
		PromptHash:      content.HashBytes([]byte(prompt)),
		SourcesHash:     row.SourcesHash,
		Status:          "rendered",
		AssetID:         assetID,
		AssetStorageKey: storageKey,
		AssetURL:        publicURL,
		AssetMimeType:   mime,
		Error:           "",
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       now,
	}
	if deps.Videos != nil {
		if err := deps.Videos.Upsert(dbctx.Context{Ctx: ctx}, update); err != nil {
			return nil, err
		}
	}
	return update, nil
}

func updateFigureBlock(block map[string]any, row *types.LearningNodeFigure) map[string]any {
	if block == nil || row == nil {
		return block
	}
	out := map[string]any{}
	for k, v := range block {
		out[k] = v
	}
	asset, _ := out["asset"].(map[string]any)
	if asset == nil {
		asset = map[string]any{}
	}
	asset["url"] = strings.TrimSpace(row.AssetURL)
	asset["storage_key"] = strings.TrimSpace(row.AssetStorageKey)
	if strings.TrimSpace(row.AssetMimeType) != "" {
		asset["mime_type"] = strings.TrimSpace(row.AssetMimeType)
	}
	out["asset"] = asset
	return out
}

func updateVideoBlock(block map[string]any, row *types.LearningNodeVideo) map[string]any {
	if block == nil || row == nil {
		return block
	}
	out := map[string]any{}
	for k, v := range block {
		out[k] = v
	}
	out["url"] = strings.TrimSpace(row.AssetURL)
	return out
}

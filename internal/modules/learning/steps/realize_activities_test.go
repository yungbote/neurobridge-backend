package steps

import (
	"strings"
	"testing"
)

func TestActivityContentMetrics_WordCountSplitsSnakeAndKebabCase(t *testing.T) {
	rawBlocks := []any{
		map[string]any{
			"kind":       "paragraph",
			"content_md": "gradient_descent foo-bar",
			"items":      []any{},
			"asset_refs": []any{},
		},
	}

	m := activityContentMetrics(rawBlocks)
	// "gradient_descent" -> 2 words, "foo-bar" -> 2 words
	if m.WordCount < 4 {
		t.Fatalf("expected word_count>=4 got %d", m.WordCount)
	}
}

func TestEnsureActivityContentMeetsMinima_DrillPadsToPassValidation(t *testing.T) {
	obj := map[string]any{
		"title": "Short Drill",
		"kind":  "drill",
		"content_json": map[string]any{
			"blocks": []any{
				map[string]any{
					"kind":       "paragraph",
					"content_md": "This is intentionally short.",
					"items":      []any{},
					"asset_refs": []any{},
				},
			},
		},
		"citations": []any{},
	}

	ensureActivityContentHasHeadingBlock(obj, "Short Drill")
	ensureActivityContentMeetsMinima(obj, "drill")

	if errs := validateActivityContent(obj, "drill"); len(errs) > 0 {
		t.Fatalf("expected no validation errors, got: %v", errs)
	}

	content, ok := obj["content_json"].(map[string]any)
	if !ok || content == nil {
		t.Fatalf("expected content_json to be map, got %#v", obj["content_json"])
	}
	rawBlocks, _ := content["blocks"].([]any)
	if len(rawBlocks) == 0 {
		t.Fatalf("expected blocks after padding")
	}
	m := activityContentMetrics(rawBlocks)
	if m.WordCount < 350 {
		t.Fatalf("expected word_count>=350 got %d", m.WordCount)
	}
	if m.Paragraphs < 2 {
		t.Fatalf("expected paragraphs>=2 got %d", m.Paragraphs)
	}
	if m.Callouts < 1 {
		t.Fatalf("expected callouts>=1 got %d", m.Callouts)
	}
	if m.Headings < 1 {
		t.Fatalf("expected headings>=1 got %d", m.Headings)
	}
}

func TestEnsureActivityContentMeetsMinima_DrillNearMissWordCountGetsToppedUp(t *testing.T) {
	nearMiss := strings.TrimSpace(strings.Repeat("word ", 325))
	obj := map[string]any{
		"title": "Near Miss Drill",
		"kind":  "drill",
		"content_json": map[string]any{
			"blocks": []any{
				map[string]any{
					"kind":       "heading",
					"content_md": "Overview",
					"items":      []any{},
					"asset_refs": []any{},
				},
				map[string]any{
					"kind":       "paragraph",
					"content_md": nearMiss,
					"items":      []any{},
					"asset_refs": []any{},
				},
				map[string]any{
					"kind":       "paragraph",
					"content_md": "Ok.",
					"items":      []any{},
					"asset_refs": []any{},
				},
				map[string]any{
					"kind":       "callout",
					"content_md": "**Hint**",
					"items":      []any{"Hint."},
					"asset_refs": []any{},
				},
			},
		},
		"citations": []any{},
	}

	before := validateActivityContent(obj, "drill")
	if len(before) == 0 {
		t.Fatalf("expected validation errors before padding, got none")
	}
	found := false
	for _, e := range before {
		if strings.Contains(e, "word_count too low") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected word_count error before padding, got: %v", before)
	}

	ensureActivityContentMeetsMinima(obj, "drill")
	if errs := validateActivityContent(obj, "drill"); len(errs) > 0 {
		t.Fatalf("expected no validation errors, got: %v", errs)
	}
}

package steps

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func TestIntakePathsBriefJSONFromPathMeta_ReturnsForMultiGoal(t *testing.T) {
	f1 := uuid.New()
	f2 := uuid.New()

	meta := map[string]any{
		"intake": map[string]any{
			"combined_goal":   "Learn two things",
			"primary_path_id": "p1",
			"material_alignment": map[string]any{
				"mode": "multi_goal",
			},
			"file_intents": []any{
				map[string]any{"file_id": f1.String(), "original_name": "topic_a.pdf"},
				map[string]any{"file_id": f2.String(), "original_name": "topic_b.pdf"},
			},
			"paths": []any{
				map[string]any{
					"path_id":          "p1",
					"title":            "Path A",
					"goal":             "Learn A",
					"core_file_ids":    []string{f1.String()},
					"support_file_ids": []string{},
					"confidence":       0.8,
					"notes":            "",
				},
				map[string]any{
					"path_id":          "p2",
					"title":            "Path B",
					"goal":             "Learn B",
					"core_file_ids":    []string{f2.String()},
					"support_file_ids": []string{},
					"confidence":       0.7,
					"notes":            "",
				},
			},
		},
	}

	raw := IntakePathsBriefJSONFromPathMeta(meta, 4)
	if raw == "" {
		t.Fatalf("expected non-empty paths json")
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if mode := stringFromAny(out["mode"]); mode != "multi_goal" {
		t.Fatalf("unexpected mode: %q", mode)
	}
	arr, ok := out["paths"].([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("unexpected paths: %#v", out["paths"])
	}

	first, _ := arr[0].(map[string]any)
	if stringFromAny(first["path_id"]) != "p1" {
		t.Fatalf("unexpected path ordering: %#v", arr)
	}
	coreFiles := stringSliceFromAny(first["core_files"])
	if len(coreFiles) != 1 || coreFiles[0] != "topic_a.pdf" {
		t.Fatalf("unexpected core_files: %#v", coreFiles)
	}
}

func TestBuildIntakeMaterialFilter_MultiGoalIncludesAllNonNoise(t *testing.T) {
	f1 := &types.MaterialFile{ID: uuid.New(), OriginalName: "a.pdf"}
	f2 := &types.MaterialFile{ID: uuid.New(), OriginalName: "b.pdf"}
	f3 := &types.MaterialFile{ID: uuid.New(), OriginalName: "noise.pdf"}

	intake := map[string]any{
		"material_alignment": map[string]any{
			"mode":             "multi_goal",
			"primary_goal":     "Two topics",
			"include_file_ids": []string{f1.ID.String()},
			"exclude_file_ids": []string{},
			"noise_file_ids":   []string{}, // derived from file_intents
			"notes":            "",
		},
		"file_intents": []any{
			map[string]any{"file_id": f1.ID.String(), "original_name": f1.OriginalName, "alignment": "core", "include_in_primary_path": true},
			map[string]any{"file_id": f2.ID.String(), "original_name": f2.OriginalName, "alignment": "core", "include_in_primary_path": true},
			map[string]any{"file_id": f3.ID.String(), "original_name": f3.OriginalName, "alignment": "noise", "include_in_primary_path": false},
		},
	}

	filter := buildIntakeMaterialFilter([]*types.MaterialFile{f1, f2, f3}, intake)
	ids := dedupeStrings(stringSliceFromAny(filter["include_file_ids"]))
	if len(ids) != 2 {
		t.Fatalf("unexpected include ids: %#v", ids)
	}
	set := map[string]bool{ids[0]: true, ids[1]: true}
	if !set[f1.ID.String()] || !set[f2.ID.String()] || set[f3.ID.String()] {
		t.Fatalf("unexpected include ids set: %#v", ids)
	}
}

func TestBuildIntakeMaterialFilter_SingleGoalRespectsIncludeList(t *testing.T) {
	f1 := &types.MaterialFile{ID: uuid.New(), OriginalName: "a.pdf"}
	f2 := &types.MaterialFile{ID: uuid.New(), OriginalName: "b.pdf"}

	intake := map[string]any{
		"material_alignment": map[string]any{
			"mode":             "single_goal",
			"primary_goal":     "Only A",
			"include_file_ids": []string{f1.ID.String()},
			"exclude_file_ids": []string{},
			"noise_file_ids":   []string{f2.ID.String()},
			"notes":            "",
		},
	}

	filter := buildIntakeMaterialFilter([]*types.MaterialFile{f1, f2}, intake)
	ids := dedupeStrings(stringSliceFromAny(filter["include_file_ids"]))
	if len(ids) != 1 || ids[0] != f1.ID.String() {
		t.Fatalf("unexpected include ids: %#v", ids)
	}
}

func TestNormalizeIntakePaths_AssignsAllFilesExactlyOnce(t *testing.T) {
	f1 := &types.MaterialFile{ID: uuid.New(), OriginalName: "alpha.pdf"}
	f2 := &types.MaterialFile{ID: uuid.New(), OriginalName: "beta.pdf"}
	f3 := &types.MaterialFile{ID: uuid.New(), OriginalName: "gamma.pdf"}

	intake := map[string]any{
		"combined_goal": "Test grouping",
		"paths": []any{
			map[string]any{
				"path_id":          "p1",
				"core_file_ids":    []string{f1.ID.String(), f2.ID.String(), f2.ID.String()},
				"support_file_ids": []string{f1.ID.String()},
			},
			map[string]any{
				"path_id":          "p2",
				"core_file_ids":    []string{f1.ID.String()},
				"support_file_ids": []string{},
			},
		},
		"file_intents": []any{
			map[string]any{
				"file_id":       f1.ID.String(),
				"original_name": f1.OriginalName,
			},
		},
	}

	normalizeIntakePaths(intake, []*types.MaterialFile{f1, f2, f3}, nil)

	paths := sliceAny(intake["paths"])
	if len(paths) == 0 {
		t.Fatalf("expected normalized paths")
	}

	assigned := map[string]int{}
	for _, p := range paths {
		m, ok := p.(map[string]any)
		if !ok || m == nil {
			t.Fatalf("unexpected path entry: %#v", p)
		}
		core := stringSliceFromAny(m["core_file_ids"])
		support := stringSliceFromAny(m["support_file_ids"])
		if len(core)+len(support) == 0 {
			t.Fatalf("empty path after normalization")
		}
		for _, id := range append(core, support...) {
			assigned[id]++
		}
	}

	for _, f := range []*types.MaterialFile{f1, f2, f3} {
		if assigned[f.ID.String()] != 1 {
			t.Fatalf("expected file %s assigned once, got %d", f.ID.String(), assigned[f.ID.String()])
		}
	}

	intents := sliceAny(intake["file_intents"])
	if len(intents) != 3 {
		t.Fatalf("expected file_intents for all files, got %d", len(intents))
	}
}

func TestSoftSplitSanityCheck_AddsStructureClarify(t *testing.T) {
	f1 := &types.MaterialFile{ID: uuid.New(), OriginalName: "http_caching.pdf"}
	f2 := &types.MaterialFile{ID: uuid.New(), OriginalName: "kubernetes_networking.pptx"}

	intake := map[string]any{
		"confidence": 0.4,
		"file_intents": []any{
			map[string]any{
				"file_id":       f1.ID.String(),
				"original_name": f1.OriginalName,
				"aim":           "HTTP caching and CDN behavior",
				"topics":        []string{"http", "caching", "cdn", "networking"},
			},
			map[string]any{
				"file_id":       f2.ID.String(),
				"original_name": f2.OriginalName,
				"aim":           "Kubernetes networking primitives",
				"topics":        []string{"kubernetes", "networking", "ingress"},
			},
		},
		"paths": []any{
			map[string]any{
				"path_id":          "p1",
				"title":            "HTTP caching",
				"goal":             "Caching and CDNs",
				"core_file_ids":    []string{f1.ID.String()},
				"support_file_ids": []string{},
				"confidence":       0.4,
				"notes":            "",
			},
			map[string]any{
				"path_id":          "p2",
				"title":            "Kubernetes networking",
				"goal":             "Services and ingress",
				"core_file_ids":    []string{f2.ID.String()},
				"support_file_ids": []string{},
				"confidence":       0.4,
				"notes":            "",
			},
		},
		"clarifying_questions": []any{},
	}

	softSplitSanityCheck(context.Background(), intake, []*types.MaterialFile{f1, f2}, nil, nil, false)

	if !boolFromAny(intake["needs_clarification"]) {
		t.Fatalf("expected needs_clarification true")
	}

	qs := sliceAny(intake["clarifying_questions"])
	found := false
	for _, it := range qs {
		m, ok := it.(map[string]any)
		if !ok || m == nil {
			continue
		}
		if stringFromAny(m["id"]) == "structure_clarify" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected structure_clarify question")
	}
}

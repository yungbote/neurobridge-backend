package steps

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func TestIntakeTracksBriefJSONFromPathMeta_ReturnsForMultiGoal(t *testing.T) {
	f1 := uuid.New()
	f2 := uuid.New()

	meta := map[string]any{
		"intake": map[string]any{
			"combined_goal":    "Learn two things",
			"primary_track_id": "t1",
			"material_alignment": map[string]any{
				"mode": "multi_goal",
			},
			"file_intents": []any{
				map[string]any{"file_id": f1.String(), "original_name": "topic_a.pdf"},
				map[string]any{"file_id": f2.String(), "original_name": "topic_b.pdf"},
			},
			"tracks": []any{
				map[string]any{
					"track_id":         "t1",
					"title":            "Track A",
					"goal":             "Learn A",
					"core_file_ids":    []string{f1.String()},
					"support_file_ids": []string{},
					"confidence":       0.8,
					"notes":            "",
				},
				map[string]any{
					"track_id":         "t2",
					"title":            "Track B",
					"goal":             "Learn B",
					"core_file_ids":    []string{f2.String()},
					"support_file_ids": []string{},
					"confidence":       0.7,
					"notes":            "",
				},
			},
		},
	}

	raw := IntakeTracksBriefJSONFromPathMeta(meta, 4)
	if raw == "" {
		t.Fatalf("expected non-empty tracks json")
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if mode := stringFromAny(out["mode"]); mode != "multi_goal" {
		t.Fatalf("unexpected mode: %q", mode)
	}
	arr, ok := out["tracks"].([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("unexpected tracks: %#v", out["tracks"])
	}

	first, _ := arr[0].(map[string]any)
	if stringFromAny(first["track_id"]) != "t1" {
		t.Fatalf("unexpected track ordering: %#v", arr)
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
			"mode":                          "multi_goal",
			"primary_goal":                  "Two topics",
			"include_file_ids":              []string{f1.ID.String()},
			"exclude_file_ids":              []string{},
			"maybe_separate_track_file_ids": []string{},
			"noise_file_ids":                []string{}, // derived from file_intents
			"notes":                         "",
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
			"mode":                          "single_goal",
			"primary_goal":                  "Only A",
			"include_file_ids":              []string{f1.ID.String()},
			"exclude_file_ids":              []string{},
			"maybe_separate_track_file_ids": []string{},
			"noise_file_ids":                []string{f2.ID.String()},
			"notes":                         "",
		},
	}

	filter := buildIntakeMaterialFilter([]*types.MaterialFile{f1, f2}, intake)
	ids := dedupeStrings(stringSliceFromAny(filter["include_file_ids"]))
	if len(ids) != 1 || ids[0] != f1.ID.String() {
		t.Fatalf("unexpected include ids: %#v", ids)
	}
}

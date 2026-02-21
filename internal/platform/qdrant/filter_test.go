package qdrant

import (
	"errors"
	"testing"
)

func TestTranslateFilterMapSubset(t *testing.T) {
	filter := map[string]any{
		"type": "chunk",
		"material_file_id": map[string]any{
			"$in": []any{"file-1", "file-2"},
		},
	}

	got, err := translateFilterMap(filter)
	if err != nil {
		t.Fatalf("translateFilterMap: %v", err)
	}
	if len(got.Must) != 2 {
		t.Fatalf("must length: want=2 got=%d", len(got.Must))
	}

	typeCond := findConditionByKey(got.Must, "type")
	if typeCond == nil {
		t.Fatalf("missing type condition")
	}
	typeMatch, ok := typeCond["match"].(map[string]any)
	if !ok || typeMatch["value"] != "chunk" {
		t.Fatalf("type match: got=%v", typeCond["match"])
	}

	fileCond := findConditionByKey(got.Must, "material_file_id")
	if fileCond == nil {
		t.Fatalf("missing material_file_id condition")
	}
	fileMatch, ok := fileCond["match"].(map[string]any)
	if !ok {
		t.Fatalf("material_file_id match type: got=%T", fileCond["match"])
	}
	anyVals, ok := fileMatch["any"].([]any)
	if !ok {
		t.Fatalf("material_file_id any type: got=%T", fileMatch["any"])
	}
	if len(anyVals) != 2 || anyVals[0] != "file-1" || anyVals[1] != "file-2" {
		t.Fatalf("material_file_id any values: got=%v", anyVals)
	}
}

func TestTranslateFilterMapUnsupportedOperator(t *testing.T) {
	_, err := translateFilterMap(map[string]any{
		"type": map[string]any{
			"$gt": 2,
		},
	})
	if err == nil {
		t.Fatalf("translateFilterMap: expected error, got nil")
	}

	var opErr *OperationError
	if !errors.As(err, &opErr) {
		t.Fatalf("expected OperationError, got=%T", err)
	}
	if opErr.Code != OperationErrorUnsupportedFilter {
		t.Fatalf("error code: want=%q got=%q", OperationErrorUnsupportedFilter, opErr.Code)
	}
}

func findConditionByKey(items []any, key string) map[string]any {
	for _, raw := range items {
		cond, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if condKey, _ := cond["key"].(string); condKey == key {
			return cond
		}
	}
	return nil
}

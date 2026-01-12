package schema

import "testing"

func TestSchemas_LoadAndLint(t *testing.T) {
	if _, err := NodeDocV1(); err != nil {
		t.Fatalf("NodeDocV1 schema invalid: %v", err)
	}
	if _, err := NodeDocGenV1(); err != nil {
		t.Fatalf("NodeDocGenV1 schema invalid: %v", err)
	}
	if _, err := NodeDocOutlineV1(); err != nil {
		t.Fatalf("NodeDocOutlineV1 schema invalid: %v", err)
	}
	if _, err := DrillPayloadV1(); err != nil {
		t.Fatalf("DrillPayloadV1 schema invalid: %v", err)
	}
	if _, err := FigurePlanV1(); err != nil {
		t.Fatalf("FigurePlanV1 schema invalid: %v", err)
	}
	if _, err := VideoPlanV1(); err != nil {
		t.Fatalf("VideoPlanV1 schema invalid: %v", err)
	}
	if _, err := VideoPlanV2(); err != nil {
		t.Fatalf("VideoPlanV2 schema invalid: %v", err)
	}
}

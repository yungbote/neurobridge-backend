package steps

import "testing"

func TestAddSupportPointerDedupes(t *testing.T) {
	base := supportPointer{
		SourceType: "event",
		SourceID:   "evt_1",
	}
	list, added := addSupportPointer(nil, base, 5)
	if !added || len(list) != 1 {
		t.Fatalf("expected add to succeed once, got added=%v len=%d", added, len(list))
	}
	list, added = addSupportPointer(list, base, 5)
	if added || len(list) != 1 {
		t.Fatalf("expected duplicate to be ignored, got added=%v len=%d", added, len(list))
	}
}

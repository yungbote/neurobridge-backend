package steps

import (
	"testing"
	"time"
)

func TestIsConceptActive(t *testing.T) {
	now := time.Now().UTC()
	entry := ConceptKnowledgeV1{
		Key:        "test_concept",
		Confidence: 0.30,
	}
	if !isConceptActive(entry, nil, 90, 0.25, now) {
		t.Fatalf("expected active due to confidence")
	}

	entry = ConceptKnowledgeV1{
		Key:        "test_concept",
		Confidence: 0.10,
		LastSeenAt: now.Add(-10 * 24 * time.Hour).Format(time.RFC3339Nano),
	}
	if !isConceptActive(entry, nil, 90, 0.25, now) {
		t.Fatalf("expected active due to recency")
	}

	entry = ConceptKnowledgeV1{
		Key:        "test_concept",
		Confidence: 0.10,
	}
	if isConceptActive(entry, nil, 7, 0.25, now) {
		t.Fatalf("expected inactive with no evidence")
	}
}

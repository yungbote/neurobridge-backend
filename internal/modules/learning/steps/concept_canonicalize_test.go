package steps

import (
	"testing"

	"github.com/google/uuid"
)

func TestResolveCanonicalOverride(t *testing.T) {
	pathID := uuid.New()
	overrideID := uuid.New()
	overrides := map[uuid.UUID]uuid.UUID{pathID: overrideID}

	if got := resolveCanonicalOverride(pathID, overrides); got != overrideID {
		t.Fatalf("expected override %s got %s", overrideID, got)
	}
	if got := resolveCanonicalOverride(uuid.New(), overrides); got != uuid.Nil {
		t.Fatalf("expected nil override for unknown key, got %s", got)
	}
	if got := resolveCanonicalOverride(uuid.Nil, overrides); got != uuid.Nil {
		t.Fatalf("expected nil override for nil key, got %s", got)
	}
}

package steps

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/learning/content"
)

func TestSanitizeNodeDocCitations_BackfillsWhenAllCitationsDisallowed(t *testing.T) {
	allowed1 := uuid.New()
	allowed2 := uuid.New()
	disallowed := uuid.New()

	allowed := map[string]bool{
		allowed1.String(): true,
		allowed2.String(): true,
	}

	doc := content.NodeDocV1{
		SchemaVersion: 1,
		Title:         "t",
		ConceptKeys:   []string{"k"},
		Blocks: []map[string]any{
			{"type": "heading", "level": 2, "text": "H"},
			{"type": "paragraph", "md": "Body", "citations": []any{
				map[string]any{"chunk_id": disallowed.String(), "quote": "x", "loc": map[string]any{"page": 0, "start": 0, "end": 0}},
				map[string]any{"chunk_id": "not-a-uuid", "quote": "y", "loc": map[string]any{"page": 0, "start": 0, "end": 0}},
			}},
		},
	}

	patched, stats, changed := sanitizeNodeDocCitations(doc, allowed, map[uuid.UUID]*types.MaterialChunk{
		allowed2: &types.MaterialChunk{Text: "some supporting text"},
	}, []uuid.UUID{allowed2, allowed1})

	if !changed {
		t.Fatalf("expected changed=true")
	}
	if stats.BlocksBackfilled != 1 {
		t.Fatalf("expected BlocksBackfilled=1 got %d", stats.BlocksBackfilled)
	}
	citsAny, ok := patched.Blocks[1]["citations"].([]any)
	if !ok || len(citsAny) != 1 {
		t.Fatalf("expected 1 citation, got %#v", patched.Blocks[1]["citations"])
	}
	c0, ok := citsAny[0].(map[string]any)
	if !ok {
		t.Fatalf("expected map citation, got %#v", citsAny[0])
	}
	if got := strings.TrimSpace(stringFromAny(c0["chunk_id"])); got != allowed2.String() {
		t.Fatalf("expected chunk_id=%s got %s", allowed2.String(), got)
	}
}

func TestSanitizeNodeDocCitations_NormalizesAndTruncates(t *testing.T) {
	id := uuid.New()
	allowed := map[string]bool{id.String(): true}

	longQuote := strings.Repeat("a", 300)
	doc := content.NodeDocV1{
		SchemaVersion: 1,
		Title:         "t",
		ConceptKeys:   []string{"k"},
		Blocks: []map[string]any{
			{"type": "paragraph", "md": "Body", "citations": []any{
				map[string]any{"chunk_id": strings.ToUpper(id.String()), "quote": longQuote, "loc": map[string]any{"page": 0, "start": 0, "end": 0}},
			}},
		},
	}

	patched, stats, changed := sanitizeNodeDocCitations(doc, allowed, nil, []uuid.UUID{id})
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if stats.ChunkIDsNormalized != 1 {
		t.Fatalf("expected ChunkIDsNormalized=1 got %d", stats.ChunkIDsNormalized)
	}
	if stats.QuotesTruncated != 1 {
		t.Fatalf("expected QuotesTruncated=1 got %d", stats.QuotesTruncated)
	}

	citsAny, ok := patched.Blocks[0]["citations"].([]any)
	if !ok || len(citsAny) != 1 {
		t.Fatalf("expected 1 citation, got %#v", patched.Blocks[0]["citations"])
	}
	c0, ok := citsAny[0].(map[string]any)
	if !ok {
		t.Fatalf("expected map citation, got %#v", citsAny[0])
	}
	if got := strings.TrimSpace(stringFromAny(c0["chunk_id"])); got != id.String() {
		t.Fatalf("expected chunk_id=%s got %s", id.String(), got)
	}
	if q := strings.TrimSpace(stringFromAny(c0["quote"])); len(q) != 240 {
		t.Fatalf("expected quote length 240 got %d", len(q))
	}
}

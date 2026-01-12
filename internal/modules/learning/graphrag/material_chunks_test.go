package graphrag

import (
	"context"
	"testing"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
)

func TestExpandMaterialChunkScores_ScopesToSetAndAllowFiles(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	// Ensure tables needed for GraphRAG expansion exist for this test DB.
	if err := tx.AutoMigrate(
		&types.MaterialSet{},
		&types.MaterialFile{},
		&types.MaterialChunk{},
		&types.Concept{},
		&types.ConceptEvidence{},
		&types.ConceptEdge{},
		&types.MaterialEntity{},
		&types.MaterialClaim{},
		&types.MaterialChunkEntity{},
		&types.MaterialChunkClaim{},
	); err != nil {
		t.Fatalf("auto-migrate: %v", err)
	}

	set1 := uuid.New()
	set2 := uuid.New()

	userID := uuid.New()

	if err := tx.Create(&types.MaterialSet{ID: set1, UserID: userID, Title: "Set 1"}).Error; err != nil {
		t.Fatalf("create set1: %v", err)
	}
	if err := tx.Create(&types.MaterialSet{ID: set2, UserID: userID, Title: "Set 2"}).Error; err != nil {
		t.Fatalf("create set2: %v", err)
	}

	file1a := uuid.New()
	file1b := uuid.New()
	file2 := uuid.New()

	if err := tx.Create(&types.MaterialFile{ID: file1a, MaterialSetID: set1, OriginalName: "a.pdf", StorageKey: "k1"}).Error; err != nil {
		t.Fatalf("create file1a: %v", err)
	}
	if err := tx.Create(&types.MaterialFile{ID: file1b, MaterialSetID: set1, OriginalName: "b.pdf", StorageKey: "k2"}).Error; err != nil {
		t.Fatalf("create file1b: %v", err)
	}
	if err := tx.Create(&types.MaterialFile{ID: file2, MaterialSetID: set2, OriginalName: "c.pdf", StorageKey: "k3"}).Error; err != nil {
		t.Fatalf("create file2: %v", err)
	}

	chA := uuid.New() // seed chunk (file1a)
	chB := uuid.New() // shares concept X (file1b)
	chC := uuid.New() // concept Y (neighbor of X) (file1b)
	chD := uuid.New() // shares entity E (file1a)
	chE := uuid.New() // shares claim Q (file1b)
	chOther := uuid.New() // other set chunk (file2)

	if err := tx.Create(&types.MaterialChunk{ID: chA, MaterialFileID: file1a, Index: 0, Text: "Seed chunk A"}).Error; err != nil {
		t.Fatalf("create chA: %v", err)
	}
	if err := tx.Create(&types.MaterialChunk{ID: chB, MaterialFileID: file1b, Index: 0, Text: "Chunk B"}).Error; err != nil {
		t.Fatalf("create chB: %v", err)
	}
	if err := tx.Create(&types.MaterialChunk{ID: chC, MaterialFileID: file1b, Index: 1, Text: "Chunk C"}).Error; err != nil {
		t.Fatalf("create chC: %v", err)
	}
	if err := tx.Create(&types.MaterialChunk{ID: chD, MaterialFileID: file1a, Index: 1, Text: "Chunk D"}).Error; err != nil {
		t.Fatalf("create chD: %v", err)
	}
	if err := tx.Create(&types.MaterialChunk{ID: chE, MaterialFileID: file1b, Index: 2, Text: "Chunk E"}).Error; err != nil {
		t.Fatalf("create chE: %v", err)
	}
	if err := tx.Create(&types.MaterialChunk{ID: chOther, MaterialFileID: file2, Index: 0, Text: "Other set chunk"}).Error; err != nil {
		t.Fatalf("create chOther: %v", err)
	}

	conceptX := uuid.New()
	conceptY := uuid.New()
	if err := tx.Create(&types.Concept{ID: conceptX, Scope: "path", Key: "concept_x", Name: "Concept X"}).Error; err != nil {
		t.Fatalf("create conceptX: %v", err)
	}
	if err := tx.Create(&types.Concept{ID: conceptY, Scope: "path", Key: "concept_y", Name: "Concept Y"}).Error; err != nil {
		t.Fatalf("create conceptY: %v", err)
	}

	if err := tx.Create(&types.ConceptEdge{ID: uuid.New(), FromConceptID: conceptX, ToConceptID: conceptY, EdgeType: "prereq", Strength: 1}).Error; err != nil {
		t.Fatalf("create concept edge: %v", err)
	}

	if err := tx.Create(&types.ConceptEvidence{ID: uuid.New(), ConceptID: conceptX, MaterialChunkID: chA, Weight: 1}).Error; err != nil {
		t.Fatalf("create evidence A->X: %v", err)
	}
	if err := tx.Create(&types.ConceptEvidence{ID: uuid.New(), ConceptID: conceptX, MaterialChunkID: chB, Weight: 1}).Error; err != nil {
		t.Fatalf("create evidence B->X: %v", err)
	}
	if err := tx.Create(&types.ConceptEvidence{ID: uuid.New(), ConceptID: conceptY, MaterialChunkID: chC, Weight: 1}).Error; err != nil {
		t.Fatalf("create evidence C->Y: %v", err)
	}
	// Same concept evidence but in a different material set; should be filtered out by set scoping.
	if err := tx.Create(&types.ConceptEvidence{ID: uuid.New(), ConceptID: conceptX, MaterialChunkID: chOther, Weight: 1}).Error; err != nil {
		t.Fatalf("create evidence other->X: %v", err)
	}

	entityE := uuid.New()
	if err := tx.Create(&types.MaterialEntity{ID: entityE, MaterialSetID: set1, Key: "entity_e", Name: "Entity E"}).Error; err != nil {
		t.Fatalf("create entity: %v", err)
	}
	if err := tx.Create(&types.MaterialChunkEntity{ID: uuid.New(), MaterialChunkID: chA, MaterialEntityID: entityE, Weight: 1}).Error; err != nil {
		t.Fatalf("create chunk_entity A->E: %v", err)
	}
	if err := tx.Create(&types.MaterialChunkEntity{ID: uuid.New(), MaterialChunkID: chD, MaterialEntityID: entityE, Weight: 1}).Error; err != nil {
		t.Fatalf("create chunk_entity D->E: %v", err)
	}

	claimQ := uuid.New()
	if err := tx.Create(&types.MaterialClaim{ID: claimQ, MaterialSetID: set1, Key: "claim_q", Content: "Claim Q"}).Error; err != nil {
		t.Fatalf("create claim: %v", err)
	}
	if err := tx.Create(&types.MaterialChunkClaim{ID: uuid.New(), MaterialChunkID: chA, MaterialClaimID: claimQ, Weight: 1}).Error; err != nil {
		t.Fatalf("create chunk_claim A->Q: %v", err)
	}
	if err := tx.Create(&types.MaterialChunkClaim{ID: uuid.New(), MaterialChunkID: chE, MaterialClaimID: claimQ, Weight: 1}).Error; err != nil {
		t.Fatalf("create chunk_claim E->Q: %v", err)
	}

	ctx := context.Background()

	// No allowlist: expansion should include same-set chunks via concepts/entities/claims.
	{
		scores, _, err := ExpandMaterialChunkScores(ctx, tx, set1, []SeedChunk{{ChunkID: chA, Score: 1}}, MaterialChunkExpandOptions{})
		if err != nil {
			t.Fatalf("expand (no allowlist): %v", err)
		}
		if len(scores) == 0 {
			t.Fatalf("expected non-empty scores")
		}
		if _, ok := scores[chA]; !ok {
			t.Fatalf("expected seed chunk in output")
		}
		if _, ok := scores[chB]; !ok {
			t.Fatalf("expected concept-evidence expansion chunk B")
		}
		if _, ok := scores[chC]; !ok {
			t.Fatalf("expected concept-edge expansion chunk C")
		}
		if _, ok := scores[chD]; !ok {
			t.Fatalf("expected entity expansion chunk D")
		}
		if _, ok := scores[chE]; !ok {
			t.Fatalf("expected claim expansion chunk E")
		}
		if _, ok := scores[chOther]; ok {
			t.Fatalf("did not expect other-set chunk to appear")
		}
	}

	// With allowlist restricted to file1a, expansion should stay within that file.
	{
		scores, _, err := ExpandMaterialChunkScores(ctx, tx, set1, []SeedChunk{{ChunkID: chA, Score: 1}}, MaterialChunkExpandOptions{
			AllowFileIDs: map[uuid.UUID]bool{file1a: true},
		})
		if err != nil {
			t.Fatalf("expand (allowlist): %v", err)
		}
		if _, ok := scores[chA]; !ok {
			t.Fatalf("expected seed chunk in output")
		}
		if _, ok := scores[chD]; !ok {
			t.Fatalf("expected entity expansion within allowed file")
		}
		if _, ok := scores[chB]; ok {
			t.Fatalf("did not expect chunk B (disallowed file)")
		}
		if _, ok := scores[chC]; ok {
			t.Fatalf("did not expect chunk C (disallowed file)")
		}
		if _, ok := scores[chE]; ok {
			t.Fatalf("did not expect chunk E (disallowed file)")
		}
	}
}


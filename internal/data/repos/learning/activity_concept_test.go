package learning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func TestActivityConceptRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewActivityConceptRepo(db, testutil.Logger(t))

	scopeID := uuid.New()
	concept1 := &types.Concept{ID: uuid.New(), Scope: "path", ScopeID: &scopeID, Key: "c1", Name: "C1"}
	concept2 := &types.Concept{ID: uuid.New(), Scope: "path", ScopeID: &scopeID, Key: "c2", Name: "C2"}
	activity := &types.Activity{ID: uuid.New(), Kind: "reading", Title: "a", Status: "draft"}
	if err := tx.WithContext(ctx).Create(concept1).Error; err != nil {
		t.Fatalf("seed concept1: %v", err)
	}
	if err := tx.WithContext(ctx).Create(concept2).Error; err != nil {
		t.Fatalf("seed concept2: %v", err)
	}
	if err := tx.WithContext(ctx).Create(activity).Error; err != nil {
		t.Fatalf("seed activity: %v", err)
	}

	ac1 := &types.ActivityConcept{ID: uuid.New(), ActivityID: activity.ID, ConceptID: concept1.ID, Role: "primary", Weight: 1}
	if _, err := repo.Create(ctx, tx, []*types.ActivityConcept{ac1}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	dup := &types.ActivityConcept{ID: uuid.New(), ActivityID: activity.ID, ConceptID: concept1.ID, Role: "primary", Weight: 1}
	ac2 := &types.ActivityConcept{ID: uuid.New(), ActivityID: activity.ID, ConceptID: concept2.ID, Role: "secondary", Weight: 0.5}
	if n, err := repo.CreateIgnoreDuplicates(ctx, tx, []*types.ActivityConcept{dup, ac2}); err != nil || n != 1 {
		t.Fatalf("CreateIgnoreDuplicates: n=%d err=%v", n, err)
	}

	if got, err := repo.GetByID(ctx, tx, ac1.ID); err != nil || got == nil || got.ID != ac1.ID {
		t.Fatalf("GetByID: got=%v err=%v", got, err)
	}
	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{ac1.ID, ac2.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByActivityIDs(ctx, tx, []uuid.UUID{activity.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByActivityIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByConceptIDs(ctx, tx, []uuid.UUID{concept1.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByConceptIDs: err=%v len=%d", err, len(rows))
	}

	up := &types.ActivityConcept{ID: ac2.ID, ActivityID: activity.ID, ConceptID: concept2.ID, Role: "primary", Weight: 2}
	if err := repo.Upsert(ctx, tx, up); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	ac1.Role = "secondary"
	if err := repo.Update(ctx, tx, ac1); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := repo.UpdateFields(ctx, tx, ac1.ID, map[string]interface{}{"weight": 3.0}); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{ac2.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}
	if err := repo.SoftDeleteByConceptIDs(ctx, tx, []uuid.UUID{concept1.ID}); err != nil {
		t.Fatalf("SoftDeleteByConceptIDs: %v", err)
	}
	if err := repo.SoftDeleteByActivityIDs(ctx, tx, []uuid.UUID{activity.ID}); err != nil {
		t.Fatalf("SoftDeleteByActivityIDs: %v", err)
	}

	fd := &types.ActivityConcept{ID: uuid.New(), ActivityID: activity.ID, ConceptID: concept1.ID, Role: "primary", Weight: 1}
	if _, err := repo.Create(ctx, tx, []*types.ActivityConcept{fd}); err != nil {
		t.Fatalf("seed fd: %v", err)
	}
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{fd.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}
	if err := repo.FullDeleteByConceptIDs(ctx, tx, []uuid.UUID{concept1.ID}); err != nil {
		t.Fatalf("FullDeleteByConceptIDs: %v", err)
	}
	if err := repo.FullDeleteByActivityIDs(ctx, tx, []uuid.UUID{activity.ID}); err != nil {
		t.Fatalf("FullDeleteByActivityIDs: %v", err)
	}
}

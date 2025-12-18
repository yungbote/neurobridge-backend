package learning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func TestConceptRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewConceptRepo(db, testutil.Logger(t))

	scopeID := uuid.New()

	root := &types.Concept{ID: uuid.New(), Scope: "path", ScopeID: &scopeID, Key: "root", Name: "Root", VectorID: "vec-root"}
	child := &types.Concept{ID: uuid.New(), Scope: "path", ScopeID: &scopeID, ParentID: testutil.PtrUUID(root.ID), Key: "child", Name: "Child"}
	global := &types.Concept{ID: uuid.New(), Scope: "global", ScopeID: nil, Key: "g1", Name: "Global", VectorID: "vec-g1"}

	if _, err := repo.Create(ctx, tx, []*types.Concept{root, child, global}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{root.ID, child.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if got, err := repo.GetByID(ctx, tx, global.ID); err != nil || got == nil || got.ID != global.ID {
		t.Fatalf("GetByID: got=%v err=%v", got, err)
	}

	if rows, err := repo.GetByScope(ctx, tx, "path", &scopeID); err != nil || len(rows) != 2 {
		t.Fatalf("GetByScope(path): err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByScope(ctx, tx, "global", nil); err != nil || len(rows) != 1 {
		t.Fatalf("GetByScope(global): err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByScopeAndKeys(ctx, tx, "path", &scopeID, []string{"root"}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByScopeAndKeys: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByScopeAndParent(ctx, tx, "path", &scopeID, nil); err != nil || len(rows) != 1 {
		t.Fatalf("GetByScopeAndParent(nil): err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByScopeAndParent(ctx, tx, "path", &scopeID, testutil.PtrUUID(root.ID)); err != nil || len(rows) != 1 {
		t.Fatalf("GetByScopeAndParent(root): err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByParentIDs(ctx, tx, []uuid.UUID{root.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByParentIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByVectorIDs(ctx, tx, []string{"vec-g1"}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByVectorIDs: err=%v len=%d", err, len(rows))
	}

	up := &types.Concept{ID: uuid.New(), Scope: "path", ScopeID: &scopeID, Key: "upsert", Name: "Upsert1"}
	if err := repo.UpsertByScopeAndKey(ctx, tx, up); err != nil {
		t.Fatalf("UpsertByScopeAndKey(create): %v", err)
	}
	up2 := &types.Concept{ID: up.ID, Scope: "path", ScopeID: &scopeID, Key: "upsert", Name: "Upsert2"}
	if err := repo.UpsertByScopeAndKey(ctx, tx, up2); err != nil {
		t.Fatalf("UpsertByScopeAndKey(update): %v", err)
	}

	root.Name = "Root2"
	if err := repo.Update(ctx, tx, root); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := repo.UpdateFields(ctx, tx, child.ID, map[string]interface{}{"name": "Child2"}); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{global.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}
	if err := repo.SoftDeleteByParentIDs(ctx, tx, []uuid.UUID{root.ID}); err != nil {
		t.Fatalf("SoftDeleteByParentIDs: %v", err)
	}
	if err := repo.SoftDeleteByScope(ctx, tx, "path", &scopeID); err != nil {
		t.Fatalf("SoftDeleteByScope: %v", err)
	}

	cFD := &types.Concept{ID: uuid.New(), Scope: "global", ScopeID: nil, Key: "fd", Name: "FD"}
	if _, err := repo.Create(ctx, tx, []*types.Concept{cFD}); err != nil {
		t.Fatalf("seed fd: %v", err)
	}
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{cFD.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}
	if err := repo.FullDeleteByParentIDs(ctx, tx, []uuid.UUID{root.ID}); err != nil {
		t.Fatalf("FullDeleteByParentIDs: %v", err)
	}
	if err := repo.FullDeleteByScope(ctx, tx, "global", nil); err != nil {
		t.Fatalf("FullDeleteByScope: %v", err)
	}
}

package learning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
)

func TestActivityRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	dbc := dbctx.Context{Ctx: ctx, Tx: tx}
	repo := NewActivityRepo(db, testutil.Logger(t))

	ownerType := "path"
	owner1 := uuid.New()
	owner2 := uuid.New()

	a1 := &types.Activity{ID: uuid.New(), OwnerType: ownerType, OwnerID: testutil.PtrUUID(owner1), Kind: "reading", Title: "a1", Status: "draft"}
	a2 := &types.Activity{ID: uuid.New(), OwnerType: ownerType, OwnerID: testutil.PtrUUID(owner1), Kind: "reading", Title: "a2", Status: "draft"}
	a3 := &types.Activity{ID: uuid.New(), OwnerType: ownerType, OwnerID: testutil.PtrUUID(owner2), Kind: "reading", Title: "a3", Status: "ready"}
	if _, err := repo.Create(dbc, []*types.Activity{a1, a2, a3}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got, err := repo.GetByID(dbc, a1.ID); err != nil || got == nil || got.ID != a1.ID {
		t.Fatalf("GetByID: got=%v err=%v", got, err)
	}
	if rows, err := repo.GetByIDs(dbc, []uuid.UUID{a1.ID, a2.ID, a3.ID}); err != nil || len(rows) != 3 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.ListByOwner(dbc, ownerType, testutil.PtrUUID(owner1)); err != nil || len(rows) != 2 {
		t.Fatalf("ListByOwner: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.ListByOwnerIDs(dbc, ownerType, []uuid.UUID{owner1, owner2}); err != nil || len(rows) != 3 {
		t.Fatalf("ListByOwnerIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.ListByStatus(dbc, []string{"ready"}); err != nil || len(rows) != 1 {
		t.Fatalf("ListByStatus: err=%v len=%d", err, len(rows))
	}

	a1.Title = "a1b"
	if err := repo.Update(dbc, a1); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := repo.UpdateFields(dbc, a2.ID, map[string]interface{}{"status": "archived"}); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	if err := repo.SoftDeleteByOwner(dbc, ownerType, testutil.PtrUUID(owner1)); err != nil {
		t.Fatalf("SoftDeleteByOwner: %v", err)
	}
	if rows, err := repo.ListByOwner(dbc, ownerType, testutil.PtrUUID(owner1)); err != nil || len(rows) != 0 {
		t.Fatalf("after SoftDeleteByOwner ListByOwner: err=%v len=%d", err, len(rows))
	}

	if err := repo.SoftDeleteByIDs(dbc, []uuid.UUID{a3.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}

	// Full deletes
	b1 := &types.Activity{ID: uuid.New(), OwnerType: ownerType, OwnerID: testutil.PtrUUID(owner1), Kind: "reading", Title: "b1", Status: "draft"}
	if _, err := repo.Create(dbc, []*types.Activity{b1}); err != nil {
		t.Fatalf("seed b1: %v", err)
	}
	if err := repo.FullDeleteByIDs(dbc, []uuid.UUID{b1.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}

	b2 := &types.Activity{ID: uuid.New(), OwnerType: ownerType, OwnerID: testutil.PtrUUID(owner2), Kind: "reading", Title: "b2", Status: "draft"}
	if _, err := repo.Create(dbc, []*types.Activity{b2}); err != nil {
		t.Fatalf("seed b2: %v", err)
	}
	if err := repo.FullDeleteByOwner(dbc, ownerType, testutil.PtrUUID(owner2)); err != nil {
		t.Fatalf("FullDeleteByOwner: %v", err)
	}
}

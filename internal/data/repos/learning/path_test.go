package learning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/datatypes"
)

func TestPathRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewPathRepo(db, testutil.Logger(t))

	u := testutil.SeedUser(t, ctx, tx, "pathrepo@example.com")

	p1 := &types.Path{ID: uuid.New(), UserID: testutil.PtrUUID(u.ID), Title: "p1", Status: "draft", Metadata: datatypes.JSON([]byte("{}"))}
	p2 := &types.Path{ID: uuid.New(), UserID: nil, Title: "p2", Status: "active", Metadata: datatypes.JSON([]byte("{}"))}
	if _, err := repo.Create(ctx, tx, []*types.Path{p1, p2}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got, err := repo.GetByID(ctx, tx, p1.ID); err != nil || got == nil || got.ID != p1.ID {
		t.Fatalf("GetByID: got=%v err=%v", got, err)
	}
	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{p1.ID, p2.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}

	if rows, err := repo.ListByUser(ctx, tx, testutil.PtrUUID(u.ID)); err != nil || len(rows) != 1 {
		t.Fatalf("ListByUser(user): err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.ListByUser(ctx, tx, nil); err != nil || len(rows) != 1 {
		t.Fatalf("ListByUser(nil): err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.ListByUserIDs(ctx, tx, []uuid.UUID{u.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("ListByUserIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.ListByStatus(ctx, tx, []string{"active"}); err != nil || len(rows) != 1 {
		t.Fatalf("ListByStatus: err=%v len=%d", err, len(rows))
	}

	p1.Title = "p1b"
	if err := repo.Update(ctx, tx, p1); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := repo.UpdateFields(ctx, tx, p2.ID, map[string]interface{}{"title": "p2b"}); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{p2.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}
	if err := repo.SoftDeleteByUserIDs(ctx, tx, []uuid.UUID{u.ID}); err != nil {
		t.Fatalf("SoftDeleteByUserIDs: %v", err)
	}

	fd := &types.Path{ID: uuid.New(), UserID: testutil.PtrUUID(u.ID), Title: "fd", Status: "draft", Metadata: datatypes.JSON([]byte("{}"))}
	if _, err := repo.Create(ctx, tx, []*types.Path{fd}); err != nil {
		t.Fatalf("seed fd: %v", err)
	}
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{fd.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}
	if err := repo.FullDeleteByUserIDs(ctx, tx, []uuid.UUID{u.ID}); err != nil {
		t.Fatalf("FullDeleteByUserIDs: %v", err)
	}
}

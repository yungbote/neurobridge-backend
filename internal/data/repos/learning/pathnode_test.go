package learning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/datatypes"
)

func TestPathNodeRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewPathNodeRepo(db, testutil.Logger(t))

	path := &types.Path{ID: uuid.New(), Title: "path", Status: "draft", Metadata: datatypes.JSON([]byte("{}"))}
	if err := tx.WithContext(ctx).Create(path).Error; err != nil {
		t.Fatalf("seed path: %v", err)
	}

	n1 := &types.PathNode{ID: uuid.New(), PathID: path.ID, Index: 0, Title: "n1", Gating: datatypes.JSON([]byte("{}")), Metadata: datatypes.JSON([]byte("{}"))}
	if _, err := repo.Create(ctx, tx, []*types.PathNode{n1}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got, err := repo.GetByID(ctx, tx, n1.ID); err != nil || got == nil || got.ID != n1.ID {
		t.Fatalf("GetByID: got=%v err=%v", got, err)
	}
	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{n1.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByPathIDs(ctx, tx, []uuid.UUID{path.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByPathIDs: err=%v len=%d", err, len(rows))
	}
	if got, err := repo.GetByPathAndIndex(ctx, tx, path.ID, 0); err != nil || got == nil || got.ID != n1.ID {
		t.Fatalf("GetByPathAndIndex: got=%v err=%v", got, err)
	}

	up := &types.PathNode{ID: n1.ID, PathID: path.ID, Index: 0, Title: "n1-upsert", Gating: datatypes.JSON([]byte("{}")), Metadata: datatypes.JSON([]byte("{}"))}
	if err := repo.Upsert(ctx, tx, up); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	gotUp, _ := repo.GetByPathAndIndex(ctx, tx, path.ID, 0)
	if gotUp == nil || gotUp.Title != "n1-upsert" {
		t.Fatalf("Upsert verify: got=%v", gotUp)
	}

	n1.Title = "n1-update"
	if err := repo.Update(ctx, tx, n1); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := repo.UpdateFields(ctx, tx, n1.ID, map[string]interface{}{"title": "n1-fields"}); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{n1.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}

	n2 := &types.PathNode{ID: uuid.New(), PathID: path.ID, Index: 1, Title: "n2", Gating: datatypes.JSON([]byte("{}")), Metadata: datatypes.JSON([]byte("{}"))}
	if _, err := repo.Create(ctx, tx, []*types.PathNode{n2}); err != nil {
		t.Fatalf("seed n2: %v", err)
	}
	if err := repo.SoftDeleteByPathIDs(ctx, tx, []uuid.UUID{path.ID}); err != nil {
		t.Fatalf("SoftDeleteByPathIDs: %v", err)
	}

	n3 := &types.PathNode{ID: uuid.New(), PathID: path.ID, Index: 2, Title: "n3", Gating: datatypes.JSON([]byte("{}")), Metadata: datatypes.JSON([]byte("{}"))}
	if _, err := repo.Create(ctx, tx, []*types.PathNode{n3}); err != nil {
		t.Fatalf("seed n3: %v", err)
	}
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{n3.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}

	n4 := &types.PathNode{ID: uuid.New(), PathID: path.ID, Index: 3, Title: "n4", Gating: datatypes.JSON([]byte("{}")), Metadata: datatypes.JSON([]byte("{}"))}
	if _, err := repo.Create(ctx, tx, []*types.PathNode{n4}); err != nil {
		t.Fatalf("seed n4: %v", err)
	}
	if err := repo.FullDeleteByPathIDs(ctx, tx, []uuid.UUID{path.ID}); err != nil {
		t.Fatalf("FullDeleteByPathIDs: %v", err)
	}
}

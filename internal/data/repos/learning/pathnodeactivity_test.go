package learning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/datatypes"
)

func TestPathNodeActivityRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewPathNodeActivityRepo(db, testutil.Logger(t))

	path := &types.Path{ID: uuid.New(), Title: "path", Status: "draft", Metadata: datatypes.JSON([]byte("{}"))}
	if err := tx.WithContext(ctx).Create(path).Error; err != nil {
		t.Fatalf("seed path: %v", err)
	}
	node := &types.PathNode{ID: uuid.New(), PathID: path.ID, Index: 0, Title: "node", Gating: datatypes.JSON([]byte("{}")), Metadata: datatypes.JSON([]byte("{}"))}
	if err := tx.WithContext(ctx).Create(node).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}

	a1 := &types.Activity{ID: uuid.New(), Kind: "reading", Title: "a1", Status: "draft"}
	a2 := &types.Activity{ID: uuid.New(), Kind: "reading", Title: "a2", Status: "draft"}
	if err := tx.WithContext(ctx).Create(a1).Error; err != nil {
		t.Fatalf("seed activity1: %v", err)
	}
	if err := tx.WithContext(ctx).Create(a2).Error; err != nil {
		t.Fatalf("seed activity2: %v", err)
	}

	pna1 := &types.PathNodeActivity{ID: uuid.New(), PathNodeID: node.ID, ActivityID: a1.ID, Rank: 0, IsPrimary: true}
	if _, err := repo.Create(ctx, tx, []*types.PathNodeActivity{pna1}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	dup := &types.PathNodeActivity{ID: uuid.New(), PathNodeID: node.ID, ActivityID: a1.ID, Rank: 0, IsPrimary: true}
	pna2 := &types.PathNodeActivity{ID: uuid.New(), PathNodeID: node.ID, ActivityID: a2.ID, Rank: 1, IsPrimary: false}
	if n, err := repo.CreateIgnoreDuplicates(ctx, tx, []*types.PathNodeActivity{dup, pna2}); err != nil || n != 1 {
		t.Fatalf("CreateIgnoreDuplicates: n=%d err=%v", n, err)
	}

	if got, err := repo.GetByID(ctx, tx, pna1.ID); err != nil || got == nil || got.ID != pna1.ID {
		t.Fatalf("GetByID: got=%v err=%v", got, err)
	}
	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{pna1.ID, pna2.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByPathNodeIDs(ctx, tx, []uuid.UUID{node.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByPathNodeIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByActivityIDs(ctx, tx, []uuid.UUID{a1.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByActivityIDs: err=%v len=%d", err, len(rows))
	}

	pna2.Rank = 2
	pna2.IsPrimary = true
	if err := repo.Upsert(ctx, tx, pna2); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	pna1.Rank = 5
	if err := repo.Update(ctx, tx, pna1); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := repo.UpdateFields(ctx, tx, pna1.ID, map[string]interface{}{"rank": 6}); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{pna2.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}
	if err := repo.SoftDeleteByActivityIDs(ctx, tx, []uuid.UUID{a1.ID}); err != nil {
		t.Fatalf("SoftDeleteByActivityIDs: %v", err)
	}
	if err := repo.SoftDeleteByPathNodeIDs(ctx, tx, []uuid.UUID{node.ID}); err != nil {
		t.Fatalf("SoftDeleteByPathNodeIDs: %v", err)
	}

	fd := &types.PathNodeActivity{ID: uuid.New(), PathNodeID: node.ID, ActivityID: a1.ID, Rank: 0, IsPrimary: true}
	if _, err := repo.Create(ctx, tx, []*types.PathNodeActivity{fd}); err != nil {
		t.Fatalf("seed fd: %v", err)
	}
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{fd.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}
	if err := repo.FullDeleteByActivityIDs(ctx, tx, []uuid.UUID{a1.ID}); err != nil {
		t.Fatalf("FullDeleteByActivityIDs: %v", err)
	}
	if err := repo.FullDeleteByPathNodeIDs(ctx, tx, []uuid.UUID{node.ID}); err != nil {
		t.Fatalf("FullDeleteByPathNodeIDs: %v", err)
	}
}

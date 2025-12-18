package learning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func TestActivityCitationRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewActivityCitationRepo(db, testutil.Logger(t))

	// Seed an activity variant
	activity := &types.Activity{ID: uuid.New(), Kind: "reading", Title: "a", Status: "draft"}
	if err := tx.WithContext(ctx).Create(activity).Error; err != nil {
		t.Fatalf("seed activity: %v", err)
	}
	variant := &types.ActivityVariant{ID: uuid.New(), ActivityID: activity.ID, Variant: "short", ContentMD: "content"}
	if err := tx.WithContext(ctx).Create(variant).Error; err != nil {
		t.Fatalf("seed variant: %v", err)
	}

	// Seed material chunks
	u := testutil.SeedUser(t, ctx, tx, "activitycitationrepo@example.com")
	ms := testutil.SeedMaterialSet(t, ctx, tx, u.ID)
	mf := testutil.SeedMaterialFile(t, ctx, tx, ms.ID, "key-activity-citation")
	ch1 := testutil.SeedMaterialChunk(t, ctx, tx, mf.ID, 0)
	ch2 := testutil.SeedMaterialChunk(t, ctx, tx, mf.ID, 1)

	c1 := &types.ActivityCitation{ID: uuid.New(), ActivityVariantID: variant.ID, MaterialChunkID: ch1.ID, Kind: "grounding"}
	if _, err := repo.Create(ctx, tx, []*types.ActivityCitation{c1}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	dup := &types.ActivityCitation{ID: uuid.New(), ActivityVariantID: variant.ID, MaterialChunkID: ch1.ID, Kind: "grounding"}
	c2 := &types.ActivityCitation{ID: uuid.New(), ActivityVariantID: variant.ID, MaterialChunkID: ch2.ID, Kind: "example"}
	if n, err := repo.CreateIgnoreDuplicates(ctx, tx, []*types.ActivityCitation{dup, c2}); err != nil || n != 1 {
		t.Fatalf("CreateIgnoreDuplicates: n=%d err=%v", n, err)
	}

	if got, err := repo.GetByID(ctx, tx, c1.ID); err != nil || got == nil || got.ID != c1.ID {
		t.Fatalf("GetByID: got=%v err=%v", got, err)
	}
	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{c1.ID, c2.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByActivityVariantIDs(ctx, tx, []uuid.UUID{variant.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByActivityVariantIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByMaterialChunkIDs(ctx, tx, []uuid.UUID{ch1.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByMaterialChunkIDs: err=%v len=%d", err, len(rows))
	}

	up := &types.ActivityCitation{ID: c2.ID, ActivityVariantID: variant.ID, MaterialChunkID: ch2.ID, Kind: "grounding"}
	if err := repo.Upsert(ctx, tx, up); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	c1.Kind = "table"
	if err := repo.Update(ctx, tx, c1); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := repo.UpdateFields(ctx, tx, c1.ID, map[string]interface{}{"kind": "figure"}); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{c2.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}
	if err := repo.SoftDeleteByMaterialChunkIDs(ctx, tx, []uuid.UUID{ch1.ID}); err != nil {
		t.Fatalf("SoftDeleteByMaterialChunkIDs: %v", err)
	}
	if err := repo.SoftDeleteByActivityVariantIDs(ctx, tx, []uuid.UUID{variant.ID}); err != nil {
		t.Fatalf("SoftDeleteByActivityVariantIDs: %v", err)
	}

	fd := &types.ActivityCitation{ID: uuid.New(), ActivityVariantID: variant.ID, MaterialChunkID: ch1.ID, Kind: "grounding"}
	if _, err := repo.Create(ctx, tx, []*types.ActivityCitation{fd}); err != nil {
		t.Fatalf("seed fd: %v", err)
	}
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{fd.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}
	if err := repo.FullDeleteByMaterialChunkIDs(ctx, tx, []uuid.UUID{ch1.ID}); err != nil {
		t.Fatalf("FullDeleteByMaterialChunkIDs: %v", err)
	}
	if err := repo.FullDeleteByActivityVariantIDs(ctx, tx, []uuid.UUID{variant.ID}); err != nil {
		t.Fatalf("FullDeleteByActivityVariantIDs: %v", err)
	}
}

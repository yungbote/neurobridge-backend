package learning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/datatypes"
)

func TestActivityVariantRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewActivityVariantRepo(db, testutil.Logger(t))

	activity := &types.Activity{ID: uuid.New(), Kind: "reading", Title: "activity", Status: "draft"}
	if err := tx.WithContext(ctx).Create(activity).Error; err != nil {
		t.Fatalf("seed activity: %v", err)
	}

	v1 := &types.ActivityVariant{
		ID:          uuid.New(),
		ActivityID:  activity.ID,
		Variant:     "short",
		ContentMD:   "v1",
		ContentJSON: datatypes.JSON([]byte("{}")),
	}
	v2 := &types.ActivityVariant{
		ID:          uuid.New(),
		ActivityID:  activity.ID,
		Variant:     "full",
		ContentMD:   "v2",
		ContentJSON: datatypes.JSON([]byte("{}")),
	}
	if _, err := repo.Create(ctx, tx, []*types.ActivityVariant{v1, v2}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got, err := repo.GetByID(ctx, tx, v1.ID); err != nil || got == nil || got.ID != v1.ID {
		t.Fatalf("GetByID: got=%v err=%v", got, err)
	}
	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{v1.ID, v2.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByActivityIDs(ctx, tx, []uuid.UUID{activity.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByActivityIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByActivityAndVariants(ctx, tx, activity.ID, []string{"short"}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByActivityAndVariants: err=%v len=%d", err, len(rows))
	}
	if got, err := repo.GetByActivityAndVariant(ctx, tx, activity.ID, "full"); err != nil || got == nil || got.ID != v2.ID {
		t.Fatalf("GetByActivityAndVariant: got=%v err=%v", got, err)
	}

	up := &types.ActivityVariant{
		ID:          uuid.New(),
		ActivityID:  activity.ID,
		Variant:     "diagram",
		ContentMD:   "d1",
		ContentJSON: datatypes.JSON([]byte("{}")),
		RenderSpec:  datatypes.JSON([]byte("{}")),
	}
	if err := repo.Upsert(ctx, tx, up); err != nil {
		t.Fatalf("Upsert(create): %v", err)
	}
	up.ContentMD = "d2"
	if err := repo.Upsert(ctx, tx, up); err != nil {
		t.Fatalf("Upsert(update): %v", err)
	}
	gotUp, _ := repo.GetByActivityAndVariant(ctx, tx, activity.ID, "diagram")
	if gotUp == nil || gotUp.ContentMD != "d2" {
		t.Fatalf("Upsert verify: got=%v", gotUp)
	}

	v1.ContentMD = "v1b"
	if err := repo.Update(ctx, tx, v1); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := repo.UpdateFields(ctx, tx, v2.ID, map[string]interface{}{"content_md": "v2b"}); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	if err := repo.SoftDeleteByActivityAndVariants(ctx, tx, activity.ID, []string{"diagram"}); err != nil {
		t.Fatalf("SoftDeleteByActivityAndVariants: %v", err)
	}
	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{v1.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}
	if err := repo.SoftDeleteByActivityIDs(ctx, tx, []uuid.UUID{activity.ID}); err != nil {
		t.Fatalf("SoftDeleteByActivityIDs: %v", err)
	}

	// Full delete variants
	v3 := &types.ActivityVariant{ID: uuid.New(), ActivityID: activity.ID, Variant: "fd1", ContentMD: "x"}
	v4 := &types.ActivityVariant{ID: uuid.New(), ActivityID: activity.ID, Variant: "fd2", ContentMD: "y"}
	if _, err := repo.Create(ctx, tx, []*types.ActivityVariant{v3, v4}); err != nil {
		t.Fatalf("seed for full delete: %v", err)
	}
	if err := repo.FullDeleteByActivityAndVariants(ctx, tx, activity.ID, []string{"fd1"}); err != nil {
		t.Fatalf("FullDeleteByActivityAndVariants: %v", err)
	}
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{v4.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}
	if err := repo.FullDeleteByActivityIDs(ctx, tx, []uuid.UUID{activity.ID}); err != nil {
		t.Fatalf("FullDeleteByActivityIDs: %v", err)
	}
}

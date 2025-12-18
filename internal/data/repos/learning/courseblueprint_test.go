package learning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/datatypes"
)

func TestCourseBlueprintRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewCourseBlueprintRepo(db, testutil.Logger(t))

	u := testutil.SeedUser(t, ctx, tx, "courseblueprintrepo@example.com")
	ms := testutil.SeedMaterialSet(t, ctx, tx, u.ID)

	b := &types.CourseBlueprint{
		ID:            uuid.New(),
		MaterialSetID: ms.ID,
		UserID:        u.ID,
		BlueprintJSON: datatypes.JSON([]byte("{}")),
	}
	if _, err := repo.Create(ctx, tx, []*types.CourseBlueprint{b}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{b.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByMaterialSetIDs(ctx, tx, []uuid.UUID{ms.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByMaterialSetIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByUserIDs(ctx, tx, []uuid.UUID{u.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByUserIDs: err=%v len=%d", err, len(rows))
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{b.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}
	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{b.ID}); err != nil || len(rows) != 0 {
		t.Fatalf("after SoftDeleteByIDs GetByIDs: err=%v len=%d", err, len(rows))
	}

	b2 := &types.CourseBlueprint{
		ID:            uuid.New(),
		MaterialSetID: ms.ID,
		UserID:        u.ID,
		BlueprintJSON: datatypes.JSON([]byte("{}")),
	}
	if _, err := repo.Create(ctx, tx, []*types.CourseBlueprint{b2}); err != nil {
		t.Fatalf("seed b2: %v", err)
	}
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{b2.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}
}

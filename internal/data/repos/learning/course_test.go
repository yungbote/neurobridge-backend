package learning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/datatypes"
)

func TestCourseRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewCourseRepo(db, testutil.Logger(t))

	u := testutil.SeedUser(t, ctx, tx, "courserepo@example.com")
	ms := testutil.SeedMaterialSet(t, ctx, tx, u.ID)
	c := &types.Course{
		ID:            uuid.New(),
		UserID:        u.ID,
		MaterialSetID: testutil.PtrUUID(ms.ID),
		Title:         "course",
		Status:        "draft",
		Metadata:      datatypes.JSON([]byte("{}")),
	}
	if _, err := repo.Create(ctx, tx, []*types.Course{c}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{c.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByUserIDs(ctx, tx, []uuid.UUID{u.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByUserIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByMaterialSetIDs(ctx, tx, []uuid.UUID{ms.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByMaterialSetIDs: err=%v len=%d", err, len(rows))
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{c.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}
	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{c.ID}); err != nil || len(rows) != 0 {
		t.Fatalf("after SoftDeleteByIDs GetByIDs: err=%v len=%d", err, len(rows))
	}

	c2 := testutil.SeedCourse(t, ctx, tx, u.ID, testutil.PtrUUID(ms.ID))
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{c2.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}
	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{c2.ID}); err != nil || len(rows) != 0 {
		t.Fatalf("after FullDeleteByIDs GetByIDs: err=%v len=%d", err, len(rows))
	}
}

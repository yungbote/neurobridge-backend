package learning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/datatypes"
)

func TestCourseModuleRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewCourseModuleRepo(db, testutil.Logger(t))

	u := testutil.SeedUser(t, ctx, tx, "coursemodule@example.com")
	ms := testutil.SeedMaterialSet(t, ctx, tx, u.ID)
	course := testutil.SeedCourse(t, ctx, tx, u.ID, testutil.PtrUUID(ms.ID))

	m1 := &types.CourseModule{
		ID:       uuid.New(),
		CourseID: course.ID,
		Index:    0,
		Title:    "m1",
		Metadata: datatypes.JSON([]byte("{}")),
	}
	if _, err := repo.Create(ctx, tx, []*types.CourseModule{m1}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{m1.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByCourseIDs(ctx, tx, []uuid.UUID{course.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByCourseIDs: err=%v len=%d", err, len(rows))
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{m1.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}

	m2 := testutil.SeedCourseModule(t, ctx, tx, course.ID, 1)
	if err := repo.SoftDeleteByCourseIDs(ctx, tx, []uuid.UUID{course.ID}); err != nil {
		t.Fatalf("SoftDeleteByCourseIDs: %v", err)
	}
	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{m2.ID}); err != nil || len(rows) != 0 {
		t.Fatalf("after SoftDeleteByCourseIDs GetByIDs: err=%v len=%d", err, len(rows))
	}

	m3 := testutil.SeedCourseModule(t, ctx, tx, course.ID, 2)
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{m3.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}

	m4 := testutil.SeedCourseModule(t, ctx, tx, course.ID, 3)
	if err := repo.FullDeleteByCourseIDs(ctx, tx, []uuid.UUID{course.ID}); err != nil {
		t.Fatalf("FullDeleteByCourseIDs: %v", err)
	}
	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{m4.ID}); err != nil || len(rows) != 0 {
		t.Fatalf("after FullDeleteByCourseIDs GetByIDs: err=%v len=%d", err, len(rows))
	}
}

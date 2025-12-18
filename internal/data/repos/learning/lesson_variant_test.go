package learning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func TestLessonVariantRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewLessonVariantRepo(db, testutil.Logger(t))

	u := testutil.SeedUser(t, ctx, tx, "lessonvariantrepo@example.com")
	course := testutil.SeedCourse(t, ctx, tx, u.ID, nil)
	module := testutil.SeedCourseModule(t, ctx, tx, course.ID, 0)
	lesson := testutil.SeedLesson(t, ctx, tx, module.ID, 0)

	v1 := &types.LessonVariant{ID: uuid.New(), LessonID: lesson.ID, Variant: "short", ContentMD: "v1"}
	v2 := &types.LessonVariant{ID: uuid.New(), LessonID: lesson.ID, Variant: "full", ContentMD: "v2"}
	if _, err := repo.Create(ctx, tx, []*types.LessonVariant{v1, v2}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got, err := repo.GetByID(ctx, tx, v1.ID); err != nil || got == nil || got.ID != v1.ID {
		t.Fatalf("GetByID: got=%v err=%v", got, err)
	}
	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{v1.ID, v2.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByLessonIDs(ctx, tx, []uuid.UUID{lesson.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByLessonIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByLessonAndVariants(ctx, tx, lesson.ID, []string{"short"}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByLessonAndVariants: err=%v len=%d", err, len(rows))
	}
	if got, err := repo.GetByLessonAndVariant(ctx, tx, lesson.ID, "full"); err != nil || got == nil || got.ID != v2.ID {
		t.Fatalf("GetByLessonAndVariant: got=%v err=%v", got, err)
	}

	up := &types.LessonVariant{ID: uuid.New(), LessonID: lesson.ID, Variant: "diagram", ContentMD: "d1"}
	if err := repo.Upsert(ctx, tx, up); err != nil {
		t.Fatalf("Upsert(create): %v", err)
	}
	up.ContentMD = "d2"
	if err := repo.Upsert(ctx, tx, up); err != nil {
		t.Fatalf("Upsert(update): %v", err)
	}

	v1.ContentMD = "v1b"
	if err := repo.Update(ctx, tx, v1); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := repo.UpdateFields(ctx, tx, v2.ID, map[string]interface{}{"content_md": "v2b"}); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	if err := repo.SoftDeleteByLessonAndVariants(ctx, tx, lesson.ID, []string{"diagram"}); err != nil {
		t.Fatalf("SoftDeleteByLessonAndVariants: %v", err)
	}
	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{v1.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}
	if err := repo.SoftDeleteByLessonIDs(ctx, tx, []uuid.UUID{lesson.ID}); err != nil {
		t.Fatalf("SoftDeleteByLessonIDs: %v", err)
	}

	fd1 := &types.LessonVariant{ID: uuid.New(), LessonID: lesson.ID, Variant: "fd1", ContentMD: "x"}
	fd2 := &types.LessonVariant{ID: uuid.New(), LessonID: lesson.ID, Variant: "fd2", ContentMD: "y"}
	if _, err := repo.Create(ctx, tx, []*types.LessonVariant{fd1, fd2}); err != nil {
		t.Fatalf("seed for full delete: %v", err)
	}
	if err := repo.FullDeleteByLessonAndVariants(ctx, tx, lesson.ID, []string{"fd1"}); err != nil {
		t.Fatalf("FullDeleteByLessonAndVariants: %v", err)
	}
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{fd2.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}
	if err := repo.FullDeleteByLessonIDs(ctx, tx, []uuid.UUID{lesson.ID}); err != nil {
		t.Fatalf("FullDeleteByLessonIDs: %v", err)
	}
}

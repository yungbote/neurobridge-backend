package learning

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func TestLessonConceptRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewLessonConceptRepo(db, testutil.Logger(t))

	u := testutil.SeedUser(t, ctx, tx, "lessonconceptrepo@example.com")
	course := testutil.SeedCourse(t, ctx, tx, u.ID, nil)
	module := testutil.SeedCourseModule(t, ctx, tx, course.ID, 0)
	lesson := testutil.SeedLesson(t, ctx, tx, module.ID, 0)

	cc1 := &types.CourseConcept{ID: uuid.New(), CourseID: course.ID, Key: "k1", Name: "K1"}
	cc2 := &types.CourseConcept{ID: uuid.New(), CourseID: course.ID, Key: "k2", Name: "K2"}
	if err := tx.WithContext(ctx).Create(cc1).Error; err != nil {
		t.Fatalf("seed course concept1: %v", err)
	}
	if err := tx.WithContext(ctx).Create(cc2).Error; err != nil {
		t.Fatalf("seed course concept2: %v", err)
	}

	lc1 := &types.LessonConcept{ID: uuid.New(), LessonID: lesson.ID, CourseConceptID: cc1.ID}
	if _, err := repo.Create(ctx, tx, []*types.LessonConcept{lc1}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	dup := &types.LessonConcept{ID: uuid.New(), LessonID: lesson.ID, CourseConceptID: cc1.ID}
	lc2 := &types.LessonConcept{ID: uuid.New(), LessonID: lesson.ID, CourseConceptID: cc2.ID}
	if n, err := repo.CreateIgnoreDuplicates(ctx, tx, []*types.LessonConcept{dup, lc2}); err != nil || n != 1 {
		t.Fatalf("CreateIgnoreDuplicates: n=%d err=%v", n, err)
	}

	if got, err := repo.GetByID(ctx, tx, lc1.ID); err != nil || got == nil || got.ID != lc1.ID {
		t.Fatalf("GetByID: got=%v err=%v", got, err)
	}
	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{lc1.ID, lc2.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByLessonIDs(ctx, tx, []uuid.UUID{lesson.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByLessonIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByCourseConceptIDs(ctx, tx, []uuid.UUID{cc1.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByCourseConceptIDs: err=%v len=%d", err, len(rows))
	}

	if err := repo.UpdateFields(ctx, tx, lc1.ID, map[string]interface{}{"updated_at": time.Now().UTC()}); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{lc2.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}
	if err := repo.SoftDeleteByCourseConceptIDs(ctx, tx, []uuid.UUID{cc1.ID}); err != nil {
		t.Fatalf("SoftDeleteByCourseConceptIDs: %v", err)
	}
	if err := repo.SoftDeleteByLessonIDs(ctx, tx, []uuid.UUID{lesson.ID}); err != nil {
		t.Fatalf("SoftDeleteByLessonIDs: %v", err)
	}

	fd := &types.LessonConcept{ID: uuid.New(), LessonID: lesson.ID, CourseConceptID: cc1.ID}
	if _, err := repo.Create(ctx, tx, []*types.LessonConcept{fd}); err != nil {
		t.Fatalf("seed fd: %v", err)
	}
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{fd.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}
	if err := repo.FullDeleteByCourseConceptIDs(ctx, tx, []uuid.UUID{cc1.ID}); err != nil {
		t.Fatalf("FullDeleteByCourseConceptIDs: %v", err)
	}
	if err := repo.FullDeleteByLessonIDs(ctx, tx, []uuid.UUID{lesson.ID}); err != nil {
		t.Fatalf("FullDeleteByLessonIDs: %v", err)
	}
}

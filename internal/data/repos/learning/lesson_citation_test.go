package learning

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func TestLessonCitationRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewLessonCitationRepo(db, testutil.Logger(t))

	u := testutil.SeedUser(t, ctx, tx, "lessoncitationrepo@example.com")
	course := testutil.SeedCourse(t, ctx, tx, u.ID, nil)
	module := testutil.SeedCourseModule(t, ctx, tx, course.ID, 0)
	lesson := testutil.SeedLesson(t, ctx, tx, module.ID, 0)

	ms := testutil.SeedMaterialSet(t, ctx, tx, u.ID)
	mf := testutil.SeedMaterialFile(t, ctx, tx, ms.ID, "key-lesson-citation")
	ch1 := testutil.SeedMaterialChunk(t, ctx, tx, mf.ID, 0)
	ch2 := testutil.SeedMaterialChunk(t, ctx, tx, mf.ID, 1)

	c1 := &types.LessonCitation{ID: uuid.New(), LessonID: lesson.ID, MaterialChunkID: ch1.ID}
	if _, err := repo.Create(ctx, tx, []*types.LessonCitation{c1}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	dup := &types.LessonCitation{ID: uuid.New(), LessonID: lesson.ID, MaterialChunkID: ch1.ID}
	c2 := &types.LessonCitation{ID: uuid.New(), LessonID: lesson.ID, MaterialChunkID: ch2.ID}
	if n, err := repo.CreateIgnoreDuplicates(ctx, tx, []*types.LessonCitation{dup, c2}); err != nil || n != 1 {
		t.Fatalf("CreateIgnoreDuplicates: n=%d err=%v", n, err)
	}

	if got, err := repo.GetByID(ctx, tx, c1.ID); err != nil || got == nil || got.ID != c1.ID {
		t.Fatalf("GetByID: got=%v err=%v", got, err)
	}
	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{c1.ID, c2.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByLessonIDs(ctx, tx, []uuid.UUID{lesson.ID}); err != nil || len(rows) != 2 {
		t.Fatalf("GetByLessonIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByMaterialChunkIDs(ctx, tx, []uuid.UUID{ch1.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByMaterialChunkIDs: err=%v len=%d", err, len(rows))
	}

	if err := repo.UpdateFields(ctx, tx, c1.ID, map[string]interface{}{"updated_at": time.Now().UTC()}); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{c2.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}
	if err := repo.SoftDeleteByMaterialChunkIDs(ctx, tx, []uuid.UUID{ch1.ID}); err != nil {
		t.Fatalf("SoftDeleteByMaterialChunkIDs: %v", err)
	}
	if err := repo.SoftDeleteByLessonIDs(ctx, tx, []uuid.UUID{lesson.ID}); err != nil {
		t.Fatalf("SoftDeleteByLessonIDs: %v", err)
	}

	fd := &types.LessonCitation{ID: uuid.New(), LessonID: lesson.ID, MaterialChunkID: ch1.ID}
	if _, err := repo.Create(ctx, tx, []*types.LessonCitation{fd}); err != nil {
		t.Fatalf("seed fd: %v", err)
	}
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{fd.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}
	if err := repo.FullDeleteByMaterialChunkIDs(ctx, tx, []uuid.UUID{ch1.ID}); err != nil {
		t.Fatalf("FullDeleteByMaterialChunkIDs: %v", err)
	}
	if err := repo.FullDeleteByLessonIDs(ctx, tx, []uuid.UUID{lesson.ID}); err != nil {
		t.Fatalf("FullDeleteByLessonIDs: %v", err)
	}
}

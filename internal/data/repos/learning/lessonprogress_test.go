package learning

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/datatypes"
)

func TestLessonProgressRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewLessonProgressRepo(db, testutil.Logger(t))

	u := testutil.SeedUser(t, ctx, tx, "lessonprogressrepo@example.com")
	course := testutil.SeedCourse(t, ctx, tx, u.ID, nil)
	module := testutil.SeedCourseModule(t, ctx, tx, course.ID, 0)
	lesson := testutil.SeedLesson(t, ctx, tx, module.ID, 0)

	lp := &types.LessonProgress{
		ID:               uuid.New(),
		UserID:           u.ID,
		LessonID:         lesson.ID,
		Status:           "not_started",
		TimeSpentSeconds: 0,
		Metadata:         datatypes.JSON([]byte("{}")),
	}
	if _, err := repo.Create(ctx, tx, []*types.LessonProgress{lp}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{lp.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByUserID(ctx, tx, u.ID); err != nil || len(rows) != 1 {
		t.Fatalf("GetByUserID: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByUserAndLessonIDs(ctx, tx, u.ID, []uuid.UUID{lesson.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByUserAndLessonIDs: err=%v len=%d", err, len(rows))
	}

	now := time.Now().UTC()
	lpUp := &types.LessonProgress{
		ID:               lp.ID,
		UserID:           u.ID,
		LessonID:         lesson.ID,
		Status:           "completed",
		CompletedAt:      &now,
		Metadata:         datatypes.JSON([]byte("{}")),
		TimeSpentSeconds: 12,
	}
	if err := repo.Upsert(ctx, tx, lpUp); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	after, _ := repo.GetByUserAndLessonIDs(ctx, tx, u.ID, []uuid.UUID{lesson.ID})
	if len(after) != 1 || after[0].Status != "completed" {
		t.Fatalf("Upsert verify: %+v", after)
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{lp.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}
	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{lp.ID}); err != nil || len(rows) != 0 {
		t.Fatalf("after SoftDeleteByIDs GetByIDs: err=%v len=%d", err, len(rows))
	}

	lp2 := &types.LessonProgress{
		ID:       uuid.New(),
		UserID:   u.ID,
		LessonID: lesson.ID,
		Status:   "not_started",
		Metadata: datatypes.JSON([]byte("{}")),
	}
	if _, err := repo.Create(ctx, tx, []*types.LessonProgress{lp2}); err != nil {
		t.Fatalf("seed lp2: %v", err)
	}
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{lp2.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}
}

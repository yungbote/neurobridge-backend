package learning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/datatypes"
)

func TestLessonRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewLessonRepo(db, testutil.Logger(t))

	u := testutil.SeedUser(t, ctx, tx, "lessonrepo@example.com")
	course := testutil.SeedCourse(t, ctx, tx, u.ID, nil)
	module := testutil.SeedCourseModule(t, ctx, tx, course.ID, 0)

	l1 := &types.Lesson{
		ID:          uuid.New(),
		ModuleID:    module.ID,
		Index:       0,
		Title:       "lesson",
		Kind:        "reading",
		ContentMD:   "content",
		SummaryMD:   "summary",
		ContentJSON: datatypes.JSON([]byte("{}")),
		Metadata:    datatypes.JSON([]byte("{}")),
	}
	if _, err := repo.Create(ctx, tx, []*types.Lesson{l1}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{l1.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByModuleIDs(ctx, tx, []uuid.UUID{module.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByModuleIDs: err=%v len=%d", err, len(rows))
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{l1.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}

	l2 := testutil.SeedLesson(t, ctx, tx, module.ID, 1)
	if err := repo.SoftDeleteByModuleIDs(ctx, tx, []uuid.UUID{module.ID}); err != nil {
		t.Fatalf("SoftDeleteByModuleIDs: %v", err)
	}
	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{l2.ID}); err != nil || len(rows) != 0 {
		t.Fatalf("after SoftDeleteByModuleIDs GetByIDs: err=%v len=%d", err, len(rows))
	}

	l3 := testutil.SeedLesson(t, ctx, tx, module.ID, 2)
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{l3.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}

	l4 := testutil.SeedLesson(t, ctx, tx, module.ID, 3)
	if err := repo.FullDeleteByModuleIDs(ctx, tx, []uuid.UUID{module.ID}); err != nil {
		t.Fatalf("FullDeleteByModuleIDs: %v", err)
	}
	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{l4.ID}); err != nil || len(rows) != 0 {
		t.Fatalf("after FullDeleteByModuleIDs GetByIDs: err=%v len=%d", err, len(rows))
	}
}

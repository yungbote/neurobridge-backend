package learning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/datatypes"
)

func TestLessonAssetRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewLessonAssetRepo(db, testutil.Logger(t))

	u := testutil.SeedUser(t, ctx, tx, "lessonassetrepo@example.com")
	course := testutil.SeedCourse(t, ctx, tx, u.ID, nil)
	module := testutil.SeedCourseModule(t, ctx, tx, course.ID, 0)
	lesson := testutil.SeedLesson(t, ctx, tx, module.ID, 0)

	la := &types.LessonAsset{
		ID:         uuid.New(),
		LessonID:   lesson.ID,
		Kind:       "image",
		StorageKey: "asset/key",
		Metadata:   datatypes.JSON([]byte("{}")),
	}
	if _, err := repo.Create(ctx, tx, []*types.LessonAsset{la}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{la.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByLessonIDs(ctx, tx, []uuid.UUID{lesson.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByLessonIDs: err=%v len=%d", err, len(rows))
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{la.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}

	la2 := &types.LessonAsset{ID: uuid.New(), LessonID: lesson.ID, Kind: "video", StorageKey: "asset/key2", Metadata: datatypes.JSON([]byte("{}"))}
	if _, err := repo.Create(ctx, tx, []*types.LessonAsset{la2}); err != nil {
		t.Fatalf("seed la2: %v", err)
	}
	if err := repo.SoftDeleteByLessonIDs(ctx, tx, []uuid.UUID{lesson.ID}); err != nil {
		t.Fatalf("SoftDeleteByLessonIDs: %v", err)
	}

	la3 := &types.LessonAsset{ID: uuid.New(), LessonID: lesson.ID, Kind: "audio", StorageKey: "asset/key3", Metadata: datatypes.JSON([]byte("{}"))}
	if _, err := repo.Create(ctx, tx, []*types.LessonAsset{la3}); err != nil {
		t.Fatalf("seed la3: %v", err)
	}
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{la3.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}

	la4 := &types.LessonAsset{ID: uuid.New(), LessonID: lesson.ID, Kind: "pdf", StorageKey: "asset/key4", Metadata: datatypes.JSON([]byte("{}"))}
	if _, err := repo.Create(ctx, tx, []*types.LessonAsset{la4}); err != nil {
		t.Fatalf("seed la4: %v", err)
	}
	if err := repo.FullDeleteByLessonIDs(ctx, tx, []uuid.UUID{lesson.ID}); err != nil {
		t.Fatalf("FullDeleteByLessonIDs: %v", err)
	}
}

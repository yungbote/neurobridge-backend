package learning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/datatypes"
)

func TestQuizAttemptRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewQuizAttemptRepo(db, testutil.Logger(t))

	u := testutil.SeedUser(t, ctx, tx, "quizattemptrepo@example.com")
	course := testutil.SeedCourse(t, ctx, tx, u.ID, nil)
	module := testutil.SeedCourseModule(t, ctx, tx, course.ID, 0)
	lesson := testutil.SeedLesson(t, ctx, tx, module.ID, 0)

	q := &types.QuizQuestion{
		ID:            uuid.New(),
		LessonID:      lesson.ID,
		Index:         0,
		Type:          "mcq",
		PromptMD:      "prompt",
		Options:       datatypes.JSON([]byte(`["a","b"]`)),
		CorrectAnswer: datatypes.JSON([]byte(`"a"`)),
		Metadata:      datatypes.JSON([]byte("{}")),
	}
	if err := tx.WithContext(ctx).Create(q).Error; err != nil {
		t.Fatalf("seed question: %v", err)
	}

	a1 := &types.QuizAttempt{
		ID:         uuid.New(),
		UserID:     u.ID,
		LessonID:   lesson.ID,
		QuestionID: q.ID,
		IsCorrect:  true,
		Answer:     datatypes.JSON([]byte(`"a"`)),
		Metadata:   datatypes.JSON([]byte("{}")),
	}
	if _, err := repo.Create(ctx, tx, []*types.QuizAttempt{a1}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{a1.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByUserID(ctx, tx, u.ID); err != nil || len(rows) != 1 {
		t.Fatalf("GetByUserID: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByLessonIDs(ctx, tx, []uuid.UUID{lesson.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByLessonIDs: err=%v len=%d", err, len(rows))
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{a1.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}

	a2 := &types.QuizAttempt{
		ID:         uuid.New(),
		UserID:     u.ID,
		LessonID:   lesson.ID,
		QuestionID: q.ID,
		IsCorrect:  false,
		Answer:     datatypes.JSON([]byte(`"b"`)),
		Metadata:   datatypes.JSON([]byte("{}")),
	}
	if _, err := repo.Create(ctx, tx, []*types.QuizAttempt{a2}); err != nil {
		t.Fatalf("seed a2: %v", err)
	}
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{a2.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}
}

package learning

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/testutil"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/datatypes"
)

func TestQuizQuestionRepo(t *testing.T) {
	db := testutil.DB(t)
	tx := testutil.Tx(t, db)

	ctx := context.Background()
	repo := NewQuizQuestionRepo(db, testutil.Logger(t))

	u := testutil.SeedUser(t, ctx, tx, "quizquestionrepo@example.com")
	course := testutil.SeedCourse(t, ctx, tx, u.ID, nil)
	module := testutil.SeedCourseModule(t, ctx, tx, course.ID, 0)
	lesson := testutil.SeedLesson(t, ctx, tx, module.ID, 0)

	q1 := &types.QuizQuestion{
		ID:            uuid.New(),
		LessonID:      lesson.ID,
		Index:         0,
		Type:          "mcq",
		PromptMD:      "prompt",
		Options:       datatypes.JSON([]byte(`["a","b"]`)),
		CorrectAnswer: datatypes.JSON([]byte(`"a"`)),
		Metadata:      datatypes.JSON([]byte("{}")),
	}
	if _, err := repo.Create(ctx, tx, []*types.QuizQuestion{q1}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if rows, err := repo.GetByIDs(ctx, tx, []uuid.UUID{q1.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByIDs: err=%v len=%d", err, len(rows))
	}
	if rows, err := repo.GetByLessonIDs(ctx, tx, []uuid.UUID{lesson.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("GetByLessonIDs: err=%v len=%d", err, len(rows))
	}

	if err := repo.SoftDeleteByIDs(ctx, tx, []uuid.UUID{q1.ID}); err != nil {
		t.Fatalf("SoftDeleteByIDs: %v", err)
	}

	q2 := &types.QuizQuestion{
		ID:            uuid.New(),
		LessonID:      lesson.ID,
		Index:         1,
		Type:          "mcq",
		PromptMD:      "prompt2",
		Options:       datatypes.JSON([]byte(`["a","b"]`)),
		CorrectAnswer: datatypes.JSON([]byte(`"b"`)),
		Metadata:      datatypes.JSON([]byte("{}")),
	}
	if _, err := repo.Create(ctx, tx, []*types.QuizQuestion{q2}); err != nil {
		t.Fatalf("seed q2: %v", err)
	}
	if err := repo.SoftDeleteByLessonIDs(ctx, tx, []uuid.UUID{lesson.ID}); err != nil {
		t.Fatalf("SoftDeleteByLessonIDs: %v", err)
	}

	q3 := &types.QuizQuestion{
		ID:            uuid.New(),
		LessonID:      lesson.ID,
		Index:         2,
		Type:          "mcq",
		PromptMD:      "prompt3",
		Options:       datatypes.JSON([]byte(`["a","b"]`)),
		CorrectAnswer: datatypes.JSON([]byte(`"a"`)),
		Metadata:      datatypes.JSON([]byte("{}")),
	}
	if _, err := repo.Create(ctx, tx, []*types.QuizQuestion{q3}); err != nil {
		t.Fatalf("seed q3: %v", err)
	}
	if err := repo.FullDeleteByIDs(ctx, tx, []uuid.UUID{q3.ID}); err != nil {
		t.Fatalf("FullDeleteByIDs: %v", err)
	}

	q4 := &types.QuizQuestion{
		ID:            uuid.New(),
		LessonID:      lesson.ID,
		Index:         3,
		Type:          "mcq",
		PromptMD:      "prompt4",
		Options:       datatypes.JSON([]byte(`["a","b"]`)),
		CorrectAnswer: datatypes.JSON([]byte(`"b"`)),
		Metadata:      datatypes.JSON([]byte("{}")),
	}
	if _, err := repo.Create(ctx, tx, []*types.QuizQuestion{q4}); err != nil {
		t.Fatalf("seed q4: %v", err)
	}
	if err := repo.FullDeleteByLessonIDs(ctx, tx, []uuid.UUID{lesson.ID}); err != nil {
		t.Fatalf("FullDeleteByLessonIDs: %v", err)
	}
}

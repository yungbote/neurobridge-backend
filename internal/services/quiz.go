package services

import (
	"context"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"github.com/yungbote/neurbridge-backend/internal/types"
)

type QuizService interface {
  GetQuestionsForLesson(ctx context.Context, userID, lessonID uuid.UUID) ([]*types.QuizQuestion, error)
  SubmitAttempt(ctx context.Context, userID, lessonID, questionID uuid.UUID, answer map[string]interface{}) (*types.QuizAttempt, error)
  RegenerateLessonQuiz(ctx context.Context, userID, lessonID uuid.UUID) ([]*types.QuizQuestion, error)
  GetQuizHistoryForLesson(ctx context.Context, userID, lessonID uuid.UUID) ([]map[string]interface{}, error)
}











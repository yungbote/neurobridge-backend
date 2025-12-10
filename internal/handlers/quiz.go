package handlers

import (
  "net/http"
  "github.com/gin-gonic/gin"
  "github.com/google/uuid"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/requestdata"
  "github.com/yungbote/neurobridge-backend/internal/services"
)

type QuizHandler struct {
  log     *logger.Logger
  quizSvc services.QuizService
}

func NewQuizHandler(log *logger.Logger, quizSvc services.QuizService) *QuizHandler {
  return &QuizHandler{
    log:     log.With("handler", "QuizHandler"),
    quizSvc: quizSvc,
  }
}

// GET /api/lessons/:id/quiz
// Get current quiz questions for a lesson.
func (h *QuizHandler) GetLessonQuiz(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// POST /api/quiz-attempts
// Submit an attempt, get correctness + feedback.
func (h *QuizHandler) SubmitQuizAttempt(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}


// POST /api/lessons/:id/quiz/regenerate
// Regenerate quiz questions (e.g. after profile or lesson change).
func (h *QuizHandler) RegenerateLessonQuiz(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// GET /api/lessons/:id/quiz/history
// View previous quiz versions (if you version).
func (h *QuizHandler) GetLessonQuizHistory(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}











package app

import (
	"github.com/yungbote/neurobridge-backend/internal/handlers"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/sse"
)

type Handlers struct {
	Auth    *handlers.AuthHandler
	User    *handlers.UserHandler
	SSE     *handlers.SSEHandler

	Material *handlers.MaterialHandler
	Course   *handlers.CourseHandler
	Module   *handlers.ModuleHandler
	Lesson   *handlers.LessonHandler

	Jobs *handlers.JobsHandler
}

func wireHandlers(log *logger.Logger, services Services, sseHub *sse.SSEHub) Handlers {
	log.Info("Wiring handlers...")
	return Handlers{
		Auth:     handlers.NewAuthHandler(services.Auth),
		User:     handlers.NewUserHandler(services.User),
		SSE:      handlers.NewSSEHandler(log, sseHub),

		Material: handlers.NewMaterialHandler(log, services.Workflow, sseHub),
		Course:   handlers.NewCourseHandler(log, services.Course),
		Module:   handlers.NewModuleHandler(services.Module),
		Lesson:   handlers.NewLessonHandler(services.Lesson),

		Jobs:     handlers.NewJobsHandler(services.JobService),
	}
}











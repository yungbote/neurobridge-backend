package app

import (
	"github.com/yungbote/neurobridge-backend/internal/handlers"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/sse"
)

type Handlers struct {
	Auth										*handlers.AuthHandler
	User										*handlers.UserHandler
	SSE											*handlers.SSEHandler
	Material								*handlers.MaterialHandler
	Course									*handlers.CourseHandler
	CourseGeneration				*handlers.CourseGenHandler
	Module									*handlers.ModuleHandler
	Lesson									*handlers.LessonHandler
}

func wireHandlers(log *logger.Logger, services Services, sseHub *sse.SSEHub) Handlers {
	log.Info("Wiring handlers...")
	return Handlers{
		Auth:									handlers.NewAuthHandler(services.Auth),
		User:									handlers.NewUserHandler(services.User),
		SSE:									handlers.NewSSEHandler(log, sseHub),
		Material:							handlers.NewMaterialHandler(log, services.Material, services.CourseGeneration, sseHub),
		Course:								handlers.NewCourseHandler(log, services.Course),
		CourseGeneration:			handlers.NewCourseGenHandler(services.CourseGenerationStatus),
		Module:								handlers.NewModuleHandler(services.Module),
		Lesson:								handlers.NewLessonHandler(services.Lesson),
	}
}











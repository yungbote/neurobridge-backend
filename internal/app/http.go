package app

import (
	"github.com/yungbote/neurobridge-backend/internal/http"
	httpH "github.com/yungbote/neurobridge-backend/internal/http/handlers"
	httpMW "github.com/yungbote/neurobridge-backend/internal/http/middleware"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/sse"
)

type Middleware struct {
	Auth			*httpMW.AuthMiddleware
}

type Handlers struct {
	Auth		 *httpH.AuthHandler
	User		 *httpH.UserHandler
	Realtime *httpH.RealtimeHandler
	Material *httpH.MaterialHandler
	Course   *httpH.CourseHandler
	Module   *httpH.ModuleHandler
	Lesson   *httpH.LessonHandler
	Jobs		 *httpH.JobsHandler
}

func wireHandlers(log *logger.Logger, services Services, sseHub *sse.SSEHub) Handlers {
	log.Info("Wiring handlers...")
	return Handlers{
		Auth:     httpH.NewAuthHandler(services.Auth),
		User:     httpH.NewUserHandler(services.User),
		Realtime: httpH.NewRealtimeHandler(log, sseHub),
		Material: httpH.NewMaterialHandler(log, services.Workflow, sseHub),
		Course:   httpH.NewCourseHandler(log, services.Course),
		Module:   httpH.NewModuleHandler(services.Module),
		Lesson:		httpH.NewLessonHandler(services.Lesson, services.JobService),
		Job:      httpH.NewJobHandler(services.JobService),
	}
}

func wireRouter(handlers Handlers, middleware Middleware) *gin.Engine {
	return http.NewRouter(http.RouterConfig{
		AuthHandler:       handlers.Auth,
		AuthMiddleware:    middleware.Auth,
		UserHandler:       handlers.User,
		RealtimeHandler:   handlers.Realtime,
		MaterialHandler:   handlers.Material,
		CourseHandler:     handlers.Course,
		ModuleHandler:     handlers.Module,
		LessonHandler:     handlers.Lesson,
		JobHandler:				 handlers.Job,
	})
}


func wireMiddleware(log *logger.Logger, services Services) Middleware {
	log.Info("Wiring middleware...")
	return Middleware{
		Auth:								httpMW.NewAuthMiddleware(log, services.Auth),
	}
}











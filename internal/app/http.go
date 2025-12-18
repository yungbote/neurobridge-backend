package app

import (
	"github.com/gin-gonic/gin"
	"github.com/yungbote/neurobridge-backend/internal/http"
	httpH "github.com/yungbote/neurobridge-backend/internal/http/handlers"
	httpMW "github.com/yungbote/neurobridge-backend/internal/http/middleware"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/realtime"
)

type Middleware struct {
	Auth *httpMW.AuthMiddleware
}

type Handlers struct {
	Health   *httpH.HealthHandler
	Auth     *httpH.AuthHandler
	User     *httpH.UserHandler
	Realtime *httpH.RealtimeHandler
	Material *httpH.MaterialHandler
	Course   *httpH.CourseHandler
	Module   *httpH.ModuleHandler
	Lesson   *httpH.LessonHandler
	Event    *httpH.EventHandler
	Job      *httpH.JobHandler
}

func wireHandlers(log *logger.Logger, services Services, sseHub *realtime.SSEHub) Handlers {
	log.Info("Wiring handlers...")
	return Handlers{
		Health:   httpH.NewHealthHandler(),
		Auth:     httpH.NewAuthHandler(services.Auth),
		User:     httpH.NewUserHandler(services.User),
		Realtime: httpH.NewRealtimeHandler(log, sseHub),
		Material: httpH.NewMaterialHandler(log, services.Workflow, sseHub),
		Course:   httpH.NewCourseHandler(log, services.Course),
		Module:   httpH.NewModuleHandler(services.Module),
		Lesson:   httpH.NewLessonHandler(services.Lesson, services.JobService),
		Event:    httpH.NewEventHandler(services.Events, services.JobService),
		Job:      httpH.NewJobHandler(services.JobService),
	}
}

func wireRouter(handlers Handlers, middleware Middleware) *gin.Engine {
	return http.NewRouter(http.RouterConfig{
		HealthHandler:   handlers.Health,
		AuthHandler:     handlers.Auth,
		AuthMiddleware:  middleware.Auth,
		UserHandler:     handlers.User,
		RealtimeHandler: handlers.Realtime,
		MaterialHandler: handlers.Material,
		CourseHandler:   handlers.Course,
		ModuleHandler:   handlers.Module,
		LessonHandler:   handlers.Lesson,
		EventHandler:    handlers.Event,
		JobHandler:      handlers.Job,
	})
}

func wireMiddleware(log *logger.Logger, services Services) Middleware {
	log.Info("Wiring middleware...")
	return Middleware{
		Auth: httpMW.NewAuthMiddleware(log, services.Auth),
	}
}

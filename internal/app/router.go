package app

import (
	"github.com/gin-gonic/gin"
	"github.com/yungbote/neurobridge-backend/internal/server"
)

func wireRouter(handlers Handlers, middleware Middleware) *gin.Engine {
	return server.NewRouter(server.RouterConfig{
		AuthHandler:							handlers.Auth,
		AuthMiddleware:						middleware.Auth,
		UserHandler:							handlers.User,
		SSEHandler:								handlers.SSE,
		MaterialHandler:					handlers.Material,
		CourseHandler:						handlers.Course,
		CourseGenHandler:	handlers.CourseGeneration,
		ModuleHandler:						handlers.Module,
		LessonHandler:						handlers.Lesson,
	})
}











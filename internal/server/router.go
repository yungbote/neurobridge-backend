package server

import (
  "github.com/gin-gonic/gin"
  "github.com/gin-contrib/cors"
  "github.com/yungbote/neurobridge-backend/internal/handlers"
  "github.com/yungbote/neurobridge-backend/internal/middleware"
)

type RouterConfig struct {
  AuthHandler       *handlers.AuthHandler
  AuthMiddleware    *middleware.AuthMiddleware
  UserHandler       *handlers.UserHandler
}

func NewRouter(cfg RouterConfig) *gin.Engine {
  router := gin.Default()

  // Cors
  router.Use(cors.New(cors.Config{
    AllowOrigins: []string{
      "http://localhost:3000",
      "http://localhost:5174"
    },
    AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"}
    AllowHeaders:     []string{"Authorization", "Content-Type", "X-Requested-With"},
    AllowCredentials: true,
  }))

  // Public
  router.POST("/register", cfg.AuthHandler.Register)
  router.POST("/login", cfg.AuthHandler.Login)

  // Protected
  protected := router.Group("/")
  protected.Use(cfg.AuthMiddleware.RequireAuth())
  // Auth
  protected.POST("/refresh", cfg.AuthHandler.Refresh)
  protected.POST("/logout", cfg.AuthHandler.Logout)
  // User
  protected.GET("/user", cfg.UserHandler.GetMe)

  return router
}

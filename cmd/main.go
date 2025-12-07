package main

import (
  "fmt"
  "os"
  "time"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/utils"
  "github.com/yungbote/neurobridge-backend/internal/db"
  "github.com/yungbote/neurobridge-backend/internal/repos"
  "github.com/yungbote/neurobridge-backend/internal/services"
  "github.com/yungbote/neurobridge-backend/internal/handlers"
  "github.com/yungbote/neurobridge-backend/internal/middleware"
  "github.com/yungbote/neurobridge-backend/internal/server"
)

func main() {
  // Logger
  logMode := os.GetEnv("LOG_MODE")
  if logMode == "" {
    logMode = "development"
  }
  log, err := logger.New(logMode)
  if err != nil {
    fmt.Printf("Failed to init logger: %v\n", err)
    os.Exit(1)
  }
  defer log.Sync()
  
  // Env
  log.Info("Loading environment variables from main...")
  jwtSecretKey := utils.GetEnv("JWT_SECRET_KEY", "defaultsecret", log)
  accessTokenTTL := utils.GetEnvAsInt("ACCESS_TOKEN_TTL", 3600, log)
  refreshTokenTTL := utils.GetEnvAsInt("REFRESH_TOKEN_TTL", 86400, log)
  
  //Postgres
  postgresService, err := db.NewPostgresService(log)
  if err != nil {
    log.Warn("Postgres init failed", "error", err)
  }
  if err = postgresService.AutoMigrateAll(); err != nil {
    log.Warn("Postgres auto migration failed", "error", err)
  }
  thePG := postgresService.DB()
  
  // Repos
  log.Info("Setting up Repos from main...")
  userRepo := repos.NewUserRepo(thePG, log)
  userTokenRepo := repos.NewUserTokenRepo(thePG, log)

  // Services
  log.Info("Setting up Services from main...")
  bucketService, err := services.NewBucketService(log)
  if err != nil {
    log.Warn("Could not init BucketService", "error", err)
  }
  avatarService, err := services.NewAvatarService(thePG, log, userRepo, bucketService)
  if err != nil {
    log.Error("Could not init AvatarService", "error", err)
    os.Exit(1)
  }
  authService := services.NewAuthService(thePG, log, userRepo, avatarService, userTokenRepo, jwtSecretKey, time.Duration(accessTokenTTL)*time.Second, time.Duration(refreshTokenTTL)*time.Second)
  userService := services.NewUserService(thePG, log, userRepo)

  // Handlers
  log.Info("Setting up handlers from main...")
  authHandler := handlers.NewAuthHandler(authService)
  userHandler := handlers.NewUserHandler(userService)

  // Middleware
  log.Info("Setting up middleware from main...")
  authMiddleware := middleware.NewAuthMiddleware(log, authService)

  // Router
  log.Info("Setting up router from main...")
  router := server.NewRouter(server.RouterConfig{
    AuthHandler:          authHandler,
    AuthMiddleware:       authMiddleware,
    UserHandler:          userHandler
  })

  port := utils.GetEnv("PORT", "8080", log)
  fmt.Printf("Server listening on :%s\n", port)
  if err := router.Run(":" + port); err != nil {
    log.Warn("Server failed: %v", err)
  }
}

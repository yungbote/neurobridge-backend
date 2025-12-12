package db

import (
  "fmt"
  "gorm.io/driver/postgres"
  "gorm.io/gorm"
  "github.com/yungbote/neurobridge-backend/internal/types"
  "github.com/yungbote/neurobridge-backend/internal/utils"
  "github.com/yungbote/neurobridge-backend/internal/logger"
)

type PostgresService struct {
  db *gorm.DB
  log *logger.Logger
}

func NewPostgresService(log *logger.Logger) (*PostgresService, error) {
  serviceLog := log.With("service", "PostgresService")
  
  log.Info("Loading environment variables...")
  postgresHost := utils.GetEnv("POSTGRES_HOST", "localhost", log)
  postgresPort := utils.GetEnv("POSTGRES_PORT", "5432", log)
  postgresUser := utils.GetEnv("POSTGRES_USER", "postgres", log)
  postgresPassword := utils.GetEnv("POSTGRES_PASSWORD", "", log)
  postgresName := utils.GetEnv("POSTGRES_NAME", "neurobridge", log)
  log.Debug("Environment variables loaded")

  dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", postgresUser, postgresPassword, postgresHost, postgresPort, postgresName)
  
  log.Info("Connecting to Postgres...")
  db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
    DisableForeignKeyConstraintWhenMigrating: true,
  })
  if err != nil {
    log.Error("Failed to connect to Postgres", "error", err)
    return nil, fmt.Errorf("Failed to connect to Postgres: %w", err)
  }

  if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp";`).Error; err != nil {
    log.Error("Failed to enable uuid-ossp extension", "error", err)
    return nil, fmt.Errorf("Failed to enable uuid-ossp extension: %w", err)
  }
  log.Info("uuid-ossp extension enabled")

  return &PostgresService{db: db, log: serviceLog}, nil
}

func (s *PostgresService) AutoMigrateAll() error {
  s.log.Info("Auto migrating postgres tables...")
  err := s.db.AutoMigrate(
    &types.User{},
    &types.UserToken{},
    &types.MaterialSet{},
    &types.MaterialFile{},
    &types.Course{},
    &types.CourseModule{},
    &types.Lesson{},
    &types.QuizQuestion{},
    &types.CourseBlueprint{},
    &types.LessonAsset{},
    &types.LearningProfile{},
    &types.TopicMastery{},
    &types.LessonProgress{},
    &types.QuizAttempt{},
    &types.UserEvent{},
    &types.AICallLog{},
    &types.MaterialChunk{},
  )
  if err != nil {
    s.log.Error("Auto migration failed for postgres tables", "error", err)
    return err
  }
  s.log.Info("Configuring foreign key relationships for postgres tables...")
  if err := s.db.Exec(`
    ALTER TABLE "user_token"
    ADD CONSTRAINT "fk_user_token_user_id"
    FOREIGN KEY ("user_id")
    REFERENCES "user"("id")
    ON DELETE CASCADE
  `).Error; err != nil {
    return fmt.Errorf("Failed to add fk_user_token_user_id: %w", err)
  }
  return nil
}

func (s *PostgresService) DB() *gorm.DB {
  return s.db
}











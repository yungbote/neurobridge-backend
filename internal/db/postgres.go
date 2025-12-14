package db

import (
  "fmt"
  "log"
  "os"
  "time"

  "gorm.io/driver/postgres"
  "gorm.io/gorm"
  gormLogger "gorm.io/gorm/logger"

  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/types"
  "github.com/yungbote/neurobridge-backend/internal/utils"
)

type PostgresService struct {
  db  *gorm.DB
  log *logger.Logger
}

func NewPostgresService(logg *logger.Logger) (*PostgresService, error) {
  serviceLog := logg.With("service", "PostgresService")

  logg.Info("Loading environment variables...")
  postgresHost := utils.GetEnv("POSTGRES_HOST", "localhost", logg)
  postgresPort := utils.GetEnv("POSTGRES_PORT", "5432", logg)
  postgresUser := utils.GetEnv("POSTGRES_USER", "postgres", logg)
  postgresPassword := utils.GetEnv("POSTGRES_PASSWORD", "", logg)
  postgresName := utils.GetEnv("POSTGRES_NAME", "neurobridge", logg)
  logg.Debug("Environment variables loaded")

  dsn := fmt.Sprintf(
    "postgres://%s:%s@%s:%s/%s?sslmode=disable",
    postgresUser,
    postgresPassword,
    postgresHost,
    postgresPort,
    postgresName,
  )

  // GORM logger: ignore "record not found" spam (critical for polling workers)
  gormLog := gormLogger.New(
    log.New(os.Stdout, "\r\n", log.LstdFlags),
    gormLogger.Config{
      SlowThreshold:             1 * time.Second,
      LogLevel:                  gormLogger.Warn, // use Info if you want all SQL
      IgnoreRecordNotFoundError: true,
      Colorful:                  false,
    },
  )

  logg.Info("Connecting to Postgres...")
  db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
    DisableForeignKeyConstraintWhenMigrating: true,
    Logger:                                   gormLog,
  })
  if err != nil {
    logg.Error("Failed to connect to Postgres", "error", err)
    return nil, fmt.Errorf("failed to connect to Postgres: %w", err)
  }

  if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp";`).Error; err != nil {
    logg.Error("Failed to enable uuid-ossp extension", "error", err)
    return nil, fmt.Errorf("failed to enable uuid-ossp extension: %w", err)
  }
  logg.Info("uuid-ossp extension enabled")

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
    &types.MaterialChunk{},
    &types.TopicStylePreference{},
    &types.JobRun{},
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
    return fmt.Errorf("failed to add fk_user_token_user_id: %w", err)
  }

  return nil
}

func (s *PostgresService) DB() *gorm.DB {
  return s.db
}











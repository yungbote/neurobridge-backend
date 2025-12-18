package db

import (
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"

	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/utils"
)

type PostgresService struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewPostgresService(logg *logger.Logger) (*PostgresService, error) {
	serviceLog := logg.With("service", "PostgresService")

	postgresHost := utils.GetEnv("POSTGRES_HOST", "localhost", logg)
	postgresPort := utils.GetEnv("POSTGRES_PORT", "5432", logg)
	postgresUser := utils.GetEnv("POSTGRES_USER", "postgres", logg)
	postgresPassword := utils.GetEnv("POSTGRES_PASSWORD", "", logg)
	postgresName := utils.GetEnv("POSTGRES_NAME", "neurobridge", logg)

	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		postgresUser,
		postgresPassword,
		postgresHost,
		postgresPort,
		postgresName,
	)

	gormLog := gormLogger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		gormLogger.Config{
			SlowThreshold:             1 * time.Second,
			LogLevel:                  gormLogger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   gormLog,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Postgres: %w", err)
	}

	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp";`).Error; err != nil {
		return nil, fmt.Errorf("failed to enable uuid-ossp extension: %w", err)
	}

	return &PostgresService{db: db, log: serviceLog}, nil
}

func (s *PostgresService) DB() *gorm.DB { return s.db }

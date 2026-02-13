package testutil

import (
	"errors"
	"os"
	"sync"
	"testing"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

var errMissingDSN = errors.New("missing TEST_POSTGRES_DSN")

var (
	dbOnce sync.Once
	db     *gorm.DB
	dbErr  error

	logOnce sync.Once
	logg    *logger.Logger
	logErr  error
)

func Logger(tb testing.TB) *logger.Logger {
	tb.Helper()
	logOnce.Do(func() {
		logg, logErr = logger.New("test")
	})
	if logErr != nil {
		tb.Fatalf("failed to init logger: %v", logErr)
	}
	return logg
}

func DB(tb testing.TB) *gorm.DB {
	tb.Helper()

	dbOnce.Do(func() {
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			dbErr = errMissingDSN
			return
		}

		var err error
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
			DisableForeignKeyConstraintWhenMigrating: true,
			Logger:                                   gormLogger.Default.LogMode(gormLogger.Silent),
		})
		if err != nil {
			dbErr = err
			return
		}

		if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp";`).Error; err != nil {
			dbErr = err
			return
		}

		if err := autoMigrateAll(db); err != nil {
			dbErr = err
			return
		}
	})

	if errors.Is(dbErr, errMissingDSN) {
		tb.Skip("set TEST_POSTGRES_DSN to run repo integration tests")
	}
	if dbErr != nil {
		tb.Fatalf("failed to init test db: %v", dbErr)
	}
	return db
}

func Tx(tb testing.TB, db *gorm.DB) *gorm.DB {
	tb.Helper()
	tx := db.Begin()
	if tx.Error != nil {
		tb.Fatalf("begin tx: %v", tx.Error)
	}
	tb.Cleanup(func() {
		_ = tx.Rollback().Error
	})
	return tx
}

func autoMigrateAll(db *gorm.DB) error {
	return db.AutoMigrate(
		&types.User{},
		&types.UserToken{},

		&types.MaterialSet{},
		&types.MaterialSetFile{},
		&types.MaterialFile{},
		&types.MaterialFileSignature{},
		&types.MaterialFileSection{},
		&types.MaterialChunk{},
		&types.MaterialAsset{},

		&types.Concept{},
		&types.Activity{},
		&types.ActivityVariant{},
		&types.ActivityConcept{},
		&types.ActivityCitation{},
		&types.Path{},
		&types.PathNode{},
		&types.PathNodeActivity{},
		&types.PathRun{},
		&types.NodeRun{},
		&types.ActivityRun{},
		&types.PathRunTransition{},
		&types.Asset{},

		&types.UserEvent{},
		&types.UserEventCursor{},
		&types.UserConceptState{},
		&types.UserSkillState{},
		&types.UserConceptEdgeStat{},
		&types.ItemCalibration{},
		&types.UserStylePreference{},
		&types.UserMisconceptionInstance{},
		&types.MisconceptionCausalEdge{},
		&types.MisconceptionResolutionState{},
		&types.UserBeliefSnapshot{},
		&types.InterventionPlan{},
		&types.ConceptReadinessSnapshot{},
		&types.PrereqGateDecision{},
		&types.DocProbe{},
		&types.DocProbeOutcome{},
		&types.DocVariantExposure{},
		&types.DocVariantOutcome{},
		&types.DecisionTrace{},
		&types.StructuralDecisionTrace{},
		&types.GraphVersion{},
		&types.StructuralDriftMetric{},
		&types.RollbackEvent{},

		&types.LearningProfile{},
		&types.TopicMastery{},
		&types.TopicStylePreference{},
		&types.LearningArtifact{},
		&types.JobRun{},
	)
}

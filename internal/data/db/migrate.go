package db

import (
	"fmt"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/gorm"
)

func AutoMigrateAll(db *gorm.DB) error {
	return db.AutoMigrate(

		// =========================
		// Core identity + auth
		// =========================
		&types.User{},
		&types.UserToken{},
		&types.UserIdentity{},
		&types.OAuthNonce{},

		// =========================
		// Materials (uploads + extraction)
		// =========================
		&types.MaterialSet{},
		&types.MaterialFile{},
		&types.MaterialChunk{},
		&types.MaterialAsset{},
		&types.MaterialSetSummary{},

		// =========================
		// Course (legacy centerpiece)
		// =========================
		&types.Course{},
		&types.CourseTag{},
		&types.CourseModule{},
		&types.Lesson{},
		&types.QuizQuestion{},
		&types.CourseBlueprint{},

		// =========================
		// Graph-centric learning (new centerpiece: Path)
		// =========================
		// Concepts + Graph Products
		&types.Concept{},
		&types.ConceptEvidence{},
		&types.ConceptEdge{},
		&types.ConceptCluster{},
		&types.ConceptClusterMember{},
		// Library + Population Priors + Decision Traces
		&types.UserLibraryIndex{},
		&types.CohortPrior{},
		&types.DecisionTrace{},
		// Chain identity + Prios + Completion
		&types.ChainSignature{},
		&types.ChainPrior{},
		&types.UserCompletedUnit{},
		// Activities (+ variants + grounding)
		&types.Activity{},
		&types.ActivityVariant{},
		&types.ActivityVariantStat{},
		&types.ActivityConcept{},
		&types.ActivityCitation{},
		// Path (the non-legacy top-level object)
		&types.Path{},
		&types.PathNode{},
		&types.PathNodeActivity{},
		// Assets (polymorphic ownership)
		&types.Asset{},

		// =========================
		// Personalization backbone
		// =========================
		&types.UserEvent{},
		&types.UserEventCursor{},
		&types.UserConceptState{},
		&types.UserStylePreference{},
		&types.UserProgressionEvent{},
		&types.UserProfileVector{},
		&types.TeachingPattern{},

		// =========================
		// Legacy (keep for now)
		// =========================
		&types.LearningProfile{},
		&types.TopicMastery{},
		&types.TopicStylePreference{},
		&types.QuizAttempt{},

		// =========================
		// Jobs / worker
		// =========================
		&types.SagaRun{},
		&types.SagaAction{},
		&types.JobRun{},
	)
}

func EnsureAuthIndexes(db *gorm.DB) error {
	// uuid-ossp is already enabled in NewPostgresService, but safe to re-run
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp";`).Error; err != nil {
		return fmt.Errorf("enable uuid-ossp: %w", err)
	}
	// user_identity
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_user_identity_user_id ON user_identity(user_id);`).Error; err != nil {
		return fmt.Errorf("create idx_user_identity_user_id: %w", err)
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_user_identity_provider_sub
		ON user_identity(provider, provider_sub)
		WHERE deleted_at IS NULL;
	`).Error; err != nil {
		return fmt.Errorf("create idx_user_identity_provider_sub: %w", err)
	}
	// oauth_nonce
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_oauth_nonce_expires_at ON oauth_nonce(expires_at);`).Error; err != nil {
		return fmt.Errorf("create idx_oauth_nonce_expires_at: %w", err)
	}
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_oauth_nonce_provider_used ON oauth_nonce(provider, used_at);`).Error; err != nil {
		return fmt.Errorf("create idx_oauth_nonce_provider_used: %w", err)
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_nonce_provider_hash_active
		ON oauth_nonce(provider, nonce_hash)
		WHERE deleted_at IS NULL AND used_at IS NULL;
	`).Error; err != nil {
		return fmt.Errorf("create idx_oauth_nonce_provider_hash_active: %w", err)
	}
	return nil
}

func (s *PostgresService) AutoMigrateAll() error {
	s.log.Info("Auto migrating postgres tables...")
	if err := AutoMigrateAll(s.db); err != nil {
		s.log.Error("Auto migration failed", "error", err)
		return err
	}
	if err := EnsureAuthIndexes(s.db); err != nil {
		s.log.Error("Auth index migration failed", "error", err)
		return err
	}

	return nil
}











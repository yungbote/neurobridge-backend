package db

import (
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
		&types.JobRun{},
	)
}

func (s *PostgresService) AutoMigrateAll() error {
	s.log.Info("Auto migrating postgres tables...")

	err := AutoMigrateAll(s.db)
	if err != nil {
		s.log.Error("Auto migration failed", "error", err)
		return err
	}
	return nil
}

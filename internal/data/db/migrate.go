package db

import (
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"gorm.io/gorm"
)

func AutoMigrateAll(db *gorm.DB) error {
	return db.AutoMigrate(
		&types.User{},
		&types.UserToken{},

		&types.MaterialSet{},
		&types.MaterialFile{},
		&types.MaterialChunk{},
		&types.MaterialAsset{},

		&types.Course{},
		&types.CourseTag{},
		&types.CourseModule{},
		&types.Lesson{},
		&types.LessonVariant{},
		&types.LessonConcept{},
		&types.LessonCitation{},
		&types.LessonAsset{},
		&types.QuizQuestion{},
		&types.CourseBlueprint{},

		// NEW graph model
		&types.Concept{},
		&types.Activity{},
		&types.ActivityVariant{},
		&types.ActivityConcept{},
		&types.ActivityCitation{},
		&types.Path{},
		&types.PathNode{},
		&types.PathNodeActivity{},
		&types.Asset{},

		// Personalization backbone
		&types.UserEvent{},
		&types.UserEventCursor{},
		&types.UserConceptState{},
		&types.UserStylePreference{},

		// Legacy (keep for now)
		&types.LearningProfile{},
		&types.TopicMastery{},
		&types.TopicStylePreference{},
		&types.LessonProgress{},
		&types.QuizAttempt{},

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

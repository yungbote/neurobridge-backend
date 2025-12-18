package app

import (
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"gorm.io/gorm"
)

type Repos struct {
	User                 repos.UserRepo
	UserToken            repos.UserTokenRepo
	Asset                repos.AssetRepo
	MaterialSet          repos.MaterialSetRepo
	MaterialFile         repos.MaterialFileRepo
	MaterialChunk        repos.MaterialChunkRepo
	MaterialAsset        repos.MaterialAssetRepo
	Course               repos.CourseRepo
	CourseModule         repos.CourseModuleRepo
	CourseConcept        repos.CourseConceptRepo
	CourseTag            repos.CourseTagRepo
	Lesson               repos.LessonRepo
	LessonVariant        repos.LessonVariantRepo
	LessonCitation       repos.LessonCitationRepo
	LessonConcept        repos.LessonConceptRepo
	QuizQuestion         repos.QuizQuestionRepo
	CourseBlueprint      repos.CourseBlueprintRepo
	JobRun               repos.JobRunRepo
	LessonProgress       repos.LessonProgressRepo
	QuizAttempt          repos.QuizAttemptRepo
	TopicMastery         repos.TopicMasteryRepo
	TopicStylePreference repos.TopicStylePreferenceRepo
	UserEvent            repos.UserEventRepo
	UserEventCursor      repos.UserEventCursorRepo
	UserConceptState     repos.UserConceptStateRepo
	UserStylePreference  repos.UserStylePreferenceRepo
	Concept              repos.ConceptRepo
	Activity             repos.ActivityRepo
	ActivityVariant      repos.ActivityVariantRepo
	ActivityConcept      repos.ActivityConceptRepo
	ActivityCitation     repos.ActivityCitationRepo
	Path                 repos.PathRepo
	PathNode             repos.PathNodeRepo
	PathNodeActivity     repos.PathNodeActivityRepo
}

func wireRepos(db *gorm.DB, log *logger.Logger) Repos {
	log.Info("Wiring repos...")
	return Repos{
		User:                 repos.NewUserRepo(db, log),
		UserToken:            repos.NewUserTokenRepo(db, log),
		Asset:                repos.NewAssetRepo(db, log),
		MaterialSet:          repos.NewMaterialSetRepo(db, log),
		MaterialFile:         repos.NewMaterialFileRepo(db, log),
		MaterialChunk:        repos.NewMaterialChunkRepo(db, log),
		MaterialAsset:        repos.NewMaterialAssetRepo(db, log),
		Course:               repos.NewCourseRepo(db, log),
		CourseModule:         repos.NewCourseModuleRepo(db, log),
		CourseConcept:        repos.NewCourseConceptRepo(db, log),
		CourseTag:            repos.NewCourseTagRepo(db, log),
		Lesson:               repos.NewLessonRepo(db, log),
		LessonVariant:        repos.NewLessonVariantRepo(db, log),
		LessonCitation:       repos.NewLessonCitationRepo(db, log),
		LessonConcept:        repos.NewLessonConceptRepo(db, log),
		QuizQuestion:         repos.NewQuizQuestionRepo(db, log),
		CourseBlueprint:      repos.NewCourseBlueprintRepo(db, log),
		JobRun:               repos.NewJobRunRepo(db, log),
		LessonProgress:       repos.NewLessonProgressRepo(db, log),
		QuizAttempt:          repos.NewQuizAttemptRepo(db, log),
		TopicMastery:         repos.NewTopicMasteryRepo(db, log),
		TopicStylePreference: repos.NewTopicStylePreferenceRepo(db, log),
		UserEvent:            repos.NewUserEventRepo(db, log),
		UserEventCursor:      repos.NewUserEventCursorRepo(db, log),
		UserConceptState:     repos.NewUserConceptStateRepo(db, log),
		UserStylePreference:  repos.NewUserStylePreferenceRepo(db, log),
		Concept:              repos.NewConceptRepo(db, log),
		Activity:             repos.NewActivityRepo(db, log),
		ActivityVariant:      repos.NewActivityVariantRepo(db, log),
		ActivityConcept:      repos.NewActivityConceptRepo(db, log),
		ActivityCitation:     repos.NewActivityCitationRepo(db, log),
		Path:                 repos.NewPathRepo(db, log),
		PathNode:             repos.NewPathNodeRepo(db, log),
		PathNodeActivity:     repos.NewPathNodeActivityRepo(db, log),
	}
}

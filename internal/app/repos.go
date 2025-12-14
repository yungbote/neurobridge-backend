package app

import (
	"gorm.io/gorm"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/repos"
)

type Repos struct {
	User										repos.UserRepo
	UserToken								repos.UserTokenRepo
	MaterialSet							repos.MaterialSetRepo
	MaterialFile						repos.MaterialFileRepo
	MaterialChunk						repos.MaterialChunkRepo
	Course									repos.CourseRepo
	CourseModule						repos.CourseModuleRepo
	Lesson									repos.LessonRepo
	QuizQuestion						repos.QuizQuestionRepo
	CourseBlueprint					repos.CourseBlueprintRepo
	CourseGenerationRun			repos.CourseGenerationRunRepo
}

func wireRepos(db *gorm.DB, log *logger.Logger) Repos {
	log.Info("Wiring repos...")
	return Repos{
		User:									repos.NewUserRepo(db, log),
		UserToken:						repos.NewUserTokenRepo(db, log),
		MaterialSet:					repos.NewMaterialSetRepo(db, log),
		MaterialFile:					repos.NewMaterialFileRepo(db, log),
		MaterialChunk:				repos.NewMaterialChunkRepo(db, log),
		Course:								repos.NewCourseRepo(db, log),
		CourseModule:					repos.NewCourseModuleRepo(db, log),
		Lesson:								repos.NewLessonRepo(db, log),
		QuizQuestion:					repos.NewQuizQuestionRepo(db, log),
		CourseBlueprint:			repos.NewCourseBlueprintRepo(db, log),
		CourseGenerationRun:	repos.NewCourseGenerationRunRepo(db, log),
	}
}











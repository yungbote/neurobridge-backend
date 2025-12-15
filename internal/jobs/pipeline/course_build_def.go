package pipelines

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/repos"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type CourseBuildPipeline struct {
	db  *gorm.DB
	log *logger.Logger
	courseRepo       repos.CourseRepo
	materialFileRepo repos.MaterialFileRepo
	moduleRepo       repos.CourseModuleRepo
	lessonRepo       repos.LessonRepo
	quizRepo         repos.QuizQuestionRepo
	blueprintRepo    repos.CourseBlueprintRepo
	chunkRepo        repos.MaterialChunkRepo
	bucket services.BucketService
	ai     services.OpenAIClient
	courseNotify services.CourseNotifier
	extractor		 services.ContentExtractionService
}

func NewCourseBuildPipeline(
	db *gorm.DB,
	baseLog *logger.Logger,
	courseRepo repos.CourseRepo,
	materialFileRepo repos.MaterialFileRepo,
	moduleRepo repos.CourseModuleRepo,
	lessonRepo repos.LessonRepo,
	quizRepo repos.QuizQuestionRepo,
	blueprintRepo repos.CourseBlueprintRepo,
	chunkRepo repos.MaterialChunkRepo,
	bucket services.BucketService,
	ai services.OpenAIClient,
	courseNotify services.CourseNotifier,
	extractor			services.ContentExtractionService,
) *CourseBuildPipeline {
	return &CourseBuildPipeline{
		db:               db,
		log:              baseLog.With("job", "course_build"),
		courseRepo:       courseRepo,
		materialFileRepo: materialFileRepo,
		moduleRepo:       moduleRepo,
		lessonRepo:       lessonRepo,
		quizRepo:         quizRepo,
		blueprintRepo:    blueprintRepo,
		chunkRepo:        chunkRepo,
		bucket:           bucket,
		ai:               ai,
		courseNotify:     courseNotify,
		extractor:				extractor,
	}
}











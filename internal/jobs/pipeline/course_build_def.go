package pipelines

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/repos"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"github.com/yungbote/neurobridge-backend/internal/clients/gcp"
	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	ingestion "github.com/yungbote/neurobridge-backend/internal/ingestion/pipeline"
)

type CourseBuildPipeline struct {
	db							 *gorm.DB
	log							 *logger.Logger
	courseRepo       repos.CourseRepo
	materialFileRepo repos.MaterialFileRepo
	moduleRepo       repos.CourseModuleRepo
	lessonRepo       repos.LessonRepo
	quizRepo         repos.QuizQuestionRepo
	blueprintRepo    repos.CourseBlueprintRepo
	chunkRepo        repos.MaterialChunkRepo
	bucket					 gcp.BucketService
	ai							 openai.Client
	courseNotify		 services.CourseNotifier
	extractor				 ingestion.ContentExtractionService
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
	bucket gcp.BucketService,
	ai openai.Client,
	courseNotify services.CourseNotifier,
	extractor			ingestion.ContentExtractionService,
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











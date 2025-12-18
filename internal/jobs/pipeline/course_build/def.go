package course_build

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/gcp"
	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	"github.com/yungbote/neurobridge-backend/internal/clients/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	ingestion "github.com/yungbote/neurobridge-backend/internal/ingestion/pipeline"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type CourseBuildPipeline struct {
	db               *gorm.DB
	log              *logger.Logger
	courseRepo       repos.CourseRepo
	materialFileRepo repos.MaterialFileRepo
	moduleRepo       repos.CourseModuleRepo
	lessonRepo       repos.LessonRepo
	quizRepo         repos.QuizQuestionRepo
	blueprintRepo    repos.CourseBlueprintRepo
	chunkRepo        repos.MaterialChunkRepo
	bucket           gcp.BucketService
	ai               openai.Client
	courseNotify     services.CourseNotifier
	extractor        ingestion.ContentExtractionService
	vectorStore      pinecone.VectorStore
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
	extractor ingestion.ContentExtractionService,
	vectorStore pinecone.VectorStore,
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
		extractor:        extractor,
		vectorStore:      vectorStore,
	}
}

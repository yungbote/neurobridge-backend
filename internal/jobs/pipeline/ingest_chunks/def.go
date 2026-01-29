package ingest_chunks

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	ingestion "github.com/yungbote/neurobridge-backend/internal/modules/learning/ingestion/pipeline"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	files     repos.MaterialFileRepo
	chunks    repos.MaterialChunkRepo
	extract   ingestion.ContentExtractionService
	saga      services.SagaService
	bootstrap services.LearningBuildBootstrapService
	artifacts repos.LearningArtifactRepo
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	files repos.MaterialFileRepo,
	chunks repos.MaterialChunkRepo,
	extract ingestion.ContentExtractionService,
	saga services.SagaService,
	bootstrap services.LearningBuildBootstrapService,
	artifacts repos.LearningArtifactRepo,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "ingest_chunks"),
		files:     files,
		chunks:    chunks,
		extract:   extract,
		saga:      saga,
		bootstrap: bootstrap,
		artifacts: artifacts,
	}
}

func (p *Pipeline) Type() string { return "ingest_chunks" }

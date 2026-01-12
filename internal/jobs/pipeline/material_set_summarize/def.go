package material_set_summarize

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	files     repos.MaterialFileRepo
	chunks    repos.MaterialChunkRepo
	summaries repos.MaterialSetSummaryRepo
	ai        openai.Client
	vec       pinecone.VectorStore
	saga      services.SagaService
	bootstrap services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	files repos.MaterialFileRepo,
	chunks repos.MaterialChunkRepo,
	summaries repos.MaterialSetSummaryRepo,
	ai openai.Client,
	vec pinecone.VectorStore,
	saga services.SagaService,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "material_set_summarize"),
		files:     files,
		chunks:    chunks,
		summaries: summaries,
		ai:        ai,
		vec:       vec,
		saga:      saga,
		bootstrap: bootstrap,
	}
}

func (p *Pipeline) Type() string { return "material_set_summarize" }

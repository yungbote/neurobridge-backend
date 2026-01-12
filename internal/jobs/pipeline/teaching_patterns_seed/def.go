package teaching_patterns_seed

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
	patterns  repos.TeachingPatternRepo
	profile   repos.UserProfileVectorRepo
	ai        openai.Client
	vec       pinecone.VectorStore
	saga      services.SagaService
	bootstrap services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	patterns repos.TeachingPatternRepo,
	profile repos.UserProfileVectorRepo,
	ai openai.Client,
	vec pinecone.VectorStore,
	saga services.SagaService,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "teaching_patterns_seed"),
		patterns:  patterns,
		profile:   profile,
		ai:        ai,
		vec:       vec,
		saga:      saga,
		bootstrap: bootstrap,
	}
}

func (p *Pipeline) Type() string { return "teaching_patterns_seed" }

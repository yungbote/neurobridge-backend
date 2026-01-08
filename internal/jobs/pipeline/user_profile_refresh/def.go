package user_profile_refresh

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	"github.com/yungbote/neurobridge-backend/internal/clients/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db         *gorm.DB
	log        *logger.Logger
	stylePrefs repos.UserStylePreferenceRepo
	progEvents repos.UserProgressionEventRepo
	profile    repos.UserProfileVectorRepo
	prefs      repos.UserPersonalizationPrefsRepo
	ai         openai.Client
	vec        pinecone.VectorStore
	saga       services.SagaService
	bootstrap  services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	stylePrefs repos.UserStylePreferenceRepo,
	progEvents repos.UserProgressionEventRepo,
	profile repos.UserProfileVectorRepo,
	prefs repos.UserPersonalizationPrefsRepo,
	ai openai.Client,
	vec pinecone.VectorStore,
	saga services.SagaService,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:         db,
		log:        baseLog.With("job", "user_profile_refresh"),
		stylePrefs: stylePrefs,
		progEvents: progEvents,
		profile:    profile,
		prefs:      prefs,
		ai:         ai,
		vec:        vec,
		saga:       saga,
		bootstrap:  bootstrap,
	}
}

func (p *Pipeline) Type() string { return "user_profile_refresh" }

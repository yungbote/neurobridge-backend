package realize_activities

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db               *gorm.DB
	log              *logger.Logger
	path             repos.PathRepo
	nodes            repos.PathNodeRepo
	nodeActivities   repos.PathNodeActivityRepo
	activities       repos.ActivityRepo
	variants         repos.ActivityVariantRepo
	activityConcepts repos.ActivityConceptRepo
	activityCites    repos.ActivityCitationRepo
	concepts         repos.ConceptRepo
	files            repos.MaterialFileRepo
	chunks           repos.MaterialChunkRepo
	profile          repos.UserProfileVectorRepo
	patterns         repos.TeachingPatternRepo
	graph            *neo4jdb.Client
	ai               openai.Client
	vec              pinecone.VectorStore
	bucket           gcp.BucketService
	saga             services.SagaService
	bootstrap        services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	path repos.PathRepo,
	nodes repos.PathNodeRepo,
	nodeActivities repos.PathNodeActivityRepo,
	activities repos.ActivityRepo,
	variants repos.ActivityVariantRepo,
	activityConcepts repos.ActivityConceptRepo,
	activityCites repos.ActivityCitationRepo,
	concepts repos.ConceptRepo,
	files repos.MaterialFileRepo,
	chunks repos.MaterialChunkRepo,
	profile repos.UserProfileVectorRepo,
	patterns repos.TeachingPatternRepo,
	graph *neo4jdb.Client,
	ai openai.Client,
	vec pinecone.VectorStore,
	bucket gcp.BucketService,
	saga services.SagaService,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:               db,
		log:              baseLog.With("job", "realize_activities"),
		path:             path,
		nodes:            nodes,
		nodeActivities:   nodeActivities,
		activities:       activities,
		variants:         variants,
		activityConcepts: activityConcepts,
		activityCites:    activityCites,
		concepts:         concepts,
		files:            files,
		chunks:           chunks,
		profile:          profile,
		patterns:         patterns,
		graph:            graph,
		ai:               ai,
		vec:              vec,
		bucket:           bucket,
		saga:             saga,
		bootstrap:        bootstrap,
	}
}

func (p *Pipeline) Type() string { return "realize_activities" }

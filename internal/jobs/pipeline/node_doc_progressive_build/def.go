package node_doc_progressive_build

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db                *gorm.DB
	log               *logger.Logger
	path              repos.PathRepo
	pathRuns          repos.PathRunRepo
	nodeRuns          repos.NodeRunRepo
	nodes             repos.PathNodeRepo
	docs              repos.LearningNodeDocRepo
	docVariants       repos.LearningNodeDocVariantRepo
	signalSnapshots   repos.UserDocSignalSnapshotRepo
	interventionPlans repos.InterventionPlanRepo
	figures           repos.LearningNodeFigureRepo
	videos            repos.LearningNodeVideoRepo
	genRuns           repos.LearningDocGenerationRunRepo
	blueprints        repos.LearningNodeDocBlueprintRepo
	retrievalPacks    repos.DocRetrievalPackRepo
	docTraces         repos.DocGenerationTraceRepo
	constraintReports repos.DocConstraintReportRepo
	revisions         repos.LearningNodeDocRevisionRepo
	files             repos.MaterialFileRepo
	chunks            repos.MaterialChunkRepo
	userProf          repos.UserProfileVectorRepo
	patterns          repos.TeachingPatternRepo
	concepts          repos.ConceptRepo
	mastery           repos.UserConceptStateRepo
	model             repos.UserConceptModelRepo
	miscon            repos.UserMisconceptionInstanceRepo
	ai                openai.Client
	vec               pinecone.VectorStore
	bucket            gcp.BucketService
	bootstrap         services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	path repos.PathRepo,
	pathRuns repos.PathRunRepo,
	nodeRuns repos.NodeRunRepo,
	nodes repos.PathNodeRepo,
	docs repos.LearningNodeDocRepo,
	docVariants repos.LearningNodeDocVariantRepo,
	signalSnapshots repos.UserDocSignalSnapshotRepo,
	interventionPlans repos.InterventionPlanRepo,
	figures repos.LearningNodeFigureRepo,
	videos repos.LearningNodeVideoRepo,
	genRuns repos.LearningDocGenerationRunRepo,
	blueprints repos.LearningNodeDocBlueprintRepo,
	retrievalPacks repos.DocRetrievalPackRepo,
	docTraces repos.DocGenerationTraceRepo,
	constraintReports repos.DocConstraintReportRepo,
	revisions repos.LearningNodeDocRevisionRepo,
	files repos.MaterialFileRepo,
	chunks repos.MaterialChunkRepo,
	userProf repos.UserProfileVectorRepo,
	patterns repos.TeachingPatternRepo,
	concepts repos.ConceptRepo,
	mastery repos.UserConceptStateRepo,
	model repos.UserConceptModelRepo,
	miscon repos.UserMisconceptionInstanceRepo,
	ai openai.Client,
	vec pinecone.VectorStore,
	bucket gcp.BucketService,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:                db,
		log:               baseLog.With("job", "node_doc_progressive_build"),
		path:              path,
		pathRuns:          pathRuns,
		nodeRuns:          nodeRuns,
		nodes:             nodes,
		docs:              docs,
		docVariants:       docVariants,
		signalSnapshots:   signalSnapshots,
		interventionPlans: interventionPlans,
		figures:           figures,
		videos:            videos,
		genRuns:           genRuns,
		blueprints:        blueprints,
		retrievalPacks:    retrievalPacks,
		docTraces:         docTraces,
		constraintReports: constraintReports,
		revisions:         revisions,
		files:             files,
		chunks:            chunks,
		userProf:          userProf,
		patterns:          patterns,
		concepts:          concepts,
		mastery:           mastery,
		model:             model,
		miscon:            miscon,
		ai:                ai,
		vec:               vec,
		bucket:            bucket,
		bootstrap:         bootstrap,
	}
}

func (p *Pipeline) Type() string { return "node_doc_progressive_build" }

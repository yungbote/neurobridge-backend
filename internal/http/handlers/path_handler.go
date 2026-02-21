package handlers

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	learningmod "github.com/yungbote/neurobridge-backend/internal/modules/learning"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type PathHandler struct {
	log *logger.Logger
	db  *gorm.DB

	path               repos.PathRepo
	pathNodes          repos.PathNodeRepo
	pathNodeActivity   repos.PathNodeActivityRepo
	activities         repos.ActivityRepo
	nodeDocs           repos.LearningNodeDocRepo
	docRevisions       repos.LearningNodeDocRevisionRepo
	docVariants        repos.LearningNodeDocVariantRepo
	docVariantExposure repos.DocVariantExposureRepo
	chunks             repos.MaterialChunkRepo
	materialSets       repos.MaterialSetRepo
	materialFiles      repos.MaterialFileRepo
	materialAssets     repos.MaterialAssetRepo
	userLibraryIndex   repos.UserLibraryIndexRepo

	concepts     repos.ConceptRepo
	edges        repos.ConceptEdgeRepo
	conceptState repos.UserConceptStateRepo
	policyEval   repos.PolicyEvalSnapshotRepo
	prereqGates  repos.PrereqGateDecisionRepo

	assets repos.AssetRepo
	jobs   repos.JobRunRepo
	jobSvc services.JobService
	events services.EventService

	avatar   services.AvatarService
	learning learningmod.Usecases
	bucket   gcp.BucketService
}

type PathHandlerPathRepos struct {
	Path             repos.PathRepo
	PathNodes        repos.PathNodeRepo
	PathNodeActivity repos.PathNodeActivityRepo
}

type PathHandlerContentRepos struct {
	Activities         repos.ActivityRepo
	NodeDocs           repos.LearningNodeDocRepo
	DocRevisions       repos.LearningNodeDocRevisionRepo
	DocVariants        repos.LearningNodeDocVariantRepo
	DocVariantExposure repos.DocVariantExposureRepo
	Chunks             repos.MaterialChunkRepo
	MaterialSets       repos.MaterialSetRepo
	MaterialFiles      repos.MaterialFileRepo
	MaterialAssets     repos.MaterialAssetRepo
	UserLibraryIndex   repos.UserLibraryIndexRepo
	Assets             repos.AssetRepo
}

type PathHandlerLearningRepos struct {
	Concepts     repos.ConceptRepo
	Edges        repos.ConceptEdgeRepo
	ConceptState repos.UserConceptStateRepo
	PolicyEval   repos.PolicyEvalSnapshotRepo
	PrereqGates  repos.PrereqGateDecisionRepo
}

type PathHandlerServices struct {
	Jobs     repos.JobRunRepo
	JobSvc   services.JobService
	Events   services.EventService
	Avatar   services.AvatarService
	Learning learningmod.Usecases
	Bucket   gcp.BucketService
}

type PathHandlerDeps struct {
	Log *logger.Logger
	DB  *gorm.DB

	Path     PathHandlerPathRepos
	Content  PathHandlerContentRepos
	Learning PathHandlerLearningRepos
	Services PathHandlerServices
}

func NewPathHandlerWithDeps(deps PathHandlerDeps) *PathHandler {
	return &PathHandler{
		log:                deps.Log.With("handler", "PathHandler"),
		db:                 deps.DB,
		path:               deps.Path.Path,
		pathNodes:          deps.Path.PathNodes,
		pathNodeActivity:   deps.Path.PathNodeActivity,
		activities:         deps.Content.Activities,
		nodeDocs:           deps.Content.NodeDocs,
		docRevisions:       deps.Content.DocRevisions,
		docVariants:        deps.Content.DocVariants,
		docVariantExposure: deps.Content.DocVariantExposure,
		chunks:             deps.Content.Chunks,
		materialSets:       deps.Content.MaterialSets,
		materialFiles:      deps.Content.MaterialFiles,
		materialAssets:     deps.Content.MaterialAssets,
		userLibraryIndex:   deps.Content.UserLibraryIndex,
		concepts:           deps.Learning.Concepts,
		edges:              deps.Learning.Edges,
		conceptState:       deps.Learning.ConceptState,
		policyEval:         deps.Learning.PolicyEval,
		prereqGates:        deps.Learning.PrereqGates,
		assets:             deps.Content.Assets,
		jobs:               deps.Services.Jobs,
		jobSvc:             deps.Services.JobSvc,
		events:             deps.Services.Events,
		avatar:             deps.Services.Avatar,
		learning:           deps.Services.Learning,
		bucket:             deps.Services.Bucket,
	}
}

// NewPathHandler is kept as a compatibility shim while app wiring migrates to typed deps.
func NewPathHandler(
	log *logger.Logger,
	db *gorm.DB,
	path repos.PathRepo,
	pathNodes repos.PathNodeRepo,
	pathNodeActivity repos.PathNodeActivityRepo,
	activities repos.ActivityRepo,
	nodeDocs repos.LearningNodeDocRepo,
	docRevisions repos.LearningNodeDocRevisionRepo,
	docVariants repos.LearningNodeDocVariantRepo,
	docVariantExposure repos.DocVariantExposureRepo,
	chunks repos.MaterialChunkRepo,
	materialSets repos.MaterialSetRepo,
	materialFiles repos.MaterialFileRepo,
	materialAssets repos.MaterialAssetRepo,
	userLibraryIndex repos.UserLibraryIndexRepo,
	concepts repos.ConceptRepo,
	edges repos.ConceptEdgeRepo,
	conceptState repos.UserConceptStateRepo,
	policyEval repos.PolicyEvalSnapshotRepo,
	prereqGates repos.PrereqGateDecisionRepo,
	assets repos.AssetRepo,
	jobs repos.JobRunRepo,
	jobSvc services.JobService,
	events services.EventService,
	avatar services.AvatarService,
	learning learningmod.Usecases,
	bucket gcp.BucketService,
) *PathHandler {
	return NewPathHandlerWithDeps(PathHandlerDeps{
		Log: log,
		DB:  db,
		Path: PathHandlerPathRepos{
			Path:             path,
			PathNodes:        pathNodes,
			PathNodeActivity: pathNodeActivity,
		},
		Content: PathHandlerContentRepos{
			Activities:         activities,
			NodeDocs:           nodeDocs,
			DocRevisions:       docRevisions,
			DocVariants:        docVariants,
			DocVariantExposure: docVariantExposure,
			Chunks:             chunks,
			MaterialSets:       materialSets,
			MaterialFiles:      materialFiles,
			MaterialAssets:     materialAssets,
			UserLibraryIndex:   userLibraryIndex,
			Assets:             assets,
		},
		Learning: PathHandlerLearningRepos{
			Concepts:     concepts,
			Edges:        edges,
			ConceptState: conceptState,
			PolicyEval:   policyEval,
			PrereqGates:  prereqGates,
		},
		Services: PathHandlerServices{
			Jobs:     jobs,
			JobSvc:   jobSvc,
			Events:   events,
			Avatar:   avatar,
			Learning: learning,
			Bucket:   bucket,
		},
	})
}

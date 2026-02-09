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
	return &PathHandler{
		log:                log.With("handler", "PathHandler"),
		db:                 db,
		path:               path,
		pathNodes:          pathNodes,
		pathNodeActivity:   pathNodeActivity,
		activities:         activities,
		nodeDocs:           nodeDocs,
		docRevisions:       docRevisions,
		docVariants:        docVariants,
		docVariantExposure: docVariantExposure,
		chunks:             chunks,
		materialSets:       materialSets,
		materialFiles:      materialFiles,
		materialAssets:     materialAssets,
		userLibraryIndex:   userLibraryIndex,
		concepts:           concepts,
		edges:              edges,
		conceptState:       conceptState,
		policyEval:         policyEval,
		prereqGates:        prereqGates,
		assets:             assets,
		jobs:               jobs,
		jobSvc:             jobSvc,
		events:             events,
		avatar:             avatar,
		learning:           learning,
		bucket:             bucket,
	}
}

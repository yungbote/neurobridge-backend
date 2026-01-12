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

	path             repos.PathRepo
	pathNodes        repos.PathNodeRepo
	pathNodeActivity repos.PathNodeActivityRepo
	activities       repos.ActivityRepo
	nodeDocs         repos.LearningNodeDocRepo
	docRevisions     repos.LearningNodeDocRevisionRepo
	chunks           repos.MaterialChunkRepo
	materialSets     repos.MaterialSetRepo
	materialFiles    repos.MaterialFileRepo
	materialAssets   repos.MaterialAssetRepo
	userLibraryIndex repos.UserLibraryIndexRepo

	concepts repos.ConceptRepo
	edges    repos.ConceptEdgeRepo

	assets repos.AssetRepo
	jobs   repos.JobRunRepo
	jobSvc services.JobService

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
	chunks repos.MaterialChunkRepo,
	materialSets repos.MaterialSetRepo,
	materialFiles repos.MaterialFileRepo,
	materialAssets repos.MaterialAssetRepo,
	userLibraryIndex repos.UserLibraryIndexRepo,
	concepts repos.ConceptRepo,
	edges repos.ConceptEdgeRepo,
	assets repos.AssetRepo,
	jobs repos.JobRunRepo,
	jobSvc services.JobService,
	avatar services.AvatarService,
	learning learningmod.Usecases,
	bucket gcp.BucketService,
) *PathHandler {
	return &PathHandler{
		log:              log.With("handler", "PathHandler"),
		db:               db,
		path:             path,
		pathNodes:        pathNodes,
		pathNodeActivity: pathNodeActivity,
		activities:       activities,
		nodeDocs:         nodeDocs,
		docRevisions:     docRevisions,
		chunks:           chunks,
		materialSets:     materialSets,
		materialFiles:    materialFiles,
		materialAssets:   materialAssets,
		userLibraryIndex: userLibraryIndex,
		concepts:         concepts,
		edges:            edges,
		assets:           assets,
		jobs:             jobs,
		jobSvc:           jobSvc,
		avatar:           avatar,
		learning:         learning,
		bucket:           bucket,
	}
}

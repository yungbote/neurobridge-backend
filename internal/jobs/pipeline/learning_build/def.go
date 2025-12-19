package learning_build

import (
	"os"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	"github.com/yungbote/neurobridge-backend/internal/clients/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	ingestion "github.com/yungbote/neurobridge-backend/internal/ingestion/pipeline"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type InlineDeps struct {
	Extract ingestion.ContentExtractionService
	AI      openai.Client
	Vec     pinecone.VectorStore

	Files     repos.MaterialFileRepo
	Chunks    repos.MaterialChunkRepo
	Summaries repos.MaterialSetSummaryRepo

	Concepts repos.ConceptRepo
	Evidence repos.ConceptEvidenceRepo
	Edges    repos.ConceptEdgeRepo

	Clusters repos.ConceptClusterRepo
	Members  repos.ConceptClusterMemberRepo

	ChainSignatures repos.ChainSignatureRepo

	StylePrefs       repos.UserStylePreferenceRepo
	ConceptState     repos.UserConceptStateRepo
	ProgEvents       repos.UserProgressionEventRepo
	UserProfile      repos.UserProfileVectorRepo
	TeachingPatterns repos.TeachingPatternRepo

	Path               repos.PathRepo
	PathNodes          repos.PathNodeRepo
	PathNodeActivities repos.PathNodeActivityRepo

	Activities        repos.ActivityRepo
	Variants          repos.ActivityVariantRepo
	ActivityConcepts  repos.ActivityConceptRepo
	ActivityCitations repos.ActivityCitationRepo

	UserEvents            repos.UserEventRepo
	UserEventCursors      repos.UserEventCursorRepo
	UserProgressionEvents repos.UserProgressionEventRepo
	VariantStats          repos.ActivityVariantStatRepo

	ChainPriors    repos.ChainPriorRepo
	CohortPriors   repos.CohortPriorRepo
	CompletedUnits repos.UserCompletedUnitRepo
}

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	jobs      services.JobService
	path      repos.PathRepo
	saga      services.SagaService
	bootstrap services.LearningBuildBootstrapService

	inline *InlineDeps

	minPoll time.Duration
	maxPoll time.Duration
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	jobs services.JobService,
	path repos.PathRepo,
	saga services.SagaService,
	bootstrap services.LearningBuildBootstrapService,
	inline *InlineDeps,
) *Pipeline {
	minPoll := 2 * time.Second
	maxPoll := 10 * time.Second
	if v := strings.TrimSpace(os.Getenv("LEARNING_BUILD_MIN_POLL_MS")); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			minPoll = time.Duration(ms) * time.Millisecond
		}
	}
	if v := strings.TrimSpace(os.Getenv("LEARNING_BUILD_MAX_POLL_MS")); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			maxPoll = time.Duration(ms) * time.Millisecond
		}
	}
	if maxPoll < minPoll {
		maxPoll = minPoll
	}

	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "learning_build"),
		jobs:      jobs,
		path:      path,
		saga:      saga,
		bootstrap: bootstrap,
		inline:    inline,
		minPoll:   minPoll,
		maxPoll:   maxPoll,
	}
}

func (p *Pipeline) Type() string { return "learning_build" }

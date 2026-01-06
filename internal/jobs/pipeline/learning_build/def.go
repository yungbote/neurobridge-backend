package learning_build

import (
	"os"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/gcp"
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
	Bucket  gcp.BucketService
	Avatar  services.AvatarService

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
	NodeDocs           repos.LearningNodeDocRepo
	NodeFigures        repos.LearningNodeFigureRepo
	NodeVideos         repos.LearningNodeVideoRepo
	DocGenRuns         repos.LearningDocGenerationRunRepo
	Assets             repos.AssetRepo

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
	threads   repos.ChatThreadRepo
	messages  repos.ChatMessageRepo
	chatNotif services.ChatNotifier
	saga      services.SagaService
	bootstrap services.LearningBuildBootstrapService

	inline *InlineDeps

	minPoll time.Duration
	maxPoll time.Duration

	childMaxWait      time.Duration
	childStaleRunning time.Duration
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	jobs services.JobService,
	path repos.PathRepo,
	threads repos.ChatThreadRepo,
	messages repos.ChatMessageRepo,
	chatNotif services.ChatNotifier,
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

	childMaxWait := 20 * time.Minute
	if v := strings.TrimSpace(os.Getenv("LEARNING_BUILD_CHILD_MAX_MINUTES")); v != "" {
		if mins, err := strconv.Atoi(v); err == nil && mins > 0 {
			childMaxWait = time.Duration(mins) * time.Minute
		}
	}

	childStaleRunning := 10 * time.Minute
	if v := strings.TrimSpace(os.Getenv("LEARNING_BUILD_CHILD_STALE_RUNNING_MINUTES")); v != "" {
		if mins, err := strconv.Atoi(v); err == nil && mins > 0 {
			childStaleRunning = time.Duration(mins) * time.Minute
		}
	}

	return &Pipeline{
		db:                db,
		log:               baseLog.With("job", "learning_build"),
		jobs:              jobs,
		path:              path,
		threads:           threads,
		messages:          messages,
		chatNotif:         chatNotif,
		saga:              saga,
		bootstrap:         bootstrap,
		inline:            inline,
		minPoll:           minPoll,
		maxPoll:           maxPoll,
		childMaxWait:      childMaxWait,
		childStaleRunning: childStaleRunning,
	}
}

func (p *Pipeline) Type() string { return "learning_build" }

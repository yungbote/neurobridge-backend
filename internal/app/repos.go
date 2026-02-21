package app

import (
	agg "github.com/yungbote/neurobridge-backend/internal/data/aggregates"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	domainagg "github.com/yungbote/neurobridge-backend/internal/domain/aggregates"
	"github.com/yungbote/neurobridge-backend/internal/observability"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"gorm.io/gorm"
)

type AuthRepos struct {
	User         repos.UserRepo
	UserToken    repos.UserTokenRepo
	UserIdentity repos.UserIdentityRepo
	OAuthNonce   repos.OAuthNonceRepo
}

type UserRepos struct {
	UserProfileVector        repos.UserProfileVectorRepo
	UserPersonalizationPrefs repos.UserPersonalizationPrefsRepo
	UserSessionState         repos.UserSessionStateRepo
	UserGazeEvent            repos.UserGazeEventRepo
	UserGazeBlockStat        repos.UserGazeBlockStatRepo
}

type EventRepos struct {
	UserEvent            repos.UserEventRepo
	UserEventCursor      repos.UserEventCursorRepo
	UserProgressionEvent repos.UserProgressionEventRepo
}

type LearningRepos struct {
	// Aggregate contract slots.
	UserConcept domainagg.UserConceptAggregate

	TopicMastery              repos.TopicMasteryRepo
	TopicStylePreference      repos.TopicStylePreferenceRepo
	UserConceptState          repos.UserConceptStateRepo
	UserConceptModel          repos.UserConceptModelRepo
	UserConceptEdgeStat       repos.UserConceptEdgeStatRepo
	UserConceptEvidence       repos.UserConceptEvidenceRepo
	UserConceptCalibration    repos.UserConceptCalibrationRepo
	ItemCalibration           repos.ItemCalibrationRepo
	UserModelAlert            repos.UserModelAlertRepo
	UserMisconception         repos.UserMisconceptionInstanceRepo
	UserMisconceptionInstance repos.UserMisconceptionInstanceRepo
	MisconceptionCausalEdge   repos.MisconceptionCausalEdgeRepo
	MisconceptionResolution   repos.MisconceptionResolutionStateRepo
	UserStylePreference       repos.UserStylePreferenceRepo
	UserTestletState          repos.UserTestletStateRepo
	UserSkillState            repos.UserSkillStateRepo
	UserBeliefSnapshot        repos.UserBeliefSnapshotRepo
	InterventionPlan          repos.InterventionPlanRepo
	ConceptReadinessSnapshot  repos.ConceptReadinessSnapshotRepo
	PrereqGateDecision        repos.PrereqGateDecisionRepo
}

type MaterialRepos struct {
	Asset                 repos.AssetRepo
	MaterialSet           repos.MaterialSetRepo
	MaterialSetFile       repos.MaterialSetFileRepo
	MaterialFile          repos.MaterialFileRepo
	MaterialFileSignature repos.MaterialFileSignatureRepo
	MaterialFileSection   repos.MaterialFileSectionRepo
	MaterialChunk         repos.MaterialChunkRepo
	MaterialAsset         repos.MaterialAssetRepo
	MaterialSetSummary    repos.MaterialSetSummaryRepo
	DrillInstance         repos.LearningDrillInstanceRepo
	LearningArtifact      repos.LearningArtifactRepo
}

type ConceptRepos struct {
	Concept                 repos.ConceptRepo
	ConceptRepresentation   repos.ConceptRepresentationRepo
	ConceptMappingOverride  repos.ConceptMappingOverrideRepo
	ConceptCluster          repos.ConceptClusterRepo
	ConceptClusterMember    repos.ConceptClusterMemberRepo
	ConceptEdge             repos.ConceptEdgeRepo
	ConceptEvidence         repos.ConceptEvidenceRepo
	ChainSignature          repos.ChainSignatureRepo
	ChainPrior              repos.ChainPriorRepo
	CohortPrior             repos.CohortPriorRepo
	GraphVersion            repos.GraphVersionRepo
	StructuralDecisionTrace repos.StructuralDecisionTraceRepo
	StructuralDriftMetric   repos.StructuralDriftMetricRepo
	RollbackEvent           repos.RollbackEventRepo
}

type ActivityRepos struct {
	Activity            repos.ActivityRepo
	ActivityVariant     repos.ActivityVariantRepo
	ActivityVariantStat repos.ActivityVariantStatRepo
	ActivityConcept     repos.ActivityConceptRepo
	ActivityCitation    repos.ActivityCitationRepo
	TeachingPattern     repos.TeachingPatternRepo
	UserCompletedUnit   repos.UserCompletedUnitRepo
}

type PathRepos struct {
	// Aggregate contract slots.
	Runtime domainagg.RuntimeAggregate

	Path               repos.PathRepo
	PathNode           repos.PathNodeRepo
	PathNodeActivity   repos.PathNodeActivityRepo
	PathStructuralUnit repos.PathStructuralUnitRepo
	PathRun            repos.PathRunRepo
	NodeRun            repos.NodeRunRepo
	ActivityRun        repos.ActivityRunRepo
	PathRunTransition  repos.PathRunTransitionRepo
}

type RuntimeRepos struct {
	DecisionTrace      repos.DecisionTraceRepo
	ModelSnapshot      repos.ModelSnapshotRepo
	PolicyEvalSnapshot repos.PolicyEvalSnapshotRepo
}

type LibraryRepos struct {
	// Aggregate contract slots.
	Taxonomy domainagg.TaxonomyAggregate

	UserLibraryIndex        repos.UserLibraryIndexRepo
	LibraryTaxonomyNode     repos.LibraryTaxonomyNodeRepo
	LibraryTaxonomyEdge     repos.LibraryTaxonomyEdgeRepo
	LibraryTaxonomyMember   repos.LibraryTaxonomyMembershipRepo
	LibraryTaxonomyState    repos.LibraryTaxonomyStateRepo
	LibraryTaxonomySnapshot repos.LibraryTaxonomySnapshotRepo
	LibraryPathEmbedding    repos.LibraryPathEmbeddingRepo
}

type DocGenRepos struct {
	// Aggregate contract slots.
	NodeDoc domainagg.NodeDocAggregate

	LearningNodeDoc          repos.LearningNodeDocRepo
	LearningNodeDocRevision  repos.LearningNodeDocRevisionRepo
	LearningNodeFigure       repos.LearningNodeFigureRepo
	LearningNodeVideo        repos.LearningNodeVideoRepo
	DocGenerationRun         repos.LearningDocGenerationRunRepo
	LearningNodeDocBlueprint repos.LearningNodeDocBlueprintRepo
	LearningNodeDocVariant   repos.LearningNodeDocVariantRepo
	UserDocSignalSnapshot    repos.UserDocSignalSnapshotRepo
	DocRetrievalPack         repos.DocRetrievalPackRepo
	DocGenerationTrace       repos.DocGenerationTraceRepo
	DocConstraintReport      repos.DocConstraintReportRepo
	DocProbe                 repos.DocProbeRepo
	DocProbeOutcome          repos.DocProbeOutcomeRepo
	DocVariantExposure       repos.DocVariantExposureRepo
	DocVariantOutcome        repos.DocVariantOutcomeRepo
}

type JobRepos struct {
	// Aggregate contract slots.
	Saga domainagg.SagaAggregate

	JobRun     repos.JobRunRepo
	SagaRun    repos.SagaRunRepo
	SagaAction repos.SagaActionRepo
}

type ChatRepos struct {
	// Aggregate contract slots.
	Thread domainagg.ThreadAggregate

	ChatThread      repos.ChatThreadRepo
	ChatMessage     repos.ChatMessageRepo
	ChatThreadState repos.ChatThreadStateRepo
	ChatSummaryNode repos.ChatSummaryNodeRepo
	ChatMemoryItem  repos.ChatMemoryItemRepo
	ChatEntity      repos.ChatEntityRepo
	ChatEdge        repos.ChatEdgeRepo
	ChatClaim       repos.ChatClaimRepo
	ChatDoc         repos.ChatDocRepo
	ChatTurn        repos.ChatTurnRepo
}

type Repos struct {
	Auth       AuthRepos
	Users      UserRepos
	Events     EventRepos
	Learning   LearningRepos
	Materials  MaterialRepos
	Concepts   ConceptRepos
	Activities ActivityRepos
	Paths      PathRepos
	Runtime    RuntimeRepos
	Library    LibraryRepos
	DocGen     DocGenRepos
	Jobs       JobRepos
	Chat       ChatRepos
}

func wireAggregateBaseDeps(db *gorm.DB, log *logger.Logger) agg.BaseDeps {
	return agg.BaseDeps{
		DB:       db,
		Log:      log,
		Runner:   agg.NewGormTxRunner(db),
		Hooks:    agg.NewObservabilityHooks(observability.Current()),
		CASGuard: agg.NewCASGuard(db),
	}
}

func wireAuthRepos(db *gorm.DB, log *logger.Logger) AuthRepos {
	return AuthRepos{
		User:         repos.NewUserRepo(db, log),
		UserToken:    repos.NewUserTokenRepo(db, log),
		UserIdentity: repos.NewUserIdentityRepo(db, log),
		OAuthNonce:   repos.NewOAuthNonceRepo(db, log),
	}
}

func wireUserRepos(db *gorm.DB, log *logger.Logger) UserRepos {
	return UserRepos{
		UserProfileVector:        repos.NewUserProfileVectorRepo(db, log),
		UserPersonalizationPrefs: repos.NewUserPersonalizationPrefsRepo(db, log),
		UserSessionState:         repos.NewUserSessionStateRepo(db, log),
		UserGazeEvent:            repos.NewUserGazeEventRepo(db, log),
		UserGazeBlockStat:        repos.NewUserGazeBlockStatRepo(db, log),
	}
}

func wireEventRepos(db *gorm.DB, log *logger.Logger) EventRepos {
	return EventRepos{
		UserEvent:            repos.NewUserEventRepo(db, log),
		UserEventCursor:      repos.NewUserEventCursorRepo(db, log),
		UserProgressionEvent: repos.NewUserProgressionEventRepo(db, log),
	}
}

func wireLearningRepos(db *gorm.DB, log *logger.Logger) LearningRepos {
	base := wireAggregateBaseDeps(db, log)
	userMisconceptionRepo := repos.NewUserMisconceptionInstanceRepo(db, log)
	userConceptStateRepo := repos.NewUserConceptStateRepo(db, log)
	userConceptEvidenceRepo := repos.NewUserConceptEvidenceRepo(db, log)
	userConceptCalibrationRepo := repos.NewUserConceptCalibrationRepo(db, log)
	userModelAlertRepo := repos.NewUserModelAlertRepo(db, log)
	conceptReadinessRepo := repos.NewConceptReadinessSnapshotRepo(db, log)
	prereqGateRepo := repos.NewPrereqGateDecisionRepo(db, log)

	return LearningRepos{
		UserConcept: agg.NewUserConceptAggregate(agg.UserConceptAggregateDeps{
			Base:          base,
			States:        userConceptStateRepo,
			Evidence:      userConceptEvidenceRepo,
			Calibration:   userConceptCalibrationRepo,
			Misconception: userMisconceptionRepo,
			Readiness:     conceptReadinessRepo,
			Gates:         prereqGateRepo,
			Alerts:        userModelAlertRepo,
		}),
		TopicMastery:              repos.NewTopicMasteryRepo(db, log),
		TopicStylePreference:      repos.NewTopicStylePreferenceRepo(db, log),
		UserConceptState:          userConceptStateRepo,
		UserConceptModel:          repos.NewUserConceptModelRepo(db, log),
		UserConceptEdgeStat:       repos.NewUserConceptEdgeStatRepo(db, log),
		UserConceptEvidence:       userConceptEvidenceRepo,
		UserConceptCalibration:    userConceptCalibrationRepo,
		ItemCalibration:           repos.NewItemCalibrationRepo(db, log),
		UserModelAlert:            userModelAlertRepo,
		UserMisconception:         userMisconceptionRepo,
		UserMisconceptionInstance: userMisconceptionRepo,
		MisconceptionCausalEdge:   repos.NewMisconceptionCausalEdgeRepo(db, log),
		MisconceptionResolution:   repos.NewMisconceptionResolutionStateRepo(db, log),
		UserStylePreference:       repos.NewUserStylePreferenceRepo(db, log),
		UserTestletState:          repos.NewUserTestletStateRepo(db, log),
		UserSkillState:            repos.NewUserSkillStateRepo(db, log),
		UserBeliefSnapshot:        repos.NewUserBeliefSnapshotRepo(db, log),
		InterventionPlan:          repos.NewInterventionPlanRepo(db, log),
		ConceptReadinessSnapshot:  conceptReadinessRepo,
		PrereqGateDecision:        prereqGateRepo,
	}
}

func wireMaterialRepos(db *gorm.DB, log *logger.Logger) MaterialRepos {
	return MaterialRepos{
		Asset:                 repos.NewAssetRepo(db, log),
		MaterialSet:           repos.NewMaterialSetRepo(db, log),
		MaterialSetFile:       repos.NewMaterialSetFileRepo(db, log),
		MaterialFile:          repos.NewMaterialFileRepo(db, log),
		MaterialFileSignature: repos.NewMaterialFileSignatureRepo(db, log),
		MaterialFileSection:   repos.NewMaterialFileSectionRepo(db, log),
		MaterialChunk:         repos.NewMaterialChunkRepo(db, log),
		MaterialAsset:         repos.NewMaterialAssetRepo(db, log),
		MaterialSetSummary:    repos.NewMaterialSetSummaryRepo(db, log),
		DrillInstance:         repos.NewLearningDrillInstanceRepo(db, log),
		LearningArtifact:      repos.NewLearningArtifactRepo(db, log),
	}
}

func wireConceptRepos(db *gorm.DB, log *logger.Logger) ConceptRepos {
	return ConceptRepos{
		Concept:                 repos.NewConceptRepo(db, log),
		ConceptRepresentation:   repos.NewConceptRepresentationRepo(db, log),
		ConceptMappingOverride:  repos.NewConceptMappingOverrideRepo(db, log),
		ConceptCluster:          repos.NewConceptClusterRepo(db, log),
		ConceptClusterMember:    repos.NewConceptClusterMemberRepo(db, log),
		ConceptEdge:             repos.NewConceptEdgeRepo(db, log),
		ConceptEvidence:         repos.NewConceptEvidenceRepo(db, log),
		ChainSignature:          repos.NewChainSignatureRepo(db, log),
		ChainPrior:              repos.NewChainPriorRepo(db, log),
		CohortPrior:             repos.NewCohortPriorRepo(db, log),
		GraphVersion:            repos.NewGraphVersionRepo(db, log),
		StructuralDecisionTrace: repos.NewStructuralDecisionTraceRepo(db, log),
		StructuralDriftMetric:   repos.NewStructuralDriftMetricRepo(db, log),
		RollbackEvent:           repos.NewRollbackEventRepo(db, log),
	}
}

func wireActivityRepos(db *gorm.DB, log *logger.Logger) ActivityRepos {
	return ActivityRepos{
		Activity:            repos.NewActivityRepo(db, log),
		ActivityVariant:     repos.NewActivityVariantRepo(db, log),
		ActivityVariantStat: repos.NewActivityVariantStatRepo(db, log),
		ActivityConcept:     repos.NewActivityConceptRepo(db, log),
		ActivityCitation:    repos.NewActivityCitationRepo(db, log),
		TeachingPattern:     repos.NewTeachingPatternRepo(db, log),
		UserCompletedUnit:   repos.NewUserCompletedUnitRepo(db, log),
	}
}

func wirePathRepos(db *gorm.DB, log *logger.Logger) PathRepos {
	base := wireAggregateBaseDeps(db, log)
	pathRunRepo := repos.NewPathRunRepo(db, log)
	nodeRunRepo := repos.NewNodeRunRepo(db, log)
	activityRunRepo := repos.NewActivityRunRepo(db, log)
	pathRunTransitionRepo := repos.NewPathRunTransitionRepo(db, log)

	return PathRepos{
		Runtime: agg.NewRuntimeAggregate(agg.RuntimeAggregateDeps{
			Base:         base,
			PathRuns:     pathRunRepo,
			NodeRuns:     nodeRunRepo,
			ActivityRuns: activityRunRepo,
			Transitions:  pathRunTransitionRepo,
		}),
		Path:               repos.NewPathRepo(db, log),
		PathNode:           repos.NewPathNodeRepo(db, log),
		PathNodeActivity:   repos.NewPathNodeActivityRepo(db, log),
		PathStructuralUnit: repos.NewPathStructuralUnitRepo(db, log),
		PathRun:            pathRunRepo,
		NodeRun:            nodeRunRepo,
		ActivityRun:        activityRunRepo,
		PathRunTransition:  pathRunTransitionRepo,
	}
}

func wireRuntimeRepos(db *gorm.DB, log *logger.Logger) RuntimeRepos {
	return RuntimeRepos{
		DecisionTrace:      repos.NewDecisionTraceRepo(db, log),
		ModelSnapshot:      repos.NewModelSnapshotRepo(db, log),
		PolicyEvalSnapshot: repos.NewPolicyEvalSnapshotRepo(db, log),
	}
}

func wireLibraryRepos(db *gorm.DB, log *logger.Logger) LibraryRepos {
	base := wireAggregateBaseDeps(db, log)
	nodeRepo := repos.NewLibraryTaxonomyNodeRepo(db, log)
	edgeRepo := repos.NewLibraryTaxonomyEdgeRepo(db, log)
	memberRepo := repos.NewLibraryTaxonomyMembershipRepo(db, log)
	stateRepo := repos.NewLibraryTaxonomyStateRepo(db, log)
	snapshotRepo := repos.NewLibraryTaxonomySnapshotRepo(db, log)

	return LibraryRepos{
		Taxonomy: agg.NewTaxonomyAggregate(agg.TaxonomyAggregateDeps{
			Base:        base,
			Nodes:       nodeRepo,
			Edges:       edgeRepo,
			Memberships: memberRepo,
			State:       stateRepo,
			Snapshots:   snapshotRepo,
		}),
		UserLibraryIndex:        repos.NewUserLibraryIndexRepo(db, log),
		LibraryTaxonomyNode:     nodeRepo,
		LibraryTaxonomyEdge:     edgeRepo,
		LibraryTaxonomyMember:   memberRepo,
		LibraryTaxonomyState:    stateRepo,
		LibraryTaxonomySnapshot: snapshotRepo,
		LibraryPathEmbedding:    repos.NewLibraryPathEmbeddingRepo(db, log),
	}
}

func wireDocGenRepos(db *gorm.DB, log *logger.Logger) DocGenRepos {
	base := wireAggregateBaseDeps(db, log)
	nodeDocRepo := repos.NewLearningNodeDocRepo(db, log)
	nodeDocRevisionRepo := repos.NewLearningNodeDocRevisionRepo(db, log)
	nodeDocVariantRepo := repos.NewLearningNodeDocVariantRepo(db, log)
	docVariantExposureRepo := repos.NewDocVariantExposureRepo(db, log)
	docVariantOutcomeRepo := repos.NewDocVariantOutcomeRepo(db, log)
	docGenerationRunRepo := repos.NewLearningDocGenerationRunRepo(db, log)
	docGenerationTraceRepo := repos.NewDocGenerationTraceRepo(db, log)
	docConstraintReportRepo := repos.NewDocConstraintReportRepo(db, log)

	return DocGenRepos{
		NodeDoc: agg.NewNodeDocAggregate(agg.NodeDocAggregateDeps{
			Base:            base,
			Docs:            nodeDocRepo,
			Revisions:       nodeDocRevisionRepo,
			Variants:        nodeDocVariantRepo,
			VariantExposure: docVariantExposureRepo,
			VariantOutcome:  docVariantOutcomeRepo,
			GenRuns:         docGenerationRunRepo,
			GenTrace:        docGenerationTraceRepo,
			Constraints:     docConstraintReportRepo,
		}),
		LearningNodeDoc:          nodeDocRepo,
		LearningNodeDocRevision:  nodeDocRevisionRepo,
		LearningNodeFigure:       repos.NewLearningNodeFigureRepo(db, log),
		LearningNodeVideo:        repos.NewLearningNodeVideoRepo(db, log),
		DocGenerationRun:         docGenerationRunRepo,
		LearningNodeDocBlueprint: repos.NewLearningNodeDocBlueprintRepo(db, log),
		LearningNodeDocVariant:   nodeDocVariantRepo,
		UserDocSignalSnapshot:    repos.NewUserDocSignalSnapshotRepo(db, log),
		DocRetrievalPack:         repos.NewDocRetrievalPackRepo(db, log),
		DocGenerationTrace:       docGenerationTraceRepo,
		DocConstraintReport:      docConstraintReportRepo,
		DocProbe:                 repos.NewDocProbeRepo(db, log),
		DocProbeOutcome:          repos.NewDocProbeOutcomeRepo(db, log),
		DocVariantExposure:       docVariantExposureRepo,
		DocVariantOutcome:        docVariantOutcomeRepo,
	}
}

func wireJobRepos(db *gorm.DB, log *logger.Logger) JobRepos {
	base := wireAggregateBaseDeps(db, log)
	sagaRunRepo := repos.NewSagaRunRepo(db, log)
	sagaActionRepo := repos.NewSagaActionRepo(db, log)

	return JobRepos{
		Saga: agg.NewSagaAggregate(agg.SagaAggregateDeps{
			Base:    base,
			Runs:    sagaRunRepo,
			Actions: sagaActionRepo,
		}),
		JobRun:     repos.NewJobRunRepo(db, log),
		SagaRun:    sagaRunRepo,
		SagaAction: sagaActionRepo,
	}
}

func wireChatRepos(db *gorm.DB, log *logger.Logger) ChatRepos {
	base := wireAggregateBaseDeps(db, log)
	threadRepo := repos.NewChatThreadRepo(db, log)
	messageRepo := repos.NewChatMessageRepo(db, log)
	threadStateRepo := repos.NewChatThreadStateRepo(db, log)
	summaryRepo := repos.NewChatSummaryNodeRepo(db, log)
	memoryRepo := repos.NewChatMemoryItemRepo(db, log)
	entityRepo := repos.NewChatEntityRepo(db, log)
	edgeRepo := repos.NewChatEdgeRepo(db, log)
	claimRepo := repos.NewChatClaimRepo(db, log)
	docRepo := repos.NewChatDocRepo(db, log)
	turnRepo := repos.NewChatTurnRepo(db, log)

	return ChatRepos{
		Thread: agg.NewThreadAggregate(agg.ThreadAggregateDeps{
			Base:        base,
			Threads:     threadRepo,
			Messages:    messageRepo,
			Turns:       turnRepo,
			ThreadState: threadStateRepo,
			Summary:     summaryRepo,
			Memory:      memoryRepo,
			Entities:    entityRepo,
			Edges:       edgeRepo,
			Claims:      claimRepo,
			Docs:        docRepo,
		}),
		ChatThread:      threadRepo,
		ChatMessage:     messageRepo,
		ChatThreadState: threadStateRepo,
		ChatSummaryNode: summaryRepo,
		ChatMemoryItem:  memoryRepo,
		ChatEntity:      entityRepo,
		ChatEdge:        edgeRepo,
		ChatClaim:       claimRepo,
		ChatDoc:         docRepo,
		ChatTurn:        turnRepo,
	}
}

func wireRepos(db *gorm.DB, log *logger.Logger) Repos {
	log.Info("Wiring repos...")
	return Repos{
		Auth:       wireAuthRepos(db, log),
		Users:      wireUserRepos(db, log),
		Events:     wireEventRepos(db, log),
		Learning:   wireLearningRepos(db, log),
		Materials:  wireMaterialRepos(db, log),
		Concepts:   wireConceptRepos(db, log),
		Activities: wireActivityRepos(db, log),
		Paths:      wirePathRepos(db, log),
		Runtime:    wireRuntimeRepos(db, log),
		Library:    wireLibraryRepos(db, log),
		DocGen:     wireDocGenRepos(db, log),
		Jobs:       wireJobRepos(db, log),
		Chat:       wireChatRepos(db, log),
	}
}

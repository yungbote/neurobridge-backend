package app

import (
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"gorm.io/gorm"
)

type Repos struct {
	User                      repos.UserRepo
	UserProfileVector         repos.UserProfileVectorRepo
	UserPersonalizationPrefs  repos.UserPersonalizationPrefsRepo
	UserSessionState          repos.UserSessionStateRepo
	UserToken                 repos.UserTokenRepo
	UserIdentity              repos.UserIdentityRepo
	OAuthNonce                repos.OAuthNonceRepo
	Asset                     repos.AssetRepo
	MaterialSet               repos.MaterialSetRepo
	MaterialSetFile           repos.MaterialSetFileRepo
	MaterialFile              repos.MaterialFileRepo
	MaterialFileSignature     repos.MaterialFileSignatureRepo
	MaterialFileSection       repos.MaterialFileSectionRepo
	MaterialChunk             repos.MaterialChunkRepo
	MaterialAsset             repos.MaterialAssetRepo
	MaterialSetSummary        repos.MaterialSetSummaryRepo
	JobRun                    repos.JobRunRepo
	SagaRun                   repos.SagaRunRepo
	SagaAction                repos.SagaActionRepo
	TopicMastery              repos.TopicMasteryRepo
	TopicStylePreference      repos.TopicStylePreferenceRepo
	UserEvent                 repos.UserEventRepo
	UserEventCursor           repos.UserEventCursorRepo
	UserProgressionEvent      repos.UserProgressionEventRepo
	UserGazeEvent             repos.UserGazeEventRepo
	UserGazeBlockStat         repos.UserGazeBlockStatRepo
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
	Concept                   repos.ConceptRepo
	ConceptRepresentation     repos.ConceptRepresentationRepo
	ConceptMappingOverride    repos.ConceptMappingOverrideRepo
	ConceptCluster            repos.ConceptClusterRepo
	ConceptClusterMember      repos.ConceptClusterMemberRepo
	ConceptEdge               repos.ConceptEdgeRepo
	ConceptEvidence           repos.ConceptEvidenceRepo
	ChainSignature            repos.ChainSignatureRepo
	ChainPrior                repos.ChainPriorRepo
	CohortPrior               repos.CohortPriorRepo
	Activity                  repos.ActivityRepo
	ActivityVariant           repos.ActivityVariantRepo
	ActivityVariantStat       repos.ActivityVariantStatRepo
	ActivityConcept           repos.ActivityConceptRepo
	ActivityCitation          repos.ActivityCitationRepo
	ModelSnapshot             repos.ModelSnapshotRepo
	PolicyEvalSnapshot        repos.PolicyEvalSnapshotRepo
	Path                      repos.PathRepo
	PathNode                  repos.PathNodeRepo
	PathNodeActivity          repos.PathNodeActivityRepo
	PathStructuralUnit        repos.PathStructuralUnitRepo
	PathRun                   repos.PathRunRepo
	NodeRun                   repos.NodeRunRepo
	ActivityRun               repos.ActivityRunRepo
	PathRunTransition         repos.PathRunTransitionRepo
	DecisionTrace             repos.DecisionTraceRepo
	StructuralDecisionTrace   repos.StructuralDecisionTraceRepo
	GraphVersion              repos.GraphVersionRepo
	StructuralDriftMetric     repos.StructuralDriftMetricRepo
	RollbackEvent             repos.RollbackEventRepo
	TeachingPattern           repos.TeachingPatternRepo
	UserCompletedUnit         repos.UserCompletedUnitRepo
	UserLibraryIndex          repos.UserLibraryIndexRepo
	LibraryTaxonomyNode       repos.LibraryTaxonomyNodeRepo
	LibraryTaxonomyEdge       repos.LibraryTaxonomyEdgeRepo
	LibraryTaxonomyMember     repos.LibraryTaxonomyMembershipRepo
	LibraryTaxonomyState      repos.LibraryTaxonomyStateRepo
	LibraryTaxonomySnapshot   repos.LibraryTaxonomySnapshotRepo
	LibraryPathEmbedding      repos.LibraryPathEmbeddingRepo
	LearningNodeDoc           repos.LearningNodeDocRepo
	LearningNodeDocRevision   repos.LearningNodeDocRevisionRepo
	LearningNodeFigure        repos.LearningNodeFigureRepo
	LearningNodeVideo         repos.LearningNodeVideoRepo
	DocGenerationRun          repos.LearningDocGenerationRunRepo
	LearningNodeDocBlueprint  repos.LearningNodeDocBlueprintRepo
	LearningNodeDocVariant    repos.LearningNodeDocVariantRepo
	UserDocSignalSnapshot     repos.UserDocSignalSnapshotRepo
	UserBeliefSnapshot        repos.UserBeliefSnapshotRepo
	InterventionPlan          repos.InterventionPlanRepo
	ConceptReadinessSnapshot  repos.ConceptReadinessSnapshotRepo
	PrereqGateDecision        repos.PrereqGateDecisionRepo
	DocRetrievalPack          repos.DocRetrievalPackRepo
	DocGenerationTrace        repos.DocGenerationTraceRepo
	DocConstraintReport       repos.DocConstraintReportRepo
	DocProbe                  repos.DocProbeRepo
	DocProbeOutcome           repos.DocProbeOutcomeRepo
	DocVariantExposure        repos.DocVariantExposureRepo
	DocVariantOutcome         repos.DocVariantOutcomeRepo
	DrillInstance             repos.LearningDrillInstanceRepo
	LearningArtifact          repos.LearningArtifactRepo
	ChatThread                repos.ChatThreadRepo
	ChatMessage               repos.ChatMessageRepo
	ChatThreadState           repos.ChatThreadStateRepo
	ChatSummaryNode           repos.ChatSummaryNodeRepo
	ChatMemoryItem            repos.ChatMemoryItemRepo
	ChatEntity                repos.ChatEntityRepo
	ChatEdge                  repos.ChatEdgeRepo
	ChatClaim                 repos.ChatClaimRepo
	ChatDoc                   repos.ChatDocRepo
	ChatTurn                  repos.ChatTurnRepo
}

func wireRepos(db *gorm.DB, log *logger.Logger) Repos {
	log.Info("Wiring repos...")
	return Repos{
		User:                      repos.NewUserRepo(db, log),
		UserProfileVector:         repos.NewUserProfileVectorRepo(db, log),
		UserPersonalizationPrefs:  repos.NewUserPersonalizationPrefsRepo(db, log),
		UserSessionState:          repos.NewUserSessionStateRepo(db, log),
		UserToken:                 repos.NewUserTokenRepo(db, log),
		UserIdentity:              repos.NewUserIdentityRepo(db, log),
		OAuthNonce:                repos.NewOAuthNonceRepo(db, log),
		Asset:                     repos.NewAssetRepo(db, log),
		MaterialSet:               repos.NewMaterialSetRepo(db, log),
		MaterialSetFile:           repos.NewMaterialSetFileRepo(db, log),
		MaterialFile:              repos.NewMaterialFileRepo(db, log),
		MaterialFileSignature:     repos.NewMaterialFileSignatureRepo(db, log),
		MaterialFileSection:       repos.NewMaterialFileSectionRepo(db, log),
		MaterialChunk:             repos.NewMaterialChunkRepo(db, log),
		MaterialAsset:             repos.NewMaterialAssetRepo(db, log),
		MaterialSetSummary:        repos.NewMaterialSetSummaryRepo(db, log),
		JobRun:                    repos.NewJobRunRepo(db, log),
		SagaRun:                   repos.NewSagaRunRepo(db, log),
		SagaAction:                repos.NewSagaActionRepo(db, log),
		TopicMastery:              repos.NewTopicMasteryRepo(db, log),
		TopicStylePreference:      repos.NewTopicStylePreferenceRepo(db, log),
		UserEvent:                 repos.NewUserEventRepo(db, log),
		UserEventCursor:           repos.NewUserEventCursorRepo(db, log),
		UserProgressionEvent:      repos.NewUserProgressionEventRepo(db, log),
		UserGazeEvent:             repos.NewUserGazeEventRepo(db, log),
		UserGazeBlockStat:         repos.NewUserGazeBlockStatRepo(db, log),
		UserConceptState:          repos.NewUserConceptStateRepo(db, log),
		UserConceptModel:          repos.NewUserConceptModelRepo(db, log),
		UserConceptEdgeStat:       repos.NewUserConceptEdgeStatRepo(db, log),
		UserConceptEvidence:       repos.NewUserConceptEvidenceRepo(db, log),
		UserConceptCalibration:    repos.NewUserConceptCalibrationRepo(db, log),
		ItemCalibration:           repos.NewItemCalibrationRepo(db, log),
		UserModelAlert:            repos.NewUserModelAlertRepo(db, log),
		UserMisconception:         repos.NewUserMisconceptionInstanceRepo(db, log),
		UserMisconceptionInstance: repos.NewUserMisconceptionInstanceRepo(db, log),
		MisconceptionCausalEdge:   repos.NewMisconceptionCausalEdgeRepo(db, log),
		MisconceptionResolution:   repos.NewMisconceptionResolutionStateRepo(db, log),
		UserStylePreference:       repos.NewUserStylePreferenceRepo(db, log),
		UserTestletState:          repos.NewUserTestletStateRepo(db, log),
		UserSkillState:            repos.NewUserSkillStateRepo(db, log),
		Concept:                   repos.NewConceptRepo(db, log),
		ConceptRepresentation:     repos.NewConceptRepresentationRepo(db, log),
		ConceptMappingOverride:    repos.NewConceptMappingOverrideRepo(db, log),
		ConceptCluster:            repos.NewConceptClusterRepo(db, log),
		ConceptClusterMember:      repos.NewConceptClusterMemberRepo(db, log),
		ConceptEdge:               repos.NewConceptEdgeRepo(db, log),
		ConceptEvidence:           repos.NewConceptEvidenceRepo(db, log),
		ChainSignature:            repos.NewChainSignatureRepo(db, log),
		ChainPrior:                repos.NewChainPriorRepo(db, log),
		CohortPrior:               repos.NewCohortPriorRepo(db, log),
		Activity:                  repos.NewActivityRepo(db, log),
		ActivityVariant:           repos.NewActivityVariantRepo(db, log),
		ActivityVariantStat:       repos.NewActivityVariantStatRepo(db, log),
		ActivityConcept:           repos.NewActivityConceptRepo(db, log),
		ActivityCitation:          repos.NewActivityCitationRepo(db, log),
		ModelSnapshot:             repos.NewModelSnapshotRepo(db, log),
		PolicyEvalSnapshot:        repos.NewPolicyEvalSnapshotRepo(db, log),
		Path:                      repos.NewPathRepo(db, log),
		PathNode:                  repos.NewPathNodeRepo(db, log),
		PathNodeActivity:          repos.NewPathNodeActivityRepo(db, log),
		PathStructuralUnit:        repos.NewPathStructuralUnitRepo(db, log),
		PathRun:                   repos.NewPathRunRepo(db, log),
		NodeRun:                   repos.NewNodeRunRepo(db, log),
		ActivityRun:               repos.NewActivityRunRepo(db, log),
		PathRunTransition:         repos.NewPathRunTransitionRepo(db, log),
		DecisionTrace:             repos.NewDecisionTraceRepo(db, log),
		StructuralDecisionTrace:   repos.NewStructuralDecisionTraceRepo(db, log),
		GraphVersion:              repos.NewGraphVersionRepo(db, log),
		StructuralDriftMetric:     repos.NewStructuralDriftMetricRepo(db, log),
		RollbackEvent:             repos.NewRollbackEventRepo(db, log),
		TeachingPattern:           repos.NewTeachingPatternRepo(db, log),
		UserCompletedUnit:         repos.NewUserCompletedUnitRepo(db, log),
		UserLibraryIndex:          repos.NewUserLibraryIndexRepo(db, log),
		LibraryTaxonomyNode:       repos.NewLibraryTaxonomyNodeRepo(db, log),
		LibraryTaxonomyEdge:       repos.NewLibraryTaxonomyEdgeRepo(db, log),
		LibraryTaxonomyMember:     repos.NewLibraryTaxonomyMembershipRepo(db, log),
		LibraryTaxonomyState:      repos.NewLibraryTaxonomyStateRepo(db, log),
		LibraryTaxonomySnapshot:   repos.NewLibraryTaxonomySnapshotRepo(db, log),
		LibraryPathEmbedding:      repos.NewLibraryPathEmbeddingRepo(db, log),
		LearningNodeDoc:           repos.NewLearningNodeDocRepo(db, log),
		LearningNodeDocRevision:   repos.NewLearningNodeDocRevisionRepo(db, log),
		LearningNodeFigure:        repos.NewLearningNodeFigureRepo(db, log),
		LearningNodeVideo:         repos.NewLearningNodeVideoRepo(db, log),
		DocGenerationRun:          repos.NewLearningDocGenerationRunRepo(db, log),
		LearningNodeDocBlueprint:  repos.NewLearningNodeDocBlueprintRepo(db, log),
		LearningNodeDocVariant:    repos.NewLearningNodeDocVariantRepo(db, log),
		UserDocSignalSnapshot:     repos.NewUserDocSignalSnapshotRepo(db, log),
		UserBeliefSnapshot:        repos.NewUserBeliefSnapshotRepo(db, log),
		InterventionPlan:          repos.NewInterventionPlanRepo(db, log),
		ConceptReadinessSnapshot:  repos.NewConceptReadinessSnapshotRepo(db, log),
		PrereqGateDecision:        repos.NewPrereqGateDecisionRepo(db, log),
		DocRetrievalPack:          repos.NewDocRetrievalPackRepo(db, log),
		DocGenerationTrace:        repos.NewDocGenerationTraceRepo(db, log),
		DocConstraintReport:       repos.NewDocConstraintReportRepo(db, log),
		DocProbe:                  repos.NewDocProbeRepo(db, log),
		DocProbeOutcome:           repos.NewDocProbeOutcomeRepo(db, log),
		DocVariantExposure:        repos.NewDocVariantExposureRepo(db, log),
		DocVariantOutcome:         repos.NewDocVariantOutcomeRepo(db, log),
		DrillInstance:             repos.NewLearningDrillInstanceRepo(db, log),
		LearningArtifact:          repos.NewLearningArtifactRepo(db, log),
		ChatThread:                repos.NewChatThreadRepo(db, log),
		ChatMessage:               repos.NewChatMessageRepo(db, log),
		ChatThreadState:           repos.NewChatThreadStateRepo(db, log),
		ChatSummaryNode:           repos.NewChatSummaryNodeRepo(db, log),
		ChatMemoryItem:            repos.NewChatMemoryItemRepo(db, log),
		ChatEntity:                repos.NewChatEntityRepo(db, log),
		ChatEdge:                  repos.NewChatEdgeRepo(db, log),
		ChatClaim:                 repos.NewChatClaimRepo(db, log),
		ChatDoc:                   repos.NewChatDocRepo(db, log),
		ChatTurn:                  repos.NewChatTurnRepo(db, log),
	}
}

package app

import (
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"gorm.io/gorm"
)

type Repos struct {
	User                     repos.UserRepo
	UserProfileVector        repos.UserProfileVectorRepo
	UserPersonalizationPrefs repos.UserPersonalizationPrefsRepo
	UserSessionState         repos.UserSessionStateRepo
	UserToken                repos.UserTokenRepo
	UserIdentity             repos.UserIdentityRepo
	OAuthNonce               repos.OAuthNonceRepo
	Asset                    repos.AssetRepo
	MaterialSet              repos.MaterialSetRepo
	MaterialSetFile          repos.MaterialSetFileRepo
	MaterialFile             repos.MaterialFileRepo
	MaterialChunk            repos.MaterialChunkRepo
	MaterialAsset            repos.MaterialAssetRepo
	MaterialSetSummary       repos.MaterialSetSummaryRepo
	JobRun                   repos.JobRunRepo
	SagaRun                  repos.SagaRunRepo
	SagaAction               repos.SagaActionRepo
	TopicMastery             repos.TopicMasteryRepo
	TopicStylePreference     repos.TopicStylePreferenceRepo
	UserEvent                repos.UserEventRepo
	UserEventCursor          repos.UserEventCursorRepo
	UserProgressionEvent     repos.UserProgressionEventRepo
	UserConceptState         repos.UserConceptStateRepo
	UserStylePreference      repos.UserStylePreferenceRepo
	Concept                  repos.ConceptRepo
	ConceptCluster           repos.ConceptClusterRepo
	ConceptClusterMember     repos.ConceptClusterMemberRepo
	ConceptEdge              repos.ConceptEdgeRepo
	ConceptEvidence          repos.ConceptEvidenceRepo
	ChainSignature           repos.ChainSignatureRepo
	ChainPrior               repos.ChainPriorRepo
	CohortPrior              repos.CohortPriorRepo
	Activity                 repos.ActivityRepo
	ActivityVariant          repos.ActivityVariantRepo
	ActivityVariantStat      repos.ActivityVariantStatRepo
	ActivityConcept          repos.ActivityConceptRepo
	ActivityCitation         repos.ActivityCitationRepo
	Path                     repos.PathRepo
	PathNode                 repos.PathNodeRepo
	PathNodeActivity         repos.PathNodeActivityRepo
	DecisionTrace            repos.DecisionTraceRepo
	TeachingPattern          repos.TeachingPatternRepo
	UserCompletedUnit        repos.UserCompletedUnitRepo
	UserLibraryIndex         repos.UserLibraryIndexRepo
	LibraryTaxonomyNode      repos.LibraryTaxonomyNodeRepo
	LibraryTaxonomyEdge      repos.LibraryTaxonomyEdgeRepo
	LibraryTaxonomyMember    repos.LibraryTaxonomyMembershipRepo
	LibraryTaxonomyState     repos.LibraryTaxonomyStateRepo
	LibraryTaxonomySnapshot  repos.LibraryTaxonomySnapshotRepo
	LibraryPathEmbedding     repos.LibraryPathEmbeddingRepo
	LearningNodeDoc          repos.LearningNodeDocRepo
	LearningNodeDocRevision  repos.LearningNodeDocRevisionRepo
	LearningNodeFigure       repos.LearningNodeFigureRepo
	LearningNodeVideo        repos.LearningNodeVideoRepo
	DocGenerationRun         repos.LearningDocGenerationRunRepo
	DrillInstance            repos.LearningDrillInstanceRepo
	ChatThread               repos.ChatThreadRepo
	ChatMessage              repos.ChatMessageRepo
	ChatThreadState          repos.ChatThreadStateRepo
	ChatSummaryNode          repos.ChatSummaryNodeRepo
	ChatMemoryItem           repos.ChatMemoryItemRepo
	ChatEntity               repos.ChatEntityRepo
	ChatEdge                 repos.ChatEdgeRepo
	ChatClaim                repos.ChatClaimRepo
	ChatDoc                  repos.ChatDocRepo
	ChatTurn                 repos.ChatTurnRepo
}

func wireRepos(db *gorm.DB, log *logger.Logger) Repos {
	log.Info("Wiring repos...")
	return Repos{
		User:                     repos.NewUserRepo(db, log),
		UserProfileVector:        repos.NewUserProfileVectorRepo(db, log),
		UserPersonalizationPrefs: repos.NewUserPersonalizationPrefsRepo(db, log),
		UserSessionState:         repos.NewUserSessionStateRepo(db, log),
		UserToken:                repos.NewUserTokenRepo(db, log),
		UserIdentity:             repos.NewUserIdentityRepo(db, log),
		OAuthNonce:               repos.NewOAuthNonceRepo(db, log),
		Asset:                    repos.NewAssetRepo(db, log),
		MaterialSet:              repos.NewMaterialSetRepo(db, log),
		MaterialSetFile:          repos.NewMaterialSetFileRepo(db, log),
		MaterialFile:             repos.NewMaterialFileRepo(db, log),
		MaterialChunk:            repos.NewMaterialChunkRepo(db, log),
		MaterialAsset:            repos.NewMaterialAssetRepo(db, log),
		MaterialSetSummary:       repos.NewMaterialSetSummaryRepo(db, log),
		JobRun:                   repos.NewJobRunRepo(db, log),
		SagaRun:                  repos.NewSagaRunRepo(db, log),
		SagaAction:               repos.NewSagaActionRepo(db, log),
		TopicMastery:             repos.NewTopicMasteryRepo(db, log),
		TopicStylePreference:     repos.NewTopicStylePreferenceRepo(db, log),
		UserEvent:                repos.NewUserEventRepo(db, log),
		UserEventCursor:          repos.NewUserEventCursorRepo(db, log),
		UserProgressionEvent:     repos.NewUserProgressionEventRepo(db, log),
		UserConceptState:         repos.NewUserConceptStateRepo(db, log),
		UserStylePreference:      repos.NewUserStylePreferenceRepo(db, log),
		Concept:                  repos.NewConceptRepo(db, log),
		ConceptCluster:           repos.NewConceptClusterRepo(db, log),
		ConceptClusterMember:     repos.NewConceptClusterMemberRepo(db, log),
		ConceptEdge:              repos.NewConceptEdgeRepo(db, log),
		ConceptEvidence:          repos.NewConceptEvidenceRepo(db, log),
		ChainSignature:           repos.NewChainSignatureRepo(db, log),
		ChainPrior:               repos.NewChainPriorRepo(db, log),
		CohortPrior:              repos.NewCohortPriorRepo(db, log),
		Activity:                 repos.NewActivityRepo(db, log),
		ActivityVariant:          repos.NewActivityVariantRepo(db, log),
		ActivityVariantStat:      repos.NewActivityVariantStatRepo(db, log),
		ActivityConcept:          repos.NewActivityConceptRepo(db, log),
		ActivityCitation:         repos.NewActivityCitationRepo(db, log),
		Path:                     repos.NewPathRepo(db, log),
		PathNode:                 repos.NewPathNodeRepo(db, log),
		PathNodeActivity:         repos.NewPathNodeActivityRepo(db, log),
		DecisionTrace:            repos.NewDecisionTraceRepo(db, log),
		TeachingPattern:          repos.NewTeachingPatternRepo(db, log),
		UserCompletedUnit:        repos.NewUserCompletedUnitRepo(db, log),
		UserLibraryIndex:         repos.NewUserLibraryIndexRepo(db, log),
		LibraryTaxonomyNode:      repos.NewLibraryTaxonomyNodeRepo(db, log),
		LibraryTaxonomyEdge:      repos.NewLibraryTaxonomyEdgeRepo(db, log),
		LibraryTaxonomyMember:    repos.NewLibraryTaxonomyMembershipRepo(db, log),
		LibraryTaxonomyState:     repos.NewLibraryTaxonomyStateRepo(db, log),
		LibraryTaxonomySnapshot:  repos.NewLibraryTaxonomySnapshotRepo(db, log),
		LibraryPathEmbedding:     repos.NewLibraryPathEmbeddingRepo(db, log),
		LearningNodeDoc:          repos.NewLearningNodeDocRepo(db, log),
		LearningNodeDocRevision:  repos.NewLearningNodeDocRevisionRepo(db, log),
		LearningNodeFigure:       repos.NewLearningNodeFigureRepo(db, log),
		LearningNodeVideo:        repos.NewLearningNodeVideoRepo(db, log),
		DocGenerationRun:         repos.NewLearningDocGenerationRunRepo(db, log),
		DrillInstance:            repos.NewLearningDrillInstanceRepo(db, log),
		ChatThread:               repos.NewChatThreadRepo(db, log),
		ChatMessage:              repos.NewChatMessageRepo(db, log),
		ChatThreadState:          repos.NewChatThreadStateRepo(db, log),
		ChatSummaryNode:          repos.NewChatSummaryNodeRepo(db, log),
		ChatMemoryItem:           repos.NewChatMemoryItemRepo(db, log),
		ChatEntity:               repos.NewChatEntityRepo(db, log),
		ChatEdge:                 repos.NewChatEdgeRepo(db, log),
		ChatClaim:                repos.NewChatClaimRepo(db, log),
		ChatDoc:                  repos.NewChatDocRepo(db, log),
		ChatTurn:                 repos.NewChatTurnRepo(db, log),
	}
}

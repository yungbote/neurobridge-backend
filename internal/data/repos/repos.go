package repos

import (
	"github.com/yungbote/neurobridge-backend/internal/data/repos/auth"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/chat"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/jobs"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/learning"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/materials"
	"github.com/yungbote/neurobridge-backend/internal/data/repos/user"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"gorm.io/gorm"
)

type UserRepo = user.UserRepo
type UserProfileVectorRepo = user.UserProfileVectorRepo
type UserPersonalizationPrefsRepo = user.UserPersonalizationPrefsRepo
type UserSessionStateRepo = user.UserSessionStateRepo
type UserTokenRepo = auth.UserTokenRepo
type UserIdentityRepo = auth.UserIdentityRepo
type OAuthNonceRepo = auth.OAuthNonceRepo

type AssetRepo = materials.AssetRepo
type MaterialSetRepo = materials.MaterialSetRepo
type MaterialSetFileRepo = materials.MaterialSetFileRepo
type MaterialFileRepo = materials.MaterialFileRepo
type MaterialFileSignatureRepo = materials.MaterialFileSignatureRepo
type MaterialFileSectionRepo = materials.MaterialFileSectionRepo
type MaterialChunkRepo = materials.MaterialChunkRepo
type MaterialAssetRepo = materials.MaterialAssetRepo
type MaterialSetSummaryRepo = materials.MaterialSetSummaryRepo

type LearningProfileRepo = learning.LearningProfileRepo
type TopicMasteryRepo = learning.TopicMasteryRepo
type TopicStylePreferenceRepo = learning.TopicStylePreferenceRepo
type UserConceptStateRepo = learning.UserConceptStateRepo
type UserConceptModelRepo = learning.UserConceptModelRepo
type UserMisconceptionInstanceRepo = learning.UserMisconceptionInstanceRepo
type UserStylePreferenceRepo = learning.UserStylePreferenceRepo
type UserEventRepo = learning.UserEventRepo
type UserEventCursorRepo = learning.UserEventCursorRepo
type UserProgressionEventRepo = learning.UserProgressionEventRepo

type ConceptRepo = learning.ConceptRepo
type ConceptRepresentationRepo = learning.ConceptRepresentationRepo
type ConceptMappingOverrideRepo = learning.ConceptMappingOverrideRepo
type ActivityRepo = learning.ActivityRepo
type ActivityVariantRepo = learning.ActivityVariantRepo
type ActivityConceptRepo = learning.ActivityConceptRepo
type ActivityCitationRepo = learning.ActivityCitationRepo

type PathRepo = learning.PathRepo
type PathNodeRepo = learning.PathNodeRepo
type PathNodeActivityRepo = learning.PathNodeActivityRepo
type PathStructuralUnitRepo = learning.PathStructuralUnitRepo
type PathRunRepo = learning.PathRunRepo
type NodeRunRepo = learning.NodeRunRepo
type ActivityRunRepo = learning.ActivityRunRepo
type PathRunTransitionRepo = learning.PathRunTransitionRepo

type ConceptClusterRepo = learning.ConceptClusterRepo
type ConceptClusterMemberRepo = learning.ConceptClusterMemberRepo
type ConceptEdgeRepo = learning.ConceptEdgeRepo
type ConceptEvidenceRepo = learning.ConceptEvidenceRepo
type CohortPriorRepo = learning.CohortPriorRepo
type ActivityVariantStatRepo = learning.ActivityVariantStatRepo
type DecisionTraceRepo = learning.DecisionTraceRepo
type UserLibraryIndexRepo = learning.UserLibraryIndexRepo
type ChainSignatureRepo = learning.ChainSignatureRepo
type ChainPriorRepo = learning.ChainPriorRepo
type UserCompletedUnitRepo = learning.UserCompletedUnitRepo
type TeachingPatternRepo = learning.TeachingPatternRepo
type LearningNodeDocRepo = learning.LearningNodeDocRepo
type LearningNodeDocRevisionRepo = learning.LearningNodeDocRevisionRepo
type LearningNodeFigureRepo = learning.LearningNodeFigureRepo
type LearningNodeVideoRepo = learning.LearningNodeVideoRepo
type LearningDocGenerationRunRepo = learning.LearningDocGenerationRunRepo
type LearningDrillInstanceRepo = learning.LearningDrillInstanceRepo
type LearningArtifactRepo = learning.LearningArtifactRepo
type LibraryTaxonomyNodeRepo = learning.LibraryTaxonomyNodeRepo
type LibraryTaxonomyEdgeRepo = learning.LibraryTaxonomyEdgeRepo
type LibraryTaxonomyMembershipRepo = learning.LibraryTaxonomyMembershipRepo
type LibraryTaxonomyStateRepo = learning.LibraryTaxonomyStateRepo
type LibraryTaxonomySnapshotRepo = learning.LibraryTaxonomySnapshotRepo
type LibraryPathEmbeddingRepo = learning.LibraryPathEmbeddingRepo

type JobRunRepo = jobs.JobRunRepo
type SagaRunRepo = jobs.SagaRunRepo
type SagaActionRepo = jobs.SagaActionRepo

type ChatThreadRepo = chat.ChatThreadRepo
type ChatMessageRepo = chat.ChatMessageRepo
type ChatThreadStateRepo = chat.ChatThreadStateRepo
type ChatSummaryNodeRepo = chat.ChatSummaryNodeRepo
type ChatMemoryItemRepo = chat.ChatMemoryItemRepo
type ChatEntityRepo = chat.ChatEntityRepo
type ChatEdgeRepo = chat.ChatEdgeRepo
type ChatClaimRepo = chat.ChatClaimRepo
type ChatDocRepo = chat.ChatDocRepo
type ChatTurnRepo = chat.ChatTurnRepo

func NewUserRepo(db *gorm.DB, baseLog *logger.Logger) UserRepo { return user.NewUserRepo(db, baseLog) }
func NewUserProfileVectorRepo(db *gorm.DB, baseLog *logger.Logger) UserProfileVectorRepo {
	return user.NewUserProfileVectorRepo(db, baseLog)
}
func NewUserPersonalizationPrefsRepo(db *gorm.DB, baseLog *logger.Logger) UserPersonalizationPrefsRepo {
	return user.NewUserPersonalizationPrefsRepo(db, baseLog)
}
func NewUserSessionStateRepo(db *gorm.DB, baseLog *logger.Logger) UserSessionStateRepo {
	return user.NewUserSessionStateRepo(db, baseLog)
}
func NewUserTokenRepo(db *gorm.DB, baseLog *logger.Logger) UserTokenRepo {
	return auth.NewUserTokenRepo(db, baseLog)
}

func NewUserIdentityRepo(db *gorm.DB, baseLog *logger.Logger) UserIdentityRepo {
	return auth.NewUserIdentityRepo(db, baseLog)
}

func NewOAuthNonceRepo(db *gorm.DB, baseLog *logger.Logger) OAuthNonceRepo {
	return auth.NewOAuthNonceRepo(db, baseLog)
}

func NewAssetRepo(db *gorm.DB, baseLog *logger.Logger) AssetRepo {
	return materials.NewAssetRepo(db, baseLog)
}
func NewMaterialSetRepo(db *gorm.DB, baseLog *logger.Logger) MaterialSetRepo {
	return materials.NewMaterialSetRepo(db, baseLog)
}
func NewMaterialSetFileRepo(db *gorm.DB, baseLog *logger.Logger) MaterialSetFileRepo {
	return materials.NewMaterialSetFileRepo(db, baseLog)
}
func NewMaterialFileRepo(db *gorm.DB, baseLog *logger.Logger) MaterialFileRepo {
	return materials.NewMaterialFileRepo(db, baseLog)
}
func NewMaterialFileSignatureRepo(db *gorm.DB, baseLog *logger.Logger) MaterialFileSignatureRepo {
	return materials.NewMaterialFileSignatureRepo(db, baseLog)
}
func NewMaterialFileSectionRepo(db *gorm.DB, baseLog *logger.Logger) MaterialFileSectionRepo {
	return materials.NewMaterialFileSectionRepo(db, baseLog)
}
func NewMaterialChunkRepo(db *gorm.DB, baseLog *logger.Logger) MaterialChunkRepo {
	return materials.NewMaterialChunkRepo(db, baseLog)
}
func NewMaterialAssetRepo(db *gorm.DB, baseLog *logger.Logger) MaterialAssetRepo {
	return materials.NewMaterialAssetRepo(db, baseLog)
}
func NewMaterialSetSummaryRepo(db *gorm.DB, baseLog *logger.Logger) MaterialSetSummaryRepo {
	return materials.NewMaterialSetSummaryRepo(db, baseLog)
}

func NewLearningProfileRepo(db *gorm.DB, baseLog *logger.Logger) LearningProfileRepo {
	return learning.NewLearningProfileRepo(db, baseLog)
}
func NewTopicMasteryRepo(db *gorm.DB, baseLog *logger.Logger) TopicMasteryRepo {
	return learning.NewTopicMasteryRepo(db, baseLog)
}
func NewTopicStylePreferenceRepo(db *gorm.DB, baseLog *logger.Logger) TopicStylePreferenceRepo {
	return learning.NewTopicStylePreferenceRepo(db, baseLog)
}
func NewUserConceptStateRepo(db *gorm.DB, baseLog *logger.Logger) UserConceptStateRepo {
	return learning.NewUserConceptStateRepo(db, baseLog)
}
func NewUserConceptModelRepo(db *gorm.DB, baseLog *logger.Logger) UserConceptModelRepo {
	return learning.NewUserConceptModelRepo(db, baseLog)
}
func NewUserMisconceptionInstanceRepo(db *gorm.DB, baseLog *logger.Logger) UserMisconceptionInstanceRepo {
	return learning.NewUserMisconceptionInstanceRepo(db, baseLog)
}
func NewUserStylePreferenceRepo(db *gorm.DB, baseLog *logger.Logger) UserStylePreferenceRepo {
	return learning.NewUserStylePreferenceRepo(db, baseLog)
}
func NewUserEventRepo(db *gorm.DB, baseLog *logger.Logger) UserEventRepo {
	return learning.NewUserEventRepo(db, baseLog)
}
func NewUserEventCursorRepo(db *gorm.DB, baseLog *logger.Logger) UserEventCursorRepo {
	return learning.NewUserEventCursorRepo(db, baseLog)
}
func NewUserProgressionEventRepo(db *gorm.DB, baseLog *logger.Logger) UserProgressionEventRepo {
	return learning.NewUserProgressionEventRepo(db, baseLog)
}

func NewConceptRepo(db *gorm.DB, baseLog *logger.Logger) ConceptRepo {
	return learning.NewConceptRepo(db, baseLog)
}
func NewConceptRepresentationRepo(db *gorm.DB, baseLog *logger.Logger) ConceptRepresentationRepo {
	return learning.NewConceptRepresentationRepo(db, baseLog)
}
func NewConceptMappingOverrideRepo(db *gorm.DB, baseLog *logger.Logger) ConceptMappingOverrideRepo {
	return learning.NewConceptMappingOverrideRepo(db, baseLog)
}
func NewActivityRepo(db *gorm.DB, baseLog *logger.Logger) ActivityRepo {
	return learning.NewActivityRepo(db, baseLog)
}
func NewActivityVariantRepo(db *gorm.DB, baseLog *logger.Logger) ActivityVariantRepo {
	return learning.NewActivityVariantRepo(db, baseLog)
}
func NewActivityConceptRepo(db *gorm.DB, baseLog *logger.Logger) ActivityConceptRepo {
	return learning.NewActivityConceptRepo(db, baseLog)
}
func NewActivityCitationRepo(db *gorm.DB, baseLog *logger.Logger) ActivityCitationRepo {
	return learning.NewActivityCitationRepo(db, baseLog)
}

func NewPathRepo(db *gorm.DB, baseLog *logger.Logger) PathRepo {
	return learning.NewPathRepo(db, baseLog)
}
func NewPathNodeRepo(db *gorm.DB, baseLog *logger.Logger) PathNodeRepo {
	return learning.NewPathNodeRepo(db, baseLog)
}
func NewPathNodeActivityRepo(db *gorm.DB, baseLog *logger.Logger) PathNodeActivityRepo {
	return learning.NewPathNodeActivityRepo(db, baseLog)
}
func NewPathStructuralUnitRepo(db *gorm.DB, baseLog *logger.Logger) PathStructuralUnitRepo {
	return learning.NewPathStructuralUnitRepo(db, baseLog)
}
func NewPathRunRepo(db *gorm.DB, baseLog *logger.Logger) PathRunRepo {
	return learning.NewPathRunRepo(db, baseLog)
}
func NewNodeRunRepo(db *gorm.DB, baseLog *logger.Logger) NodeRunRepo {
	return learning.NewNodeRunRepo(db, baseLog)
}
func NewActivityRunRepo(db *gorm.DB, baseLog *logger.Logger) ActivityRunRepo {
	return learning.NewActivityRunRepo(db, baseLog)
}
func NewPathRunTransitionRepo(db *gorm.DB, baseLog *logger.Logger) PathRunTransitionRepo {
	return learning.NewPathRunTransitionRepo(db, baseLog)
}

func NewChainSignatureRepo(db *gorm.DB, baseLog *logger.Logger) ChainSignatureRepo {
	return learning.NewChainSignatureRepo(db, baseLog)
}

func NewChainPriorRepo(db *gorm.DB, baseLog *logger.Logger) ChainPriorRepo {
	return learning.NewChainPriorRepo(db, baseLog)
}

func NewUserCompletedUnitRepo(db *gorm.DB, baseLog *logger.Logger) UserCompletedUnitRepo {
	return learning.NewUserCompletedUnitRepo(db, baseLog)
}

func NewTeachingPatternRepo(db *gorm.DB, baseLog *logger.Logger) TeachingPatternRepo {
	return learning.NewTeachingPatternRepo(db, baseLog)
}
func NewLearningNodeDocRepo(db *gorm.DB, baseLog *logger.Logger) LearningNodeDocRepo {
	return learning.NewLearningNodeDocRepo(db, baseLog)
}
func NewLearningNodeDocRevisionRepo(db *gorm.DB, baseLog *logger.Logger) LearningNodeDocRevisionRepo {
	return learning.NewLearningNodeDocRevisionRepo(db, baseLog)
}
func NewLearningNodeFigureRepo(db *gorm.DB, baseLog *logger.Logger) LearningNodeFigureRepo {
	return learning.NewLearningNodeFigureRepo(db, baseLog)
}
func NewLearningNodeVideoRepo(db *gorm.DB, baseLog *logger.Logger) LearningNodeVideoRepo {
	return learning.NewLearningNodeVideoRepo(db, baseLog)
}
func NewLearningDocGenerationRunRepo(db *gorm.DB, baseLog *logger.Logger) LearningDocGenerationRunRepo {
	return learning.NewLearningDocGenerationRunRepo(db, baseLog)
}
func NewLearningDrillInstanceRepo(db *gorm.DB, baseLog *logger.Logger) LearningDrillInstanceRepo {
	return learning.NewLearningDrillInstanceRepo(db, baseLog)
}
func NewLearningArtifactRepo(db *gorm.DB, baseLog *logger.Logger) LearningArtifactRepo {
	return learning.NewLearningArtifactRepo(db, baseLog)
}
func NewLibraryTaxonomyNodeRepo(db *gorm.DB, baseLog *logger.Logger) LibraryTaxonomyNodeRepo {
	return learning.NewLibraryTaxonomyNodeRepo(db, baseLog)
}
func NewLibraryTaxonomyEdgeRepo(db *gorm.DB, baseLog *logger.Logger) LibraryTaxonomyEdgeRepo {
	return learning.NewLibraryTaxonomyEdgeRepo(db, baseLog)
}
func NewLibraryTaxonomyMembershipRepo(db *gorm.DB, baseLog *logger.Logger) LibraryTaxonomyMembershipRepo {
	return learning.NewLibraryTaxonomyMembershipRepo(db, baseLog)
}
func NewLibraryTaxonomyStateRepo(db *gorm.DB, baseLog *logger.Logger) LibraryTaxonomyStateRepo {
	return learning.NewLibraryTaxonomyStateRepo(db, baseLog)
}
func NewLibraryTaxonomySnapshotRepo(db *gorm.DB, baseLog *logger.Logger) LibraryTaxonomySnapshotRepo {
	return learning.NewLibraryTaxonomySnapshotRepo(db, baseLog)
}
func NewLibraryPathEmbeddingRepo(db *gorm.DB, baseLog *logger.Logger) LibraryPathEmbeddingRepo {
	return learning.NewLibraryPathEmbeddingRepo(db, baseLog)
}

func NewJobRunRepo(db *gorm.DB, baseLog *logger.Logger) JobRunRepo {
	return jobs.NewJobRunRepo(db, baseLog)
}
func NewSagaRunRepo(db *gorm.DB, baseLog *logger.Logger) SagaRunRepo {
	return jobs.NewSagaRunRepo(db, baseLog)
}
func NewSagaActionRepo(db *gorm.DB, baseLog *logger.Logger) SagaActionRepo {
	return jobs.NewSagaActionRepo(db, baseLog)
}

func NewConceptClusterRepo(db *gorm.DB, baseLog *logger.Logger) ConceptClusterRepo {
	return learning.NewConceptClusterRepo(db, baseLog)
}

func NewConceptClusterMemberRepo(db *gorm.DB, baseLog *logger.Logger) ConceptClusterMemberRepo {
	return learning.NewConceptClusterMemberRepo(db, baseLog)
}

func NewConceptEdgeRepo(db *gorm.DB, baseLog *logger.Logger) ConceptEdgeRepo {
	return learning.NewConceptEdgeRepo(db, baseLog)
}

func NewCohortPriorRepo(db *gorm.DB, baseLog *logger.Logger) CohortPriorRepo {
	return learning.NewCohortPriorRepo(db, baseLog)
}

func NewActivityVariantStatRepo(db *gorm.DB, baseLog *logger.Logger) ActivityVariantStatRepo {
	return learning.NewActivityVariantStatRepo(db, baseLog)
}

func NewDecisionTraceRepo(db *gorm.DB, baseLog *logger.Logger) DecisionTraceRepo {
	return learning.NewDecisionTraceRepo(db, baseLog)
}

func NewUserLibraryIndexRepo(db *gorm.DB, baseLog *logger.Logger) UserLibraryIndexRepo {
	return learning.NewUserLibraryIndexRepo(db, baseLog)
}

func NewConceptEvidenceRepo(db *gorm.DB, baseLog *logger.Logger) ConceptEvidenceRepo {
	return learning.NewConceptEvidenceRepo(db, baseLog)
}

func NewChatThreadRepo(db *gorm.DB, baseLog *logger.Logger) ChatThreadRepo {
	return chat.NewChatThreadRepo(db, baseLog)
}

func NewChatMessageRepo(db *gorm.DB, baseLog *logger.Logger) ChatMessageRepo {
	return chat.NewChatMessageRepo(db, baseLog)
}

func NewChatThreadStateRepo(db *gorm.DB, baseLog *logger.Logger) ChatThreadStateRepo {
	return chat.NewChatThreadStateRepo(db, baseLog)
}

func NewChatSummaryNodeRepo(db *gorm.DB, baseLog *logger.Logger) ChatSummaryNodeRepo {
	return chat.NewChatSummaryNodeRepo(db, baseLog)
}

func NewChatMemoryItemRepo(db *gorm.DB, baseLog *logger.Logger) ChatMemoryItemRepo {
	return chat.NewChatMemoryItemRepo(db, baseLog)
}

func NewChatEntityRepo(db *gorm.DB, baseLog *logger.Logger) ChatEntityRepo {
	return chat.NewChatEntityRepo(db, baseLog)
}

func NewChatEdgeRepo(db *gorm.DB, baseLog *logger.Logger) ChatEdgeRepo {
	return chat.NewChatEdgeRepo(db, baseLog)
}

func NewChatClaimRepo(db *gorm.DB, baseLog *logger.Logger) ChatClaimRepo {
	return chat.NewChatClaimRepo(db, baseLog)
}

func NewChatDocRepo(db *gorm.DB, baseLog *logger.Logger) ChatDocRepo {
	return chat.NewChatDocRepo(db, baseLog)
}

func NewChatTurnRepo(db *gorm.DB, baseLog *logger.Logger) ChatTurnRepo {
	return chat.NewChatTurnRepo(db, baseLog)
}

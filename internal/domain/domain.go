package domain

import (
	"github.com/yungbote/neurobridge-backend/internal/domain/auth"
	"github.com/yungbote/neurobridge-backend/internal/domain/chat"
	"github.com/yungbote/neurobridge-backend/internal/domain/jobs"
	"github.com/yungbote/neurobridge-backend/internal/domain/learning/core"
	"github.com/yungbote/neurobridge-backend/internal/domain/learning/joins"
	"github.com/yungbote/neurobridge-backend/internal/domain/learning/personalization"
	"github.com/yungbote/neurobridge-backend/internal/domain/learning/products"
	"github.com/yungbote/neurobridge-backend/internal/domain/learning/runtime"
	"github.com/yungbote/neurobridge-backend/internal/domain/materials"
	"github.com/yungbote/neurobridge-backend/internal/domain/user"
	"gorm.io/datatypes"
)

const (
	EventSessionStarted = personalization.EventSessionStarted
	EventSessionEnded   = personalization.EventSessionEnded

	EventPathOpened     = personalization.EventPathOpened
	EventPathClosed     = personalization.EventPathClosed
	EventNodeOpened     = personalization.EventNodeOpened
	EventNodeClosed     = personalization.EventNodeClosed
	EventActivityOpened = personalization.EventActivityOpened
	EventActivityClosed = personalization.EventActivityClosed

	EventActivityStarted   = personalization.EventActivityStarted
	EventActivityCompleted = personalization.EventActivityCompleted
	EventActivityAbandoned = personalization.EventActivityAbandoned

	EventScrollDepth     = personalization.EventScrollDepth
	EventBlockViewed     = personalization.EventBlockViewed
	EventBlockRead       = personalization.EventBlockRead
	EventTextSelected    = personalization.EventTextSelected
	EventNoteCreated     = personalization.EventNoteCreated
	EventBookmarkCreated = personalization.EventBookmarkCreated

	EventVideoPlayed    = personalization.EventVideoPlayed
	EventVideoPaused    = personalization.EventVideoPaused
	EventVideoSeeked    = personalization.EventVideoSeeked
	EventAudoPlayed     = personalization.EventAudoPlayed
	EVentDiagramToggled = personalization.EVentDiagramToggled

	EventQuizStarted      = personalization.EventQuizStarted
	EventQuestionAnswered = personalization.EventQuestionAnswered
	EventQuizCompleted    = personalization.EventQuizCompleted

	EventHintUsed          = personalization.EventHintUsed
	EventExplanationOpened = personalization.EventExplanationOpened

	EventFeedbackThumbsUp       = personalization.EventFeedbackThumbsUp
	EventFeedbackThumbsDown     = personalization.EventFeedbackThumbsDown
	EventFeedbackTooEasy        = personalization.EventFeedbackTooEasy
	EventFeedbackTooHard        = personalization.EventFeedbackTooHard
	EventFeedbackConfusing      = personalization.EventFeedbackConfusing
	EventFeedbackLovedDiagram   = personalization.EventFeedbackLovedDiagram
	EventFeedbackWantExamples   = personalization.EventFeedbackWantExamples
	EventRuntimePromptCompleted = personalization.EventRuntimePromptCompleted
	EventRuntimePromptDismissed = personalization.EventRuntimePromptDismissed

	EventClientError = personalization.EventClientError
	EventClientPerf  = personalization.EventClientPerf

	EventConceptClaimEvaluated     = personalization.EventConceptClaimEvaluated
	EventBridgeValidationNeeded    = personalization.EventBridgeValidationNeeded
	EventExperimentExposure        = personalization.EventExperimentExposure
	EventExperimentGuardrailBreach = personalization.EventExperimentGuardrailBreach
	EventEngagementFunnelStep      = personalization.EventEngagementFunnelStep
	EventCostTelemetry             = personalization.EventCostTelemetry
	EventSecurityEvent             = personalization.EventSecurityEvent
)

type User = user.User
type UserProfileVector = user.UserProfileVector
type UserPersonalizationPrefs = user.UserPersonalizationPrefs
type UserSessionState = user.UserSessionState
type UserToken = auth.UserToken
type UserIdentity = auth.UserIdentity
type OAuthNonce = auth.OAuthNonce

type Asset = materials.Asset
type MaterialSet = materials.MaterialSet
type MaterialSetFile = materials.MaterialSetFile
type MaterialFile = materials.MaterialFile
type MaterialFileSignature = materials.MaterialFileSignature
type MaterialFileSection = materials.MaterialFileSection
type MaterialChunk = materials.MaterialChunk
type MaterialAsset = materials.MaterialAsset
type MaterialSetSummary = materials.MaterialSetSummary
type MaterialIntent = materials.MaterialIntent
type MaterialChunkSignal = materials.MaterialChunkSignal
type MaterialSetIntent = materials.MaterialSetIntent
type MaterialEdge = materials.MaterialEdge
type MaterialSetConceptCoverage = materials.MaterialSetConceptCoverage
type MaterialChunkLink = materials.MaterialChunkLink
type GlobalConceptCoverage = materials.GlobalConceptCoverage
type MaterialSetEdge = materials.MaterialSetEdge
type EmergentConcept = materials.EmergentConcept
type GlobalEntity = materials.GlobalEntity
type MaterialEntity = materials.MaterialEntity
type MaterialClaim = materials.MaterialClaim
type MaterialChunkEntity = materials.MaterialChunkEntity
type MaterialChunkClaim = materials.MaterialChunkClaim
type MaterialClaimEntity = materials.MaterialClaimEntity
type MaterialClaimConcept = materials.MaterialClaimConcept
type MaterialReference = materials.MaterialReference
type MaterialChunkReference = materials.MaterialChunkReference
type Segment = materials.Segment

func PtrFloat(v float64) *float64 { return materials.PtrFloat(v) }

type JobRun = jobs.JobRun
type JobRunEvent = jobs.JobRunEvent
type SagaRun = jobs.SagaRun
type SagaAction = jobs.SagaAction

type LearningProfile = personalization.LearningProfile
type TopicMastery = personalization.TopicMastery
type TopicStylePreference = personalization.TopicStylePreference
type UserConceptState = personalization.UserConceptState
type UserConceptModel = personalization.UserConceptModel
type UserConceptEdgeStat = personalization.UserConceptEdgeStat
type UserConceptEvidence = personalization.UserConceptEvidence
type UserConceptCalibration = personalization.UserConceptCalibration
type ItemCalibration = personalization.ItemCalibration
type UserModelAlert = personalization.UserModelAlert
type UserMisconceptionInstance = personalization.UserMisconceptionInstance
type MisconceptionCausalEdge = personalization.MisconceptionCausalEdge
type MisconceptionSupport = personalization.MisconceptionSupport
type MisconceptionSupportPointer = personalization.MisconceptionSupportPointer
type UserStylePreference = personalization.UserStylePreference
type UserTestletState = personalization.UserTestletState
type UserSkillState = personalization.UserSkillState
type UserEvent = personalization.UserEvent
type UserEventCursor = personalization.UserEventCursor
type UserGazeEvent = personalization.UserGazeEvent
type UserGazeBlockStat = personalization.UserGazeBlockStat
type UserProgressionEvent = personalization.UserProgressionEvent
type UserBeliefSnapshot = personalization.UserBeliefSnapshot
type InterventionPlan = personalization.InterventionPlan
type ConceptReadinessSnapshot = personalization.ConceptReadinessSnapshot
type PrereqGateDecision = personalization.PrereqGateDecision
type MisconceptionResolutionState = personalization.MisconceptionResolutionState

const MisconceptionSupportSchemaVersion = personalization.MisconceptionSupportSchemaVersion

func NormalizeMisconceptionSignature(sig string) string {
	return personalization.NormalizeMisconceptionSignature(sig)
}

func DecodeMisconceptionSupport(raw datatypes.JSON) personalization.MisconceptionSupport {
	return personalization.DecodeMisconceptionSupport(raw)
}

func EncodeMisconceptionSupport(s personalization.MisconceptionSupport) datatypes.JSON {
	return personalization.EncodeMisconceptionSupport(s)
}

func MergeMisconceptionSupportPointer(s personalization.MisconceptionSupport, ptr personalization.MisconceptionSupportPointer, max int) personalization.MisconceptionSupport {
	return personalization.MergeMisconceptionSupportPointer(s, ptr, max)
}

func AddMisconceptionTriggerContext(s personalization.MisconceptionSupport, ctx string, max int) personalization.MisconceptionSupport {
	return personalization.AddMisconceptionTriggerContext(s, ctx, max)
}

type Concept = core.Concept
type ConceptRepresentation = core.ConceptRepresentation
type ConceptMappingOverride = core.ConceptMappingOverride
type Activity = core.Activity
type ActivityVariant = core.ActivityVariant
type ActivityConcept = joins.ActivityConcept
type ActivityCitation = joins.ActivityCitation

type Path = core.Path
type PathNode = core.PathNode
type PathStructuralUnit = core.PathStructuralUnit
type PathNodeActivity = joins.PathNodeActivity

type PathRun = runtime.PathRun
type NodeRun = runtime.NodeRun
type ActivityRun = runtime.ActivityRun
type PathRunTransition = runtime.PathRunTransition

type ConceptEvidence = products.ConceptEvidence
type ConceptEdge = products.ConceptEdge
type ConceptCluster = products.ConceptCluster
type ConceptClusterMember = products.ConceptClusterMember

type UserLibraryIndex = products.UserLibraryIndex
type CohortPrior = products.CohortPrior
type DecisionTrace = products.DecisionTrace
type StructuralDecisionTrace = products.StructuralDecisionTrace
type GraphVersion = products.GraphVersion
type StructuralDriftMetric = products.StructuralDriftMetric
type RollbackEvent = products.RollbackEvent
type ModelSnapshot = products.ModelSnapshot
type PolicyEvalSnapshot = products.PolicyEvalSnapshot
type ChainSignature = products.ChainSignature
type ChainPrior = products.ChainPrior
type UserCompletedUnit = products.UserCompletedUnit
type TeachingPattern = products.TeachingPattern
type ActivityVariantStat = products.ActivityVariantStat
type LearningNodeDoc = products.LearningNodeDoc
type LearningNodeDocRevision = products.LearningNodeDocRevision
type LearningNodeFigure = products.LearningNodeFigure
type LearningNodeVideo = products.LearningNodeVideo
type LearningDocGenerationRun = products.LearningDocGenerationRun
type LearningNodeDocVariant = products.LearningNodeDocVariant
type LearningNodeDocBlueprint = products.LearningNodeDocBlueprint
type UserDocSignalSnapshot = products.UserDocSignalSnapshot
type DocRetrievalPack = products.DocRetrievalPack
type DocGenerationTrace = products.DocGenerationTrace
type DocConstraintReport = products.DocConstraintReport
type DocProbe = products.DocProbe
type DocProbeOutcome = products.DocProbeOutcome
type DocVariantExposure = products.DocVariantExposure
type DocVariantOutcome = products.DocVariantOutcome
type LearningDrillInstance = products.LearningDrillInstance
type LearningArtifact = products.LearningArtifact
type LibraryTaxonomyNode = products.LibraryTaxonomyNode
type LibraryTaxonomyEdge = products.LibraryTaxonomyEdge
type LibraryTaxonomyMembership = products.LibraryTaxonomyMembership
type LibraryTaxonomyState = products.LibraryTaxonomyState
type LibraryTaxonomySnapshot = products.LibraryTaxonomySnapshot
type LibraryPathEmbedding = products.LibraryPathEmbedding

type ChatThread = chat.ChatThread
type ChatMessage = chat.ChatMessage
type ChatThreadState = chat.ChatThreadState
type ChatSummaryNode = chat.ChatSummaryNode
type ChatMemoryItem = chat.ChatMemoryItem
type ChatEntity = chat.ChatEntity
type ChatEdge = chat.ChatEdge
type ChatClaim = chat.ChatClaim
type ChatDoc = chat.ChatDoc
type ChatTurn = chat.ChatTurn

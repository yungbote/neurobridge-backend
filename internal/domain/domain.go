package domain

import (
	"github.com/yungbote/neurobridge-backend/internal/domain/auth"
	"github.com/yungbote/neurobridge-backend/internal/domain/chat"
	"github.com/yungbote/neurobridge-backend/internal/domain/jobs"
	"github.com/yungbote/neurobridge-backend/internal/domain/learning/core"
	"github.com/yungbote/neurobridge-backend/internal/domain/learning/joins"
	"github.com/yungbote/neurobridge-backend/internal/domain/learning/legacy_course"
	"github.com/yungbote/neurobridge-backend/internal/domain/learning/personalization"
	"github.com/yungbote/neurobridge-backend/internal/domain/learning/products"
	"github.com/yungbote/neurobridge-backend/internal/domain/materials"
	"github.com/yungbote/neurobridge-backend/internal/domain/user"
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

	EventFeedbackThumbsUp     = personalization.EventFeedbackThumbsUp
	EventFeedbackThumbsDown   = personalization.EventFeedbackThumbsDown
	EventFeedbackTooEasy      = personalization.EventFeedbackTooEasy
	EventFeedbackTooHard      = personalization.EventFeedbackTooHard
	EventFeedbackConfusing    = personalization.EventFeedbackConfusing
	EventFeedbackLovedDiagram = personalization.EventFeedbackLovedDiagram
	EventFeedbackWantExamples = personalization.EventFeedbackWantExamples

	EventClientError = personalization.EventClientError
	EventClientPerf  = personalization.EventClientPerf
)

type User = user.User
type UserProfileVector = user.UserProfileVector
type UserToken = auth.UserToken
type UserIdentity = auth.UserIdentity
type OAuthNonce = auth.OAuthNonce

type Asset = materials.Asset
type MaterialSet = materials.MaterialSet
type MaterialFile = materials.MaterialFile
type MaterialChunk = materials.MaterialChunk
type MaterialAsset = materials.MaterialAsset
type MaterialSetSummary = materials.MaterialSetSummary
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
type UserStylePreference = personalization.UserStylePreference
type UserEvent = personalization.UserEvent
type UserEventCursor = personalization.UserEventCursor
type UserProgressionEvent = personalization.UserProgressionEvent

type Course = legacy_course.Course
type CourseModule = legacy_course.CourseModule
type CourseTag = legacy_course.CourseTag
type CourseBlueprint = legacy_course.CourseBlueprint

type Lesson = legacy_course.Lesson

type QuizQuestion = legacy_course.QuizQuestion
type QuizAttempt = legacy_course.QuizAttempt

type StyleSpec = legacy_course.StyleSpec
type LessonBlock = legacy_course.LessonBlock
type LessonContentV1 = legacy_course.LessonContentV1

type Concept = core.Concept
type Activity = core.Activity
type ActivityVariant = core.ActivityVariant
type ActivityConcept = joins.ActivityConcept
type ActivityCitation = joins.ActivityCitation

type Path = core.Path
type PathNode = core.PathNode
type PathNodeActivity = joins.PathNodeActivity

type ConceptEvidence = products.ConceptEvidence
type ConceptEdge = products.ConceptEdge
type ConceptCluster = products.ConceptCluster
type ConceptClusterMember = products.ConceptClusterMember

type UserLibraryIndex = products.UserLibraryIndex
type CohortPrior = products.CohortPrior
type DecisionTrace = products.DecisionTrace
type ChainSignature = products.ChainSignature
type ChainPrior = products.ChainPrior
type UserCompletedUnit = products.UserCompletedUnit
type TeachingPattern = products.TeachingPattern
type ActivityVariantStat = products.ActivityVariantStat
type LearningNodeDoc = products.LearningNodeDoc
type LearningNodeFigure = products.LearningNodeFigure
type LearningNodeVideo = products.LearningNodeVideo
type LearningDocGenerationRun = products.LearningDocGenerationRun
type LearningDrillInstance = products.LearningDrillInstance

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

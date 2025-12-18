package domain

import (
	"github.com/yungbote/neurobridge-backend/internal/domain/auth"
	"github.com/yungbote/neurobridge-backend/internal/domain/jobs"
	"github.com/yungbote/neurobridge-backend/internal/domain/learning"
	"github.com/yungbote/neurobridge-backend/internal/domain/materials"
	"github.com/yungbote/neurobridge-backend/internal/domain/user"
)

const (
	EventSessionStarted = learning.EventSessionStarted
	EventSessionEnded   = learning.EventSessionEnded

	EventPathOpened     = learning.EventPathOpened
	EventPathClosed     = learning.EventPathClosed
	EventNodeOpened     = learning.EventNodeOpened
	EventNodeClosed     = learning.EventNodeClosed
	EventActivityOpened = learning.EventActivityOpened
	EventActivityClosed = learning.EventActivityClosed

	EventActivityStarted   = learning.EventActivityStarted
	EventActivityCompleted = learning.EventActivityCompleted
	EventActivityAbandoned = learning.EventActivityAbandoned

	EventScrollDepth     = learning.EventScrollDepth
	EventBlockViewed     = learning.EventBlockViewed
	EventTextSelected    = learning.EventTextSelected
	EventNoteCreated     = learning.EventNoteCreated
	EventBookmarkCreated = learning.EventBookmarkCreated

	EventVideoPlayed    = learning.EventVideoPlayed
	EventVideoPaused    = learning.EventVideoPaused
	EventVideoSeeked    = learning.EventVideoSeeked
	EventAudoPlayed     = learning.EventAudoPlayed
	EVentDiagramToggled = learning.EVentDiagramToggled

	EventQuizStarted      = learning.EventQuizStarted
	EventQuestionAnswered = learning.EventQuestionAnswered
	EventQuizCompleted    = learning.EventQuizCompleted

	EventHintUsed          = learning.EventHintUsed
	EventExplanationOpened = learning.EventExplanationOpened

	EventFeedbackThumbsUp     = learning.EventFeedbackThumbsUp
	EventFeedbackThumbsDown   = learning.EventFeedbackThumbsDown
	EventFeedbackTooEasy      = learning.EventFeedbackTooEasy
	EventFeedbackTooHard      = learning.EventFeedbackTooHard
	EventFeedbackConfusing    = learning.EventFeedbackConfusing
	EventFeedbackLovedDiagram = learning.EventFeedbackLovedDiagram
	EventFeedbackWantExamples = learning.EventFeedbackWantExamples

	EventClientError = learning.EventClientError
	EventClientPerf  = learning.EventClientPerf
)

type User = user.User
type UserToken = auth.UserToken

type Asset = materials.Asset
type MaterialSet = materials.MaterialSet
type MaterialFile = materials.MaterialFile
type MaterialChunk = materials.MaterialChunk
type MaterialAsset = materials.MaterialAsset
type Segment = materials.Segment

func PtrFloat(v float64) *float64 { return materials.PtrFloat(v) }

type JobRun = jobs.JobRun

type LearningProfile = learning.LearningProfile
type TopicMastery = learning.TopicMastery
type TopicStylePreference = learning.TopicStylePreference
type UserConceptState = learning.UserConceptState
type UserStylePreference = learning.UserStylePreference
type UserEvent = learning.UserEvent
type UserEventCursor = learning.UserEventCursor

type Course = learning.Course
type CourseModule = learning.CourseModule
type CourseConcept = learning.CourseConcept
type CourseTag = learning.CourseTag
type CourseBlueprint = learning.CourseBlueprint

type Lesson = learning.Lesson
type LessonVariant = learning.LessonVariant
type LessonCitation = learning.LessonCitation
type LessonConcept = learning.LessonConcept
type LessonAsset = learning.LessonAsset
type LessonProgress = learning.LessonProgress

type QuizQuestion = learning.QuizQuestion
type QuizAttempt = learning.QuizAttempt

type StyleSpec = learning.StyleSpec
type LessonBlock = learning.LessonBlock
type LessonContentV1 = learning.LessonContentV1

type Concept = learning.Concept
type Activity = learning.Activity
type ActivityVariant = learning.ActivityVariant
type ActivityConcept = learning.ActivityConcept
type ActivityCitation = learning.ActivityCitation

type Path = learning.Path
type PathNode = learning.PathNode
type PathNodeActivity = learning.PathNodeActivity

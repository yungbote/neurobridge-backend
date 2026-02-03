package personalization

import (
	"time"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/domain/user"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	// Session
	EventSessionStarted = "session_started"
	EventSessionEnded   = "session_ended"
	// Navigation
	EventPathOpened     = "path_opened"
	EventPathClosed     = "path_closed"
	EventNodeOpened     = "path_node_opened"
	EventNodeClosed     = "path_node_closed"
	EventActivityOpened = "activity_opened"
	EventActivityClosed = "activity_closed"
	// Progress Lifecycle
	EventActivityStarted   = "activity_started"
	EventActivityCompleted = "activity_completed"
	EventActivityAbandoned = "activity_abandoned"
	// Reading / Content Interaction
	EventScrollDepth     = "scroll_depth"     // data: {percent, max_percent}
	EventBlockViewed     = "block_viewed"     // data: {block_id, block_kind, dwell_ms}
	EventBlockRead       = "block_read"       // data: {block_id, read_credit, source}
	EventTextSelected    = "text_selected"    // data: {len, block_id}
	EventNoteCreated     = "note_created"     // data: {block_id}
	EventBookmarkCreated = "bookmark_created" // data: {block_id}
	// Media
	EventVideoPlayed    = "video_played" // data: {asset_id, position_sec}
	EventVideoPaused    = "video_paused"
	EventVideoSeeked    = "video_seeked" // data: {from_sec, to_sec}
	EventAudoPlayed     = "audio_played"
	EVentDiagramToggled = "diagram_toggled" // data: {diagram_type, on}
	// Assessment / quiz (keep even if you later generalize)
	EventQuizStarted      = "quiz_started"
	EventQuestionAnswered = "question_answered" // data: {question_id, is_correct, latency_ms, confidence}
	EventQuizCompleted    = "quiz_completed"
	// Hints / help
	EventHintUsed          = "hint_used"
	EventExplanationOpened = "explanation_opened"
	// Feedback
	EventFeedbackThumbsUp     = "feedback_thumbs_up"
	EventFeedbackThumbsDown   = "feedback_thumbs_down"
	EventFeedbackTooEasy      = "feedback_too_easy"
	EventFeedbackTooHard      = "feedback_too_hard"
	EventFeedbackConfusing    = "feedback_confusing"
	EventFeedbackLovedDiagram = "feedback_loved_diagram"
	EventFeedbackWantExamples = "feedback_want_examples"

	// runtime prompt lifecycle
	EventRuntimePromptCompleted = "runtime_prompt_completed"
	EventRuntimePromptDismissed = "runtime_prompt_dismissed"
	// Diagnostics
	EventClientError = "client_error" // data: {message, stack?}
	EventClientPerf  = "client_perf"  // data: {ttfb_ms, render_ms, api_ms}
)

type UserEvent struct {
	ID     uuid.UUID  `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID uuid.UUID  `gorm:"type:uuid;not null;index;index:idx_user_client_event,unique,priority:1" json:"user_id"`
	User   *user.User `gorm:"constraint:OnDelete:CASCADE;foreignKey:UserID;references:ID" json:"user,omitempty"`
	// Frontend-provided idempotency key (required; backend will generate if missing)
	ClientEventID string `gorm:"column:client_event_id;not null;index:idx_user_client_event,unique,priority:2" json:"client_event_id"`
	// When the action happened (client clock). CreatedAt is server receive time.
	OccurredAt time.Time `gorm:"column:occurred_at;not null;index" json:"occurred_at"`
	// Correlate to a session (your UserToken.ID is perfect)
	SessionID uuid.UUID `gorm:"type:uuid;column:session_id;index" json:"session_id"`
	// New model pointers (queryable)
	PathID          *uuid.UUID `gorm:"type:uuid;column:path_id;index" json:"path_id,omitempty"`
	PathNodeID      *uuid.UUID `gorm:"type:uuid;column:path_node_id;index" json:"path_node_id,omitempty"`
	ActivityID      *uuid.UUID `gorm:"type:uuid;column:activity_id;index" json:"activity_id,omitempty"`
	ActivityVariant string     `gorm:"column:activity_variant;index" json:"activity_variant,omitempty"`
	Modality        string     `gorm:"column:modality;index" json:"modality,omitempty"`
	// Optional primary concept shortcut (full list can still be in Data.concept_ids)
	ConceptID *uuid.UUID     `gorm:"type:uuid;column:concept_id;index" json:"concept_id,omitempty"`
	Type      string         `gorm:"column:type;not null;index" json:"type"`
	Data      datatypes.JSON `gorm:"type:jsonb;column:data" json:"data"`
	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserEvent) TableName() string { return "user_event" }

type UserEventCursor struct {
	ID       uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID   uuid.UUID `gorm:"type:uuid;not null;index:idx_user_event_cursor,unique,priority:1" json:"user_id"`
	Consumer string    `gorm:"column:consumer;not null;index:idx_user_event_cursor,unique,priority:2" json:"consumer"`
	// Watermark (monotonic cursor)
	LastCreatedAt *time.Time     `gorm:"column:last_created_at;index" json:"last_created_at,omitempty"`
	LastEventID   *uuid.UUID     `gorm:"type:uuid;column:last_event_id" json:"last_event_id,omitempty"`
	UpdatedAt     time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserEventCursor) TableName() string { return "user_event_cursor" }

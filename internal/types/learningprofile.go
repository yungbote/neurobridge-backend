package types

import (
  "time"
  "github.com/google/uuid"
  "gorm.io/datatypes"
  "gorm.io/gorm"
)

type LearningProfile struct {
  gorm.Model
  ID              uuid.UUID       `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
  UserID          uuid.UUID       `gorm:"type:uuid;not nul;uniqueIndex" json:"user_id"`
  User            *User           `gorm:"constraint:OnDelete:CASCADE;foreignKey:UserID;references:ID" json:"user,omitempty"`
  Diagnoses       datatypes.JSON  `gorm:"type:jsonb;column:diagnoses" json:"diagnoses"`
  Accomodations   datatypes.JSON  `gorm:"type:jsonb;column:accommodations" json:"accomodations"`
  Constraints     datatypes.JSON  `gorm:"type:jsonb;column:constraints" json:"constraints"`
  Preferences     datatypes.JSON  `gorm:"type:jsonb;column:preferences" json:"preferences"`
  Notes           string          `gorm:"column:notes" json:"notes"`
  CreatedAt       time.Time       `gorm:"not null;default:now()" json:"created_at"`
  UpdatedAt       time.Time       `gorm:"not null;default:now()" json:"updated_at"`
}

func (LearningProfile) TableName() string {
  return "learning_profile"
}

type TopicMastery struct {
  gorm.Model
  ID              uuid.UUID       `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
  UserID          uuid.UUID       `gorm:"type:uuid;not null;index" json:"user_id"`
  User            *User           `gorm:"constraint:OnDelete:CASCADE;foreignKey:UserID;references:ID" json:"user,omitempty"`
  Topic           string          `gorm:"column:topic;not null;index:idx_user_topic,unique" json:"topic"`
  Mastery         float64         `gorm:"column:mastery;not null" json:"mastery"`
  Metadata        datatypes.JSON  `gorm:"type:jsonb;column:metadata" json:"metadata"`
  LastUpdate      time.Time       `gorm:"column:last_update;not null;default:now()" json:"last_update"`
  CreatedAt       time.Time       `gorm:"not null;default:now()" json:"created_at"`
  UpdatedAt       time.Time       `gorm:"not null;default:now()" json:"updated_at"`
}

func (TopicMastery) TableName() string {
  return "topic_mastery"
}

type LessonProgress struct {
  gorm.Model
  ID              uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
  UserID          uuid.UUID `gorm:"type:uuid;not null;index:idx_user_lesson,unique" json:"user_id"`
  User            *User     `gorm:"constraint:OnDelete:CASCADE;foreignKey:UserID;references:ID" json:"user,omitempty"`
  LessonID        uuid.UUID `gorm:"type:uuid;not null;index:idx_user_lesson,unique" json:"lesson_id"`
  Lesson          *Lesson   `gorm:"constraint:OnDelete:CASCADE;foreignKey:LessonID;references:ID" json:"lesson,omitempty"`
  Status          string    `gorm:"column:status;not null;default:'not_started'" json:"status"` // not_started | in_progress | completed
  LastOpenedAt    *time.Time `gorm:"column:last_opened_at" json:"last_opened_at,omitempty"`
  CompletedAt     *time.Time `gorm:"column:completed_at" json:"completed_at,omitempty"`
  TimeSpentSeconds int       `gorm:"column:time_spent_seconds;not null;default:0" json:"time_spent_seconds"`
  Metadata        datatypes.JSON `gorm:"type:jsonb;column:metadata" json:"metadata"`
  CreatedAt       time.Time      `gorm:"not null;default:now()" json:"created_at"`
  UpdatedAt       time.Time      `gorm:"not null;default:now()" json:"updated_at"`
}

func (LessonProgress) TableName() string {
  return "lesson_progress"
}

type QuizAttempt struct {
  gorm.Model
  ID          uuid.UUID      `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
  UserID      uuid.UUID      `gorm:"type:uuid;not null;index" json:"user_id"`
  User        *User          `gorm:"constraint:OnDelete:CASCADE;foreignKey:UserID;references:ID" json:"user,omitempty"`
  LessonID    uuid.UUID      `gorm:"type:uuid;not null;index" json:"lesson_id"`
  Lesson      *Lesson        `gorm:"constraint:OnDelete:CASCADE;foreignKey:LessonID;references:ID" json:"lesson,omitempty"`
  QuestionID  uuid.UUID      `gorm:"type:uuid;not null;index" json:"question_id"`
  Question    *QuizQuestion  `gorm:"constraint:OnDelete:CASCADE;foreignKey:QuestionID;references:ID" json:"question,omitempty"`
  IsCorrect   bool           `gorm:"column:is_correct;not null" json:"is_correct"`
  Answer      datatypes.JSON `gorm:"type:jsonb;column:answer" json:"answer"`
  Metadata    datatypes.JSON `gorm:"type:jsonb;column:metadata" json:"metadata"`
  CreatedAt   time.Time      `gorm:"not null;default:now()" json:"created_at"`
  UpdatedAt   time.Time      `gorm:"not null;default:now()" json:"updated_at"`
}

func (QuizAttempt) TableName() string {
  return "quiz_attempt"
}


type UserEvent struct {
  gorm.Model
  ID        uuid.UUID      `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
  UserID    uuid.UUID      `gorm:"type:uuid;not null;index" json:"user_id"`
  User      *User          `gorm:"constraint:OnDelete:CASCADE;foreignKey:UserID;references:ID" json:"user,omitempty"`
  CourseID  *uuid.UUID     `gorm:"type:uuid;index" json:"course_id,omitempty"`
  Course    *Course        `gorm:"constraint:OnDelete:SET NULL;foreignKey:CourseID;references:ID" json:"course,omitempty"`
  LessonID  *uuid.UUID     `gorm:"type:uuid;index" json:"lesson_id,omitempty"`
  Lesson    *Lesson        `gorm:"constraint:OnDelete:SET NULL;foreignKey:LessonID;references:ID" json:"lesson,omitempty"`
  Type      string         `gorm:"column:type;not null;index" json:"type"` // "lesson_opened","hint_requested","content_too_hard","content_too_easy","review_requested", etc.
  Data      datatypes.JSON `gorm:"type:jsonb;column:data" json:"data"`
  CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
  UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
}

func (UserEvent) TableName() string {
  return "user_event"
}

type TopicStylePreference struct {
  gorm.Model
  ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
  UserID    uuid.UUID `gorm:"type:uuid;not null;index:idx_user_topic_style,unique" json:"user_id"`
  Topic     string    `gorm:"not null;index:idx_user_topic_style,unique" json:"topic"`
  Style     string    `gorm:"not null;index:idx_user_topic_style,unique" json:"style"` // "diagram_first" | "step_by_step" | "concise" | "formal" | "analogy"
  Score     float64   `gorm:"not null;default:0" json:"score"` // can be -1..+1
  N         int       `gorm:"not null;default:0" json:"n"`
  UpdatedAt time.Time `gorm:"not null;default:now()" json:"updated_at"`
}

func (TopicStylePreference) TableName() string {
  return "topic_style_preference"
}











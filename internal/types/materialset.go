package types

import (
  "time"
  "github.com/google/uuid"
  "gorm.io/datatypes"
  "gorm.io/gorm"
)

type MaterialSet struct {
  gorm.Model
  ID            uuid.UUID       `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
  UserID        uuid.UUID       `gorm:"type:uuid;not null;index" json:"user_id"`
  User          *User           `gorm:"constraint:OnDelete:CASCADE;foreignKey:UserID;references:ID" json:"user,omitempty"`
  Title         string          `gorm:"column:title" json:"title"`
  Description   string          `gorm:"column:description" json:"description"`
  Status        string          `gorm:"column:status;not null;default:'pending'" json:"status"`
  CreatedAt     time.Time       `gorm:"not null;default:now()" json:"created_at"`
  UpdatedAt     time.Time       `gorm:"not null;default:now()" json:"updated_at"`
}

func (MaterialSet) TableName() string {
  return "material_set"
}

type MaterialFile struct {
  gorm.Model
  ID            uuid.UUID       `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
  MaterialSetID uuid.UUID       `gorm:"type:uuid;not null;index" json:"material_set_id"`
  MaterialSet   *MaterialSet    `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialSetID;references:ID" json:"material_set,omitempty"`
  OriginalName  string          `gorm:"column:original_name;not null" json:"original_name"`
  MimeType      string          `gorm:"column:mime_type" json:"mime_type"`
  SizeBytes     int64           `gorm:"column:size_bytes" json:"size_bytes"`
  StorageKey    string          `gorm:"column:storage_key;not null" json:"storage_key"`
  FileURL       string          `gorm:"column:file_url" json:"file_url"`
  Status        string          `gorm:"column:status;not null;default: 'uploaded'" json:"status"`
  AIType        string          `gorm:"column:ai_type" json:"ai_type"`
  AITopics      datatypes.JSON  `gorm:"column:ai_topics;type:jsonb" json:"ai_topics"`
  CreatedAt     time.Time       `gorm:"not null;default:now()" json:"created_at"`
  UpdatedAt     time.Time       `gorm:"not null;default:now()" json:"updated_at"`
}

func (MaterialFile) TableName() string {
  return "material_file"
}


type Course struct {
  gorm.Model
  ID            uuid.UUID       `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
  UserID        uuid.UUID       `gorm:"type:uuid;not null;index" json:"user_id"`
  User          *User           `gorm:"constraint:OnDelete:CASCADE;foreignKey:UserID;references:ID" json:"user,omitempty"`
  MaterialSetID *uuid.UUID      `gorm:"type:uuid;index" json:"material_set_id,omitempty"`
  MaterialSet   *MaterialSet    `gorm:"constraint:OnDelete:SET NULL;foreignKey:MaterialSetID;references:ID" json:"material_set,omitempty"`
  Title         string          `gorm:"column:title;not null" json:"title"`
  Description   string          `gorm:"column:description" json:"description"`
  Level         string          `gorm:"column:level" json:"level"`
  Subject       string          `gorm:"column:subject" json:"subject"`
  Metadata      datatypes.JSON  `gorm:"column:metadata;type:jsonb" json:"metadata"`
  Progress      int             `gorm:"column:progress;not null;default:0" json:"progress"`
  CreatedAt     time.Time       `gorm:"not null;default:now()" json:"created_at"`
  UpdatedAt     time.Time       `gorm:"not null;default:now()" json:"updated_at"`
}

func (Course) TableName() string {
  return "course"
}

type CourseModule struct {
  gorm.Model
  ID            uuid.UUID       `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
  CourseID      uuid.UUID       `gorm:"type:uuid;not null;index" json:"course_id"`
  Course        *Course         `gorm:"constraint:OnDelete:CASCADE;foreignKey:CourseID;references:ID" json:"course,omitempty"`
  Index         int             `gorm:"column:index;not null" json:"index"`
  Title         string          `gorm:"column:title;not null" json:"title"`
  Description   string          `gorm:"column:description" json:"description"`
  Metadata      datatypes.JSON  `gorm:"column:metadata;type:jsonb" json:"metadata"`
  CreatedAt     time.Time       `gorm:"not null;default:now()" json:"created_at"`
  UpdatedAt     time.Time       `gorm:"not null;default:now()" json:"updated_at"`
}

func (CourseModule) TableName() string {
  return "course_module"
}

type Lesson struct {
  gorm.Model
  ID                uuid.UUID       `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
  ModuleID          uuid.UUID       `gorm:"type:uuid;not null;index" json:"module_id"`
  Module            *CourseModule   `gorm:"constraint:OnDelete:CASCADE;foreignKey:ModuleID;references:ID" json:"module,omitempty"`
  Index             int             `gorm:"column:index;not null" json:"index"`

  Title             string          `gorm:"column:title;not null" json:"title"`
  Kind              string          `gorm:"column:kind;not null;default:'reading'" json:"kind"`

  ContentMD         string          `gorm:"column:content_md" json:"content_md"`
  ContentJSON       datatypes.JSON  `gorm:"column:content_json;type:jsonb" json:"content_json"`
  EstimatedMinutes  int             `gorm:"column:estimated_minutes" json:"estimated_minutes"`
  Metadata          datatypes.JSON  `gorm:"column:metadata;type:jsonb" json:"metadata"`
  CreatedAt         time.Time       `gorm:"not null;default:now()" json:"created_at"`
  UpdatedAt         time.Time       `gorm:"not null;default:now()" json:"updated_at"`
}

func (Lesson) TableName() string {
  return "lesson"
}


type QuizQuestion struct {
  gorm.Model
  ID                uuid.UUID       `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
  LessonID          uuid.UUID       `gorm:"type:uuid;not null;index" json:"lesson_id"`
  Lesson            *Lesson         `gorm:"constraint:OnDelete:CASCADE;foreignKey:LessonID;references:ID" json:"lesson,omitempty"`
  Index             int             `gorm:"column:index;not null" json:"index"`
  Type              string          `gorm:"column:type;not null" json:"type"`
  PromptMD          string          `gorm:"column:prompt_md;not null" json:"prompt_md"`
  Options           datatypes.JSON  `gorm:"column:options;type:jsonb" json:"options"`
  CorrectAnswer     datatypes.JSON  `gorm:"column:correct_answer;type:jsonb" json:"correct_answer"`
  ExplanationMD     string          `gorm:"column:explanation_md" json:"explanation_md"`
  Metadata          datatypes.JSON  `gorm:"column:metadata;type:jsonb" json:"metadata"`
  CreatedAt         time.Time       `gorm:"not null;default:now()" json:"created_at"`
  UpdatedAt         time.Time       `gorm:"not null;default:now()" json:"updated_at"`
}

func (QuizQuestion) TableName() string {
  return "quiz_question"
}

type CourseBlueprint struct {
  gorm.Model
  ID                uuid.UUID       `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
  MaterialSetID     uuid.UUID       `gorm:"type:uuid;not null;index" json:"material_set_id"`
  MaterialSet       *MaterialSet    `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialSetID;references:ID" json:"material_set,omitempty"`
  UserID            uuid.UUID       `gorm:"type:uuid;not null;index" json:"user_id"`
  User              *User           `gorm:"constraint:OnDelete:CASCADE;foreignKey:UserID;references:ID" json:"user,omitempty"`
  BlueprintJSON     datatypes.JSON  `gorm:"column:blueprint_json;type:jsonb;not null" json:"blueprint_json"`
  CreatedAt         time.Time       `gorm:"not null;default:now()" json:"created_at"`
  UpdatedAt         time.Time       `gorm:"not null;default:now()" json:"updated_at"`
}

func (CourseBlueprint) TableName() string {
  return "course_blueprint"
}

type LessonAsset struct {
  gorm.Model
  ID                uuid.UUID       `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
  LessonID          uuid.UUID       `gorm:"type:uuid;not null;index" json:"lesson_id"`
  Lesson            *Lesson         `gorm:"constraint:OnDelete:CASCADE;foreignKey:LessonID;references:ID" json:"lesson,omitempty"`
  Kind              string          `gorm:"column:kind;not null" json:"kind"`
  StorageKey        string          `gorm:"column:storage_key;not null" json:"storage_key"`
  Metadata          datatypes.JSON  `gorm:"column:metadata;type:jsonb" json:"metadata"`
  CreatedAt         time.Time       `gorm:"not null;default:now()" json:"created_at"`
  UpdatedAt         time.Time       `gorm:"not null;default:now()" json:"updated_at"`
}

func (LessonAsset) TableName() string {
  return "lesson_asset"
}

type CourseGenerationRun struct {
  ID            uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
  UserID        uuid.UUID      `gorm:"type:uuid;not null;index" json:"user_id"`
  MaterialSetID uuid.UUID      `gorm:"type:uuid;not null;index" json:"material_set_id"`
  CourseID      uuid.UUID      `gorm:"type:uuid;not null;index" json:"course_id"`

  Status   string `gorm:"column:status;not null;index" json:"status"` // queued|running|succeeded|failed
  Stage    string `gorm:"column:stage;not null;index" json:"stage"`   // ingest|embed|metadata|blueprint|lessons|quizzes|done
  Progress int    `gorm:"column:progress;not null;default:0" json:"progress"`

  Attempts    int        `gorm:"column:attempts;not null;default:0" json:"attempts"`
  Error       string     `gorm:"column:error" json:"error"`
  LastErrorAt *time.Time `gorm:"column:last_error_at" json:"last_error_at,omitempty"`

  // Locking/health for workers (optional but recommended)
  LockedAt    *time.Time `gorm:"column:locked_at;index" json:"locked_at,omitempty"`
  HeartbeatAt *time.Time `gorm:"column:heartbeat_at;index" json:"heartbeat_at,omitempty"`

  Metadata datatypes.JSON `gorm:"type:jsonb;column:metadata" json:"metadata"`

  CreatedAt time.Time  `gorm:"not null;default:now();index" json:"created_at"`
  UpdatedAt time.Time  `gorm:"not null;default:now();index" json:"updated_at"`
  DeletedAt *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}

func (CourseGenerationRun) TableName() string {
  return "course_generation_run"
}











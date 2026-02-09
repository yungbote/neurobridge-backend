package docgen

import (
	"strings"
)

const (
	DocBlueprintSchemaVersion        = 1
	DocSignalsSnapshotSchemaVersion  = 1
	DocRetrievalPackSchemaVersion    = 1
	DocGenerationTraceSchemaVersion  = 1
	DocConstraintReportSchemaVersion = 1
)

// DocBlueprintV1 defines the immutable constraints for a node doc.
type DocBlueprintV1 struct {
	SchemaVersion       int                     `json:"schema_version"`
	BlueprintVersion    string                  `json:"blueprint_version"`
	PathID              string                  `json:"path_id"`
	PathNodeID          string                  `json:"path_node_id"`
	Objectives          []string                `json:"objectives"`
	RequiredConceptKeys []string                `json:"required_concept_keys"`
	RequiredClaims      []DocClaimRef           `json:"required_claims"`
	OptionalSlots       []DocOptionalSlot       `json:"optional_slots"`
	AllowedFrames       []string                `json:"allowed_frames"`
	Constraints         DocBlueprintConstraints `json:"constraints"`
	CreatedAt           string                  `json:"created_at"`
}

type DocBlueprintConstraints struct {
	MinBlocks          int      `json:"min_blocks"`
	MaxBlocks          int      `json:"max_blocks"`
	MinQuickChecks     int      `json:"min_quick_checks"`
	MaxQuickChecks     int      `json:"max_quick_checks"`
	MinFlashcards      int      `json:"min_flashcards"`
	MaxFlashcards      int      `json:"max_flashcards"`
	RequiredBlockKinds []string `json:"required_block_kinds"`
	ForbiddenPhrases   []string `json:"forbidden_phrases"`
}

type DocOptionalSlot struct {
	SlotID            string   `json:"slot_id"`
	Purpose           string   `json:"purpose"`
	MinBlocks         int      `json:"min_blocks"`
	MaxBlocks         int      `json:"max_blocks"`
	AllowedBlockKinds []string `json:"allowed_block_kinds"`
	ConceptKeys       []string `json:"concept_keys"`
}

// DocSlotFill records how optional slots were filled in a generated doc.
type DocSlotFill struct {
	SlotID            string   `json:"slot_id"`
	Purpose           string   `json:"purpose,omitempty"`
	MinBlocks         int      `json:"min_blocks,omitempty"`
	MaxBlocks         int      `json:"max_blocks,omitempty"`
	AllowedBlockKinds []string `json:"allowed_block_kinds,omitempty"`
	ConceptKeys       []string `json:"concept_keys,omitempty"`
	FilledBlocks      int      `json:"filled_blocks,omitempty"`
	BlockIDs          []string `json:"block_ids,omitempty"`
	BlockKinds        []string `json:"block_kinds,omitempty"`
}

type DocClaimRef struct {
	ClaimID     string   `json:"claim_id"`
	ConceptKeys []string `json:"concept_keys"`
	CitationIDs []string `json:"citation_ids"`
	Required    bool     `json:"required"`
	Weight      float64  `json:"weight"`
}

// DocSignalsSnapshotV1 is a stable, versioned signal snapshot for doc generation.
type DocSignalsSnapshotV1 struct {
	SchemaVersion  int                   `json:"schema_version"`
	SnapshotID     string                `json:"snapshot_id"`
	PolicyVersion  string                `json:"policy_version"`
	UserID         string                `json:"user_id"`
	PathID         string                `json:"path_id"`
	PathNodeID     string                `json:"path_node_id"`
	Concepts       []ConceptSignal       `json:"concepts"`
	Misconceptions []MisconceptionSignal `json:"misconceptions"`
	FrameProfile   map[string]float64    `json:"frame_profile,omitempty"`
	Reading        ReadingProfile        `json:"reading_profile,omitempty"`
	Assessment     AssessmentProfile     `json:"assessment_profile,omitempty"`
	Fatigue        FatigueProfile        `json:"fatigue_profile,omitempty"`
	CreatedAt      string                `json:"created_at"`
}

type ConceptSignal struct {
	ConceptID            string  `json:"concept_id"`
	ConceptKey           string  `json:"concept_key"`
	Mastery              float64 `json:"mastery"`
	Confidence           float64 `json:"confidence"`
	EpistemicUncertainty float64 `json:"epistemic_uncertainty"`
	AleatoricUncertainty float64 `json:"aleatoric_uncertainty"`
	CoverageDebt         float64 `json:"coverage_debt"`
	LastUpdatedAt        string  `json:"last_updated_at"`
}

type MisconceptionSignal struct {
	ConceptID        string  `json:"concept_id"`
	MisconceptionKey string  `json:"misconception_key"`
	Confidence       float64 `json:"confidence"`
	FirstSeenAt      string  `json:"first_seen_at"`
	LastSeenAt       string  `json:"last_seen_at"`
}

type ReadingProfile struct {
	AvgDwellMs         int     `json:"avg_dwell_ms"`
	SkipRate           float64 `json:"skip_rate"`
	RereadRate         float64 `json:"reread_rate"`
	ReadDepth          float64 `json:"read_depth"`
	ProgressConfidence float64 `json:"progress_confidence"`
}

type AssessmentProfile struct {
	QuickCheckCount    int     `json:"quick_check_count"`
	QuickCheckAccuracy float64 `json:"quick_check_accuracy"`
	ActivityCount      int     `json:"activity_count"`
	ActivityAccuracy   float64 `json:"activity_accuracy"`
	HintUsageCount     int     `json:"hint_usage_count"`
}

type FatigueProfile struct {
	SessionMinutes  float64 `json:"session_minutes"`
	PromptsInWindow int     `json:"prompts_in_window"`
	FatigueScore    float64 `json:"fatigue_score"`
}

// DocRetrievalPackV1 defines the evidence inputs for doc generation.
type DocRetrievalPackV1 struct {
	SchemaVersion    int                `json:"schema_version"`
	PackID           string             `json:"pack_id"`
	BlueprintVersion string             `json:"blueprint_version"`
	PolicyVersion    string             `json:"policy_version"`
	Claims           []DocClaimEvidence `json:"claims"`
	Citations        []DocCitation      `json:"citations"`
	Deltas           []DocDelta         `json:"deltas"`
	CreatedAt        string             `json:"created_at"`
}

type DocClaimEvidence struct {
	ClaimID     string   `json:"claim_id"`
	ConceptKeys []string `json:"concept_keys"`
	Text        string   `json:"text"`
	SourceIDs   []string `json:"source_ids"`
	CitationIDs []string `json:"citation_ids"`
	Required    bool     `json:"required"`
	Confidence  float64  `json:"confidence"`
}

type DocCitation struct {
	CitationID string `json:"citation_id"`
	ChunkID    string `json:"chunk_id"`
	SourceType string `json:"source_type"`
}

type DocDelta struct {
	SourceDocID    string   `json:"source_doc_id"`
	Summary        string   `json:"summary"`
	BlocksAffected []string `json:"blocks_affected"`
}

// DocGenerationTraceV1 records generation inputs and validation output.
type DocGenerationTraceV1 struct {
	SchemaVersion    int                   `json:"schema_version"`
	TraceID          string                `json:"trace_id"`
	PolicyVersion    string                `json:"policy_version"`
	Model            string                `json:"model"`
	PromptHash       string                `json:"prompt_hash"`
	RetrievalPackID  string                `json:"retrieval_pack_id"`
	BlueprintVersion string                `json:"blueprint_version"`
	SlotFills        []DocSlotFill         `json:"slot_fills,omitempty"`
	ConstraintReport DocConstraintReportV1 `json:"constraint_report"`
	CreatedAt        string                `json:"created_at"`
}

// DocConstraintReportV1 is the authoritative constraint check result.
type DocConstraintReportV1 struct {
	SchemaVersion  int                      `json:"schema_version"`
	Passed         bool                     `json:"passed"`
	Violations     []DocConstraintViolation `json:"violations"`
	FallbackReason string                   `json:"fallback_reason"`
	CheckedAt      string                   `json:"checked_at"`
}

type DocConstraintViolation struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	BlockID  string `json:"block_id"`
}

func (b DocBlueprintV1) Validate() []string {
	var errs []string
	if b.SchemaVersion != DocBlueprintSchemaVersion {
		errs = append(errs, "invalid_schema_version")
	}
	if strings.TrimSpace(b.BlueprintVersion) == "" {
		errs = append(errs, "missing_blueprint_version")
	}
	if strings.TrimSpace(b.PathID) == "" || strings.TrimSpace(b.PathNodeID) == "" {
		errs = append(errs, "missing_path_ids")
	}
	if len(b.OptionalSlots) > 0 {
		seen := map[string]bool{}
		for _, slot := range b.OptionalSlots {
			id := strings.ToLower(strings.TrimSpace(slot.SlotID))
			if id == "" {
				errs = append(errs, "optional_slot_missing_id")
				continue
			}
			if seen[id] {
				errs = append(errs, "optional_slot_duplicate_id:"+id)
			}
			seen[id] = true
			minBlocks := slot.MinBlocks
			maxBlocks := slot.MaxBlocks
			if minBlocks < 0 {
				minBlocks = 0
			}
			if maxBlocks < 0 {
				maxBlocks = 0
			}
			if maxBlocks > 0 && minBlocks > maxBlocks {
				errs = append(errs, "optional_slot_min_gt_max:"+id)
			}
		}
	}
	return errs
}

func (s DocSignalsSnapshotV1) Validate() []string {
	var errs []string
	if s.SchemaVersion != DocSignalsSnapshotSchemaVersion {
		errs = append(errs, "invalid_schema_version")
	}
	if strings.TrimSpace(s.SnapshotID) == "" {
		errs = append(errs, "missing_snapshot_id")
	}
	if strings.TrimSpace(s.PolicyVersion) == "" {
		errs = append(errs, "missing_policy_version")
	}
	if strings.TrimSpace(s.UserID) == "" || strings.TrimSpace(s.PathID) == "" || strings.TrimSpace(s.PathNodeID) == "" {
		errs = append(errs, "missing_scope_ids")
	}
	return errs
}

func (p DocRetrievalPackV1) Validate() []string {
	var errs []string
	if p.SchemaVersion != DocRetrievalPackSchemaVersion {
		errs = append(errs, "invalid_schema_version")
	}
	if strings.TrimSpace(p.PackID) == "" {
		errs = append(errs, "missing_pack_id")
	}
	if strings.TrimSpace(p.PolicyVersion) == "" {
		errs = append(errs, "missing_policy_version")
	}
	return errs
}

func (t DocGenerationTraceV1) Validate() []string {
	var errs []string
	if t.SchemaVersion != DocGenerationTraceSchemaVersion {
		errs = append(errs, "invalid_schema_version")
	}
	if strings.TrimSpace(t.TraceID) == "" {
		errs = append(errs, "missing_trace_id")
	}
	if strings.TrimSpace(t.PolicyVersion) == "" {
		errs = append(errs, "missing_policy_version")
	}
	if strings.TrimSpace(t.RetrievalPackID) == "" {
		errs = append(errs, "missing_retrieval_pack_id")
	}
	return errs
}

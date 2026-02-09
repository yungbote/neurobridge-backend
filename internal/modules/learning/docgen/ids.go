package docgen

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func hashJSON(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ComputeSnapshotID returns a deterministic id that excludes volatile fields.
func ComputeSnapshotID(s DocSignalsSnapshotV1) string {
	return hashJSON(struct {
		SchemaVersion  int
		PolicyVersion  string
		UserID         string
		PathID         string
		PathNodeID     string
		Concepts       []ConceptSignal
		Misconceptions []MisconceptionSignal
		FrameProfile   map[string]float64
		Reading        ReadingProfile
		Assessment     AssessmentProfile
		Fatigue        FatigueProfile
	}{
		SchemaVersion:  s.SchemaVersion,
		PolicyVersion:  s.PolicyVersion,
		UserID:         s.UserID,
		PathID:         s.PathID,
		PathNodeID:     s.PathNodeID,
		Concepts:       s.Concepts,
		Misconceptions: s.Misconceptions,
		FrameProfile:   s.FrameProfile,
		Reading:        s.Reading,
		Assessment:     s.Assessment,
		Fatigue:        s.Fatigue,
	})
}

// ComputeRetrievalPackID returns a deterministic id that excludes volatile fields.
func ComputeRetrievalPackID(p DocRetrievalPackV1) string {
	return hashJSON(struct {
		SchemaVersion    int
		BlueprintVersion string
		PolicyVersion    string
		Claims           []DocClaimEvidence
		Citations        []DocCitation
		Deltas           []DocDelta
	}{
		SchemaVersion:    p.SchemaVersion,
		BlueprintVersion: p.BlueprintVersion,
		PolicyVersion:    p.PolicyVersion,
		Claims:           p.Claims,
		Citations:        p.Citations,
		Deltas:           p.Deltas,
	})
}

// ComputeTraceID returns a deterministic id for a generation trace.
func ComputeTraceID(t DocGenerationTraceV1) string {
	return hashJSON(struct {
		SchemaVersion    int
		PolicyVersion    string
		Model            string
		PromptHash       string
		RetrievalPackID  string
		BlueprintVersion string
		SlotFills        []DocSlotFill
		ConstraintReport DocConstraintReportV1
	}{
		SchemaVersion:    t.SchemaVersion,
		PolicyVersion:    t.PolicyVersion,
		Model:            t.Model,
		PromptHash:       t.PromptHash,
		RetrievalPackID:  t.RetrievalPackID,
		BlueprintVersion: t.BlueprintVersion,
		SlotFills:        t.SlotFills,
		ConstraintReport: t.ConstraintReport,
	})
}

func ComputeConstraintReportID(r DocConstraintReportV1) string {
	return hashJSON(struct {
		SchemaVersion  int
		Passed         bool
		Violations     []DocConstraintViolation
		FallbackReason string
	}{
		SchemaVersion:  r.SchemaVersion,
		Passed:         r.Passed,
		Violations:     r.Violations,
		FallbackReason: r.FallbackReason,
	})
}

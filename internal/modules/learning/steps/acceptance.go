package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

type AcceptanceMetrics struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	PathID        uuid.UUID

	FileCount    int
	PageCount    int
	SectionCount int
	ChunkCount   int
	ConceptCount int
	EdgeCount    int
	NodeCount    int

	UnitCount   int
	LessonCount int

	UncoveredConcepts int
	CoverageRatio     float64

	PromptSizeErrors int
	PromptErrorHints []string
}

type AcceptanceThresholds struct {
	LargePageThreshold    int
	LargeFileThreshold    int
	MinConceptsLarge      int
	MinConceptsPerPage    float64
	MinNodesPerConcept    float64
	MaxUncoveredRatio     float64
	SmallPageThreshold    int
	MinUnitsSmall         int
	MinLessonsSmall       int
	MinNodesSmall         int
	MaxPromptSizeFailures int
}

type AcceptanceCheck struct {
	ID      string
	Passed  bool
	Details string
	Metrics map[string]any
}

type AcceptanceResult struct {
	PathID   uuid.UUID
	Passed   bool
	Checks   []AcceptanceCheck
	Warnings []string
	Metrics  AcceptanceMetrics
}

func DefaultAcceptanceThresholds() AcceptanceThresholds {
	return AcceptanceThresholds{
		LargePageThreshold:    200,
		LargeFileThreshold:    6,
		MinConceptsLarge:      40,
		MinConceptsPerPage:    0.08,
		MinNodesPerConcept:    0.2,
		MaxUncoveredRatio:     0.25,
		SmallPageThreshold:    40,
		MinUnitsSmall:         1,
		MinLessonsSmall:       1,
		MinNodesSmall:         1,
		MaxPromptSizeFailures: 0,
	}
}

func ComputeAcceptanceMetrics(ctx context.Context, db *gorm.DB, pathID uuid.UUID) (AcceptanceMetrics, error) {
	out := AcceptanceMetrics{PathID: pathID}
	if db == nil {
		return out, fmt.Errorf("acceptance: missing db")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if pathID == uuid.Nil {
		return out, fmt.Errorf("acceptance: missing path_id")
	}

	var path types.Path
	if err := db.WithContext(ctx).First(&path, "id = ?", pathID).Error; err != nil {
		return out, err
	}
	if path.MaterialSetID != nil {
		out.MaterialSetID = *path.MaterialSetID
	}
	if path.UserID != nil {
		out.OwnerUserID = *path.UserID
	}

	if out.MaterialSetID != uuid.Nil {
		signals := loadAdaptiveSignals(ctx, db, out.MaterialSetID, pathID)
		out.FileCount = signals.FileCount
		out.PageCount = signals.PageCount
		out.SectionCount = signals.SectionCount
		out.ChunkCount = signals.ChunkCount
		out.ConceptCount = signals.ConceptCount
		out.EdgeCount = signals.EdgeCount
		out.NodeCount = signals.NodeCount
	}

	var unitCount int64
	_ = db.WithContext(ctx).
		Model(&types.PathNode{}).
		Where("path_id = ? AND parent_node_id IS NULL", pathID).
		Count(&unitCount).Error
	out.UnitCount = int(unitCount)

	var lessonCount int64
	_ = db.WithContext(ctx).
		Model(&types.PathNode{}).
		Where("path_id = ? AND parent_node_id IS NOT NULL", pathID).
		Count(&lessonCount).Error
	out.LessonCount = int(lessonCount)

	out.UncoveredConcepts = countAuditUncoveredConcepts(path.Metadata)
	if out.ConceptCount > 0 {
		uncoveredRatio := float64(out.UncoveredConcepts) / float64(out.ConceptCount)
		out.CoverageRatio = clamp01(1.0 - uncoveredRatio)
	}

	out.PromptSizeErrors, out.PromptErrorHints = countPromptSizeFailures(ctx, db, out.OwnerUserID, pathID)
	return out, nil
}

func EvaluateAcceptance(metrics AcceptanceMetrics, thresholds AcceptanceThresholds) AcceptanceResult {
	res := AcceptanceResult{PathID: metrics.PathID, Metrics: metrics}
	checks := make([]AcceptanceCheck, 0, 6)

	largeSet := metrics.PageCount >= thresholds.LargePageThreshold || metrics.FileCount >= thresholds.LargeFileThreshold
	if largeSet {
		minByPages := int(math.Ceil(float64(metrics.PageCount) * thresholds.MinConceptsPerPage))
		required := thresholds.MinConceptsLarge
		if minByPages > required {
			required = minByPages
		}
		pass := metrics.ConceptCount >= required
		checks = append(checks, AcceptanceCheck{
			ID:      "large_set_concepts",
			Passed:  pass,
			Details: fmt.Sprintf("concepts=%d required=%d", metrics.ConceptCount, required),
			Metrics: map[string]any{"concept_count": metrics.ConceptCount, "required": required},
		})
	}

	if metrics.ConceptCount > 0 {
		requiredNodes := int(math.Ceil(float64(metrics.ConceptCount) * thresholds.MinNodesPerConcept))
		pass := metrics.NodeCount >= requiredNodes
		checks = append(checks, AcceptanceCheck{
			ID:      "nodes_scale_with_concepts",
			Passed:  pass,
			Details: fmt.Sprintf("nodes=%d required=%d", metrics.NodeCount, requiredNodes),
			Metrics: map[string]any{"node_count": metrics.NodeCount, "required": requiredNodes},
		})
	}

	if metrics.ConceptCount > 0 {
		uncoveredRatio := 0.0
		if metrics.ConceptCount > 0 {
			uncoveredRatio = float64(metrics.UncoveredConcepts) / float64(metrics.ConceptCount)
		}
		pass := uncoveredRatio <= thresholds.MaxUncoveredRatio
		checks = append(checks, AcceptanceCheck{
			ID:      "coverage_ratio_stable",
			Passed:  pass,
			Details: fmt.Sprintf("uncovered_ratio=%.2f max=%.2f", uncoveredRatio, thresholds.MaxUncoveredRatio),
			Metrics: map[string]any{"uncovered_ratio": uncoveredRatio, "max": thresholds.MaxUncoveredRatio},
		})
	}

	if metrics.PageCount > 0 && metrics.PageCount <= thresholds.SmallPageThreshold {
		lessons := metrics.LessonCount
		if lessons == 0 {
			lessons = metrics.NodeCount
		}
		units := metrics.UnitCount
		if units == 0 {
			units = metrics.NodeCount
		}
		pass := units >= thresholds.MinUnitsSmall && lessons >= thresholds.MinLessonsSmall && metrics.NodeCount >= thresholds.MinNodesSmall
		checks = append(checks, AcceptanceCheck{
			ID:      "small_set_node_counts",
			Passed:  pass,
			Details: fmt.Sprintf("units=%d lessons=%d nodes=%d", units, lessons, metrics.NodeCount),
			Metrics: map[string]any{"units": units, "lessons": lessons, "nodes": metrics.NodeCount},
		})
	}

	passPrompt := metrics.PromptSizeErrors <= thresholds.MaxPromptSizeFailures
	checks = append(checks, AcceptanceCheck{
		ID:      "prompt_size_failures",
		Passed:  passPrompt,
		Details: fmt.Sprintf("prompt_size_errors=%d max=%d", metrics.PromptSizeErrors, thresholds.MaxPromptSizeFailures),
		Metrics: map[string]any{"prompt_size_errors": metrics.PromptSizeErrors, "max": thresholds.MaxPromptSizeFailures},
	})

	res.Checks = checks
	res.Passed = true
	for _, c := range checks {
		if !c.Passed {
			res.Passed = false
			res.Warnings = append(res.Warnings, c.ID+": "+c.Details)
		}
	}
	return res
}

func CompareConceptCounts(large AcceptanceMetrics, small AcceptanceMetrics) AcceptanceCheck {
	pass := large.ConceptCount > small.ConceptCount
	return AcceptanceCheck{
		ID:      "large_vs_small_concepts",
		Passed:  pass,
		Details: fmt.Sprintf("large=%d small=%d", large.ConceptCount, small.ConceptCount),
		Metrics: map[string]any{"large": large.ConceptCount, "small": small.ConceptCount},
	}
}

func countAuditUncoveredConcepts(raw datatypes.JSON) int {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return 0
	}
	meta := map[string]any{}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return 0
	}
	audit := mapFromAny(meta["audit"])
	if audit == nil {
		return 0
	}
	coverage := mapFromAny(audit["coverage"])
	if coverage == nil {
		return 0
	}
	uncovered := stringSliceFromAny(coverage["uncovered_concept_keys"])
	return len(uncovered)
}

func countPromptSizeFailures(ctx context.Context, db *gorm.DB, ownerUserID uuid.UUID, pathID uuid.UUID) (int, []string) {
	if db == nil || ownerUserID == uuid.Nil || pathID == uuid.Nil {
		return 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	type jobRow struct {
		ID     uuid.UUID
		Error  string
		Status string
	}
	var job jobRow
	if err := db.WithContext(ctx).
		Table("job_run").
		Select("id", "error", "status").
		Where("owner_user_id = ? AND entity_type = ? AND entity_id = ? AND job_type IN ?", ownerUserID, "path", pathID, []string{"learning_build", "learning_build_progressive"}).
		Order("created_at DESC").
		Limit(1).
		Find(&job).Error; err != nil {
		return 0, nil
	}
	if job.ID == uuid.Nil {
		return 0, nil
	}

	hits := 0
	hints := make([]string, 0, 3)
	if isPromptSizeError(job.Error) {
		hits++
		hints = appendHint(hints, job.Error)
	}

	type eventRow struct {
		Message string
		Data    datatypes.JSON
	}
	var events []eventRow
	_ = db.WithContext(ctx).
		Table("job_run_event").
		Select("message", "data").
		Where("job_id = ?", job.ID).
		Find(&events).Error
	for _, ev := range events {
		if isPromptSizeError(ev.Message) {
			hits++
			hints = appendHint(hints, ev.Message)
			continue
		}
		if len(ev.Data) > 0 && isPromptSizeError(string(ev.Data)) {
			hits++
			hints = appendHint(hints, string(ev.Data))
		}
	}

	return hits, hints
}

func isPromptSizeError(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return false
	}
	needles := []string{
		"context_length_exceeded",
		"context length",
		"maximum context",
		"max context",
		"max_tokens",
		"prompt too long",
		"token limit",
		"exceeds the context window",
	}
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

func appendHint(hints []string, msg string) []string {
	if len(hints) >= 3 {
		return hints
	}
	trimmed := strings.TrimSpace(msg)
	if trimmed == "" {
		return hints
	}
	if len(trimmed) > 180 {
		trimmed = truncateUTF8(trimmed, 180)
	}
	hints = append(hints, trimmed)
	return hints
}

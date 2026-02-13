package drift

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/observability"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type ComputeDeps struct {
	DB      *gorm.DB
	Log     *logger.Logger
	Metrics interface {
		CreateMany(dbc dbctx.Context, rows []*types.StructuralDriftMetric) ([]*types.StructuralDriftMetric, error)
	}
	RollbackRepo interface {
		Create(dbc dbctx.Context, row *types.RollbackEvent) error
	}
}

type ComputeInput struct {
	GraphVersion string
	WindowHours  int
	MinSamples   int
	MaxSamples   int

	NearThresholdMargin float64

	ScoreMarginMeanWarnMin float64
	ScoreMarginMeanCritMin float64
	ScoreMarginP10WarnMin  float64
	ScoreMarginP10CritMin  float64

	NearThresholdRateWarnMax float64
	NearThresholdRateCritMax float64

	ReMergeRateWarnMax float64
	ReMergeRateCritMax float64

	EdgeConfidenceShiftWarnMax float64
	EdgeConfidenceShiftCritMax float64

	AlertOnWarn bool

	RecommendationStatus        string
	RecommendationCooldownHours int

	AllowFallbackGraphVersion bool
	DecisionTypes             []string

	TraceID string
}

type ComputeOutput struct {
	GraphVersion          string
	WindowStart           time.Time
	WindowEnd             time.Time
	MetricsWritten        int
	Alerts                []string
	RecommendationWritten bool
	RollbackEventID       uuid.UUID
	TraceID               string
}

func Compute(ctx context.Context, deps ComputeDeps, input ComputeInput) (ComputeOutput, error) {
	out := ComputeOutput{}
	if deps.DB == nil || deps.Metrics == nil {
		return out, fmt.Errorf("drift: missing deps")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if input.WindowHours <= 0 {
		input.WindowHours = 168
	}
	if input.MinSamples <= 0 {
		input.MinSamples = 50
	}
	if input.MaxSamples <= 0 {
		input.MaxSamples = 5000
	}
	if input.NearThresholdMargin <= 0 {
		input.NearThresholdMargin = 0.05
	}
	if input.RecommendationCooldownHours <= 0 {
		input.RecommendationCooldownHours = 24
	}

	graphVersion := strings.TrimSpace(input.GraphVersion)
	if graphVersion == "" && input.AllowFallbackGraphVersion {
		graphVersion = latestGraphVersion(ctx, deps.DB)
	}
	if graphVersion == "" {
		return out, fmt.Errorf("drift: missing graph_version")
	}

	now := time.Now().UTC()
	window := time.Duration(input.WindowHours) * time.Hour
	windowStart := now.Add(-window)
	out.GraphVersion = graphVersion
	out.WindowStart = windowStart
	out.WindowEnd = now
	out.TraceID = strings.TrimSpace(input.TraceID)

	traces, err := listStructuralTraces(ctx, deps.DB, graphVersion, windowStart, now, input.DecisionTypes, input.MaxSamples)
	if err != nil {
		return out, err
	}

	scoreMargins, marginSamples, nearThresholdRate, nearThresholdSamples, nearThresholdCount := analyzeCandidateMargins(traces, input.NearThresholdMargin)
	metrics := []metricResult{}
	if marginSamples > 0 && len(scoreMargins) > 0 {
		sort.Float64s(scoreMargins)
		mean := meanOf(scoreMargins)
		p10 := quantile(scoreMargins, 0.1)
		metrics = append(metrics, buildRateMetric("candidate_score_margin_mean", mean, input.ScoreMarginMeanWarnMin, input.ScoreMarginMeanCritMin, marginSamples, "min").withMeta(map[string]any{
			"p10": p10,
		}))
		metrics = append(metrics, buildRateMetric("candidate_score_margin_p10", p10, input.ScoreMarginP10WarnMin, input.ScoreMarginP10CritMin, marginSamples, "min").withMeta(map[string]any{
			"mean": mean,
		}))
	} else {
		metrics = append(metrics,
			buildRateMetric("candidate_score_margin_mean", 0, input.ScoreMarginMeanWarnMin, input.ScoreMarginMeanCritMin, 0, "min"),
			buildRateMetric("candidate_score_margin_p10", 0, input.ScoreMarginP10WarnMin, input.ScoreMarginP10CritMin, 0, "min"),
		)
	}
	metrics = append(metrics, buildRateMetric("merge_near_threshold_rate", nearThresholdRate, input.NearThresholdRateWarnMax, input.NearThresholdRateCritMax, nearThresholdSamples, "max").withMeta(map[string]any{
		"accept_count": nearThresholdSamples,
		"near_count":   nearThresholdCount,
	}))

	remergeRate, remergeSamples, remergeMeta, err := computeReMergeRate(ctx, deps.DB, windowStart, now)
	if err != nil && deps.Log != nil {
		deps.Log.Warn("drift: remerge rate compute failed", "error", err.Error())
	}
	metrics = append(metrics, buildRateMetric("remerge_rate", remergeRate, input.ReMergeRateWarnMax, input.ReMergeRateCritMax, remergeSamples, "max").withMeta(remergeMeta))

	edgeShift, edgeSamples, edgeMeta, err := computeEdgeConfidenceShift(ctx, deps.DB, windowStart, now)
	if err != nil && deps.Log != nil {
		deps.Log.Warn("drift: edge confidence shift compute failed", "error", err.Error())
	}
	metrics = append(metrics, buildRateMetric("edge_confidence_shift", edgeShift, input.EdgeConfidenceShiftWarnMax, input.EdgeConfidenceShiftCritMax, edgeSamples, "max").withMeta(edgeMeta))

	rows := make([]*types.StructuralDriftMetric, 0, len(metrics))
	for _, metric := range metrics {
		meta := metric.Meta
		if meta == nil {
			meta = map[string]any{}
		}
		meta["samples"] = metric.Samples
		meta["trace_id"] = out.TraceID
		meta["warn_threshold"] = metric.Warn
		if metric.Crit > 0 {
			meta["crit_threshold"] = metric.Crit
		}
		row := &types.StructuralDriftMetric{
			GraphVersion: graphVersion,
			MetricName:   metric.Name,
			WindowStart:  windowStart,
			WindowEnd:    now,
			Value:        metric.Value,
			Threshold:    metric.Warn,
			Status:       metric.Status,
			Metadata:     datatypes.JSON(mustJSON(meta)),
		}
		rows = append(rows, row)
	}
	if len(rows) > 0 {
		if _, err := deps.Metrics.CreateMany(dbctx.Context{Ctx: ctx}, rows); err != nil {
			return out, err
		}
	}
	out.MetricsWritten = len(rows)

	alerts := collectAlerts(metrics, input.AlertOnWarn)
	out.Alerts = alerts
	if len(alerts) > 0 {
		observability.ReportStructuralDrift(ctx, deps.Log, toAlertMetrics(metrics), map[string]any{
			"graph_version": graphVersion,
			"window_start":  windowStart.Format(time.RFC3339),
			"window_end":    now.Format(time.RFC3339),
			"trace_id":      out.TraceID,
		})
	}

	if len(alerts) > 0 && strings.TrimSpace(input.RecommendationStatus) != "" {
		if ok, id := maybeRecommendRollback(ctx, deps, graphVersion, input.RecommendationStatus, input.RecommendationCooldownHours, metrics, out.TraceID); ok {
			out.RecommendationWritten = true
			out.RollbackEventID = id
		}
	}

	return out, nil
}

type metricResult struct {
	Name    string
	Value   float64
	Warn    float64
	Crit    float64
	Status  string
	Samples int
	Meta    map[string]any
}

func (m metricResult) withMeta(meta map[string]any) metricResult {
	if len(meta) == 0 {
		return m
	}
	if m.Meta == nil {
		m.Meta = map[string]any{}
	}
	for k, v := range meta {
		m.Meta[k] = v
	}
	return m
}

func buildRateMetric(name string, value, warn, crit float64, samples int, direction string) metricResult {
	status := "insufficient"
	if samples > 0 {
		status = evalStatus(value, warn, crit, direction)
	}
	return metricResult{
		Name:    name,
		Value:   value,
		Warn:    warn,
		Crit:    crit,
		Status:  status,
		Samples: samples,
	}
}

func evalStatus(value, warn, crit float64, direction string) string {
	direction = strings.TrimSpace(strings.ToLower(direction))
	if warn <= 0 && crit <= 0 {
		return "ok"
	}
	if direction == "min" {
		if crit > 0 && value <= crit {
			return "critical"
		}
		if warn > 0 && value <= warn {
			return "warn"
		}
		return "ok"
	}
	if crit > 0 && value >= crit {
		return "critical"
	}
	if warn > 0 && value >= warn {
		return "warn"
	}
	return "ok"
}

func collectAlerts(metrics []metricResult, alertOnWarn bool) []string {
	alerts := []string{}
	for _, metric := range metrics {
		switch metric.Status {
		case "critical":
			alerts = append(alerts, metric.Name)
		case "warn":
			if alertOnWarn {
				alerts = append(alerts, metric.Name)
			}
		}
	}
	return alerts
}

func toAlertMetrics(metrics []metricResult) []observability.StructuralDriftAlertMetric {
	out := make([]observability.StructuralDriftAlertMetric, 0, len(metrics))
	for _, m := range metrics {
		out = append(out, observability.StructuralDriftAlertMetric{
			Name:      m.Name,
			Status:    m.Status,
			Value:     m.Value,
			Threshold: m.Warn,
			Meta:      m.Meta,
		})
	}
	return out
}

func listStructuralTraces(ctx context.Context, db *gorm.DB, graphVersion string, start, end time.Time, decisionTypes []string, maxSamples int) ([]*types.StructuralDecisionTrace, error) {
	if db == nil {
		return nil, nil
	}
	q := db.WithContext(ctx).Model(&types.StructuralDecisionTrace{}).
		Where("occurred_at >= ? AND occurred_at < ?", start, end)
	if strings.TrimSpace(graphVersion) != "" {
		q = q.Where("graph_version = ?", graphVersion)
	}
	if len(decisionTypes) > 0 {
		q = q.Where("decision_type IN ?", decisionTypes)
	}
	if maxSamples > 0 {
		q = q.Limit(maxSamples)
	}
	q = q.Order("occurred_at DESC")
	rows := []*types.StructuralDecisionTrace{}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func analyzeCandidateMargins(traces []*types.StructuralDecisionTrace, nearThresholdMargin float64) ([]float64, int, float64, int, int) {
	if nearThresholdMargin <= 0 {
		nearThresholdMargin = 0.05
	}
	margins := []float64{}
	sampleCount := 0
	acceptCount := 0
	nearCount := 0

	for _, tr := range traces {
		if tr == nil {
			continue
		}
		scores := extractCandidateScores(tr.Candidates)
		if len(scores) > 0 {
			sort.Slice(scores, func(i, j int) bool { return scores[i] > scores[j] })
		}
		if len(scores) >= 2 {
			margin := scores[0] - scores[1]
			if !math.IsNaN(margin) && !math.IsInf(margin, 0) {
				margins = append(margins, margin)
				sampleCount++
			}
		}

		topScore := 0.0
		if len(scores) > 0 {
			topScore = scores[0]
		} else {
			topScore = extractChosenScore(tr.Chosen)
		}
		threshold, ok := extractThreshold(tr.Thresholds)
		if !ok || threshold <= 0 || topScore <= 0 {
			continue
		}
		if topScore >= threshold {
			acceptCount++
			if (topScore - threshold) <= nearThresholdMargin {
				nearCount++
			}
		}
	}

	rate := 0.0
	if acceptCount > 0 {
		rate = float64(nearCount) / float64(acceptCount)
	}
	return margins, sampleCount, rate, acceptCount, nearCount
}

func computeReMergeRate(ctx context.Context, db *gorm.DB, start, end time.Time) (float64, int, map[string]any, error) {
	if db == nil {
		return 0, 0, nil, nil
	}
	meta := map[string]any{}
	var total int64
	if err := db.WithContext(ctx).
		Model(&types.Concept{}).
		Where("scope = ? AND updated_at >= ? AND updated_at < ?", "global", start, end).
		Count(&total).Error; err != nil {
		return 0, 0, nil, err
	}
	var remerged int64
	if err := db.WithContext(ctx).
		Model(&types.Concept{}).
		Where("scope = ? AND updated_at >= ? AND updated_at < ? AND canonical_concept_id IS NOT NULL AND created_at < ?", "global", start, end, start).
		Count(&remerged).Error; err != nil {
		return 0, int(total), nil, err
	}
	var newAliases int64
	_ = db.WithContext(ctx).
		Model(&types.Concept{}).
		Where("scope = ? AND created_at >= ? AND created_at < ? AND canonical_concept_id IS NOT NULL", "global", start, end).
		Count(&newAliases).Error
	meta["remerge_updates"] = remerged
	meta["new_aliases"] = newAliases
	rate := 0.0
	if total > 0 {
		rate = float64(remerged) / float64(total)
	}
	return rate, int(total), meta, nil
}

func computeEdgeConfidenceShift(ctx context.Context, db *gorm.DB, start, end time.Time) (float64, int, map[string]any, error) {
	if db == nil {
		return 0, 0, nil, nil
	}
	meta := map[string]any{}
	prevStart := start.Add(-(end.Sub(start)))
	prevEnd := start

	var currentMean float64
	var currentCount int64
	if err := db.WithContext(ctx).
		Model(&types.ConceptEdge{}).
		Where("created_at >= ? AND created_at < ?", start, end).
		Select("COALESCE(AVG(strength), 0)").
		Scan(&currentMean).Error; err != nil {
		return 0, 0, nil, err
	}
	_ = db.WithContext(ctx).
		Model(&types.ConceptEdge{}).
		Where("created_at >= ? AND created_at < ?", start, end).
		Count(&currentCount).Error

	var prevMean float64
	var prevCount int64
	if err := db.WithContext(ctx).
		Model(&types.ConceptEdge{}).
		Where("created_at >= ? AND created_at < ?", prevStart, prevEnd).
		Select("COALESCE(AVG(strength), 0)").
		Scan(&prevMean).Error; err != nil {
		return 0, int(currentCount), nil, err
	}
	_ = db.WithContext(ctx).
		Model(&types.ConceptEdge{}).
		Where("created_at >= ? AND created_at < ?", prevStart, prevEnd).
		Count(&prevCount).Error

	meta["current_mean"] = currentMean
	meta["current_count"] = currentCount
	meta["previous_mean"] = prevMean
	meta["previous_count"] = prevCount

	if currentCount == 0 || prevCount == 0 {
		meta["baseline_missing"] = prevCount == 0
		meta["current_missing"] = currentCount == 0
		return 0, 0, meta, nil
	}
	shift := math.Abs(currentMean - prevMean)
	return shift, int(currentCount + prevCount), meta, nil
}

func latestGraphVersion(ctx context.Context, db *gorm.DB) string {
	if db == nil {
		return ""
	}
	row := &types.GraphVersion{}
	if err := db.WithContext(ctx).
		Where("status = ?", "active").
		Order("updated_at desc").
		Limit(1).
		Find(row).Error; err == nil && row.GraphVersion != "" {
		return row.GraphVersion
	}
	row = &types.GraphVersion{}
	if err := db.WithContext(ctx).
		Order("updated_at desc").
		Limit(1).
		Find(row).Error; err == nil && row.GraphVersion != "" {
		return row.GraphVersion
	}
	return ""
}

func maybeRecommendRollback(ctx context.Context, deps ComputeDeps, graphVersion, status string, cooldownHours int, metrics []metricResult, traceID string) (bool, uuid.UUID) {
	if deps.DB == nil || deps.RollbackRepo == nil {
		return false, uuid.Nil
	}
	cutoff := time.Now().UTC().Add(-time.Duration(cooldownHours) * time.Hour)
	var count int64
	_ = deps.DB.WithContext(ctx).
		Model(&types.RollbackEvent{}).
		Where("graph_version_from = ? AND trigger = ? AND created_at >= ?", graphVersion, "structural_drift", cutoff).
		Count(&count).Error
	if count > 0 {
		return false, uuid.Nil
	}
	note := map[string]any{
		"metrics":  metricsSummary(metrics),
		"trace_id": traceID,
	}
	row := &types.RollbackEvent{
		ID:               uuid.New(),
		GraphVersionFrom: graphVersion,
		Trigger:          "structural_drift",
		Status:           status,
		Notes:            datatypes.JSON(mustJSON(note)),
	}
	if err := deps.RollbackRepo.Create(dbctx.Context{Ctx: ctx}, row); err != nil {
		return false, uuid.Nil
	}
	return true, row.ID
}

func metricsSummary(metrics []metricResult) []map[string]any {
	out := make([]map[string]any, 0, len(metrics))
	for _, m := range metrics {
		out = append(out, map[string]any{
			"name":    m.Name,
			"status":  m.Status,
			"value":   m.Value,
			"warn":    m.Warn,
			"crit":    m.Crit,
			"samples": m.Samples,
		})
	}
	return out
}

func meanOf(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func quantile(values []float64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	if q <= 0 {
		return values[0]
	}
	if q >= 1 {
		return values[len(values)-1]
	}
	pos := q * float64(len(values)-1)
	idx := int(math.Floor(pos))
	frac := pos - float64(idx)
	if idx+1 >= len(values) {
		return values[len(values)-1]
	}
	return values[idx] + (values[idx+1]-values[idx])*frac
}

func mustJSON(v any) []byte {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}

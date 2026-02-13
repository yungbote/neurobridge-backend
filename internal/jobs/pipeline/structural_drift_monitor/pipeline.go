package structural_drift_monitor

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/drift"
	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
)

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	if p.db == nil || p.metrics == nil {
		jc.Fail("deps", fmt.Errorf("missing repos"))
		return nil
	}

	cfg := drift.LoadConfigFromEnv()
	payload := jc.Payload()
	cfg.ApplyPayload(payload)
	if cfg.Disabled {
		jc.Succeed("disabled", map[string]any{"disabled": true})
		return nil
	}

	jc.Progress("scan", 5, "Computing structural drift metrics")

	traceID := ""
	if td := ctxutil.GetTraceData(jc.Ctx); td != nil {
		traceID = strings.TrimSpace(td.TraceID)
	}

	out, err := drift.Compute(jc.Ctx, drift.ComputeDeps{
		DB:           p.db,
		Log:          p.log,
		Metrics:      p.metrics,
		RollbackRepo: p.rollbackRepo,
	}, drift.ComputeInput{
		GraphVersion:                cfg.GraphVersion,
		WindowHours:                 cfg.WindowHours,
		MinSamples:                  cfg.MinSamples,
		MaxSamples:                  cfg.MaxSamples,
		NearThresholdMargin:         cfg.NearThresholdMargin,
		ScoreMarginMeanWarnMin:      cfg.ScoreMarginMeanWarnMin,
		ScoreMarginMeanCritMin:      cfg.ScoreMarginMeanCritMin,
		ScoreMarginP10WarnMin:       cfg.ScoreMarginP10WarnMin,
		ScoreMarginP10CritMin:       cfg.ScoreMarginP10CritMin,
		NearThresholdRateWarnMax:    cfg.NearThresholdRateWarnMax,
		NearThresholdRateCritMax:    cfg.NearThresholdRateCritMax,
		ReMergeRateWarnMax:          cfg.ReMergeRateWarnMax,
		ReMergeRateCritMax:          cfg.ReMergeRateCritMax,
		EdgeConfidenceShiftWarnMax:  cfg.EdgeConfidenceShiftWarnMax,
		EdgeConfidenceShiftCritMax:  cfg.EdgeConfidenceShiftCritMax,
		AlertOnWarn:                 cfg.AlertOnWarn,
		RecommendationStatus:        cfg.RecommendationStatus,
		RecommendationCooldownHours: cfg.RecommendationCooldownHours,
		TraceID:                     traceID,
		DecisionTypes:               cfg.DecisionTypes,
		AllowFallbackGraphVersion:   cfg.AllowFallbackGraphVersion,
	})
	if err != nil {
		jc.Fail("compute", err)
		return nil
	}

	result := map[string]any{
		"graph_version":          out.GraphVersion,
		"window_start":           out.WindowStart.Format(time.RFC3339),
		"window_end":             out.WindowEnd.Format(time.RFC3339),
		"metrics_written":        out.MetricsWritten,
		"alerts":                 out.Alerts,
		"recommendation_written": out.RecommendationWritten,
	}
	if out.RollbackEventID != uuid.Nil {
		result["rollback_event_id"] = out.RollbackEventID.String()
	}
	if out.TraceID != "" {
		result["trace_id"] = out.TraceID
	}

	jc.Succeed("done", result)
	return nil
}

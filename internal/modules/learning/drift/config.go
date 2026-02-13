package drift

import (
	"fmt"
	"os"
	"strings"

	"github.com/yungbote/neurobridge-backend/internal/platform/envutil"
)

type Config struct {
	Disabled bool

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
}

func LoadConfigFromEnv() Config {
	cfg := Config{
		Disabled:                    !envutil.Bool("STRUCTURAL_DRIFT_ENABLED", true),
		GraphVersion:                strings.TrimSpace(os.Getenv("STRUCTURAL_DRIFT_GRAPH_VERSION")),
		WindowHours:                 envutil.Int("STRUCTURAL_DRIFT_WINDOW_HOURS", 168),
		MinSamples:                  envutil.Int("STRUCTURAL_DRIFT_MIN_SAMPLES", 50),
		MaxSamples:                  envutil.Int("STRUCTURAL_DRIFT_MAX_SAMPLES", 5000),
		NearThresholdMargin:         envutil.Float("STRUCTURAL_DRIFT_NEAR_THRESHOLD_MARGIN", 0.05),
		ScoreMarginMeanWarnMin:      envutil.Float("STRUCTURAL_DRIFT_SCORE_MARGIN_MEAN_WARN_MIN", 0.15),
		ScoreMarginMeanCritMin:      envutil.Float("STRUCTURAL_DRIFT_SCORE_MARGIN_MEAN_CRIT_MIN", 0),
		ScoreMarginP10WarnMin:       envutil.Float("STRUCTURAL_DRIFT_SCORE_MARGIN_P10_WARN_MIN", 0.05),
		ScoreMarginP10CritMin:       envutil.Float("STRUCTURAL_DRIFT_SCORE_MARGIN_P10_CRIT_MIN", 0),
		NearThresholdRateWarnMax:    envutil.Float("STRUCTURAL_DRIFT_NEAR_THRESHOLD_RATE_WARN_MAX", 0.2),
		NearThresholdRateCritMax:    envutil.Float("STRUCTURAL_DRIFT_NEAR_THRESHOLD_RATE_CRIT_MAX", 0),
		ReMergeRateWarnMax:          envutil.Float("STRUCTURAL_DRIFT_REMERGE_RATE_WARN_MAX", 0.1),
		ReMergeRateCritMax:          envutil.Float("STRUCTURAL_DRIFT_REMERGE_RATE_CRIT_MAX", 0),
		EdgeConfidenceShiftWarnMax:  envutil.Float("STRUCTURAL_DRIFT_EDGE_CONF_SHIFT_WARN_MAX", 0.1),
		EdgeConfidenceShiftCritMax:  envutil.Float("STRUCTURAL_DRIFT_EDGE_CONF_SHIFT_CRIT_MAX", 0),
		AlertOnWarn:                 envutil.Bool("STRUCTURAL_DRIFT_ALERT_ON_WARN", true),
		RecommendationStatus:        strings.TrimSpace(os.Getenv("STRUCTURAL_DRIFT_RECOMMENDATION_STATUS")),
		RecommendationCooldownHours: envutil.Int("STRUCTURAL_DRIFT_RECOMMENDATION_COOLDOWN_HOURS", 24),
		AllowFallbackGraphVersion:   envutil.Bool("STRUCTURAL_DRIFT_ALLOW_FALLBACK_GRAPH_VERSION", true),
		DecisionTypes:               splitCSV(os.Getenv("STRUCTURAL_DRIFT_DECISION_TYPES")),
	}
	if cfg.RecommendationStatus == "" {
		cfg.RecommendationStatus = "recommended"
	}
	cfg.ensureCritDefaults()
	return cfg
}

func (c *Config) ApplyPayload(payload map[string]any) {
	if payload == nil {
		return
	}
	if v := strings.TrimSpace(stringFromAny(payload["graph_version"])); v != "" {
		c.GraphVersion = v
	}
	if v := intFromAny(payload["window_hours"], 0); v > 0 {
		c.WindowHours = v
	}
	if v := intFromAny(payload["min_samples"], 0); v > 0 {
		c.MinSamples = v
	}
	if v := intFromAny(payload["max_samples"], 0); v > 0 {
		c.MaxSamples = v
	}
	if v := floatFromAny(payload["near_threshold_margin"], 0); v > 0 {
		c.NearThresholdMargin = v
	}
	if v := floatFromAny(payload["score_margin_mean_warn_min"], 0); v > 0 {
		c.ScoreMarginMeanWarnMin = v
	}
	if v := floatFromAny(payload["score_margin_mean_crit_min"], 0); v > 0 {
		c.ScoreMarginMeanCritMin = v
	}
	if v := floatFromAny(payload["score_margin_p10_warn_min"], 0); v > 0 {
		c.ScoreMarginP10WarnMin = v
	}
	if v := floatFromAny(payload["score_margin_p10_crit_min"], 0); v > 0 {
		c.ScoreMarginP10CritMin = v
	}
	if v := floatFromAny(payload["near_threshold_rate_warn_max"], 0); v > 0 {
		c.NearThresholdRateWarnMax = v
	}
	if v := floatFromAny(payload["near_threshold_rate_crit_max"], 0); v > 0 {
		c.NearThresholdRateCritMax = v
	}
	if v := floatFromAny(payload["remerge_rate_warn_max"], 0); v > 0 {
		c.ReMergeRateWarnMax = v
	}
	if v := floatFromAny(payload["remerge_rate_crit_max"], 0); v > 0 {
		c.ReMergeRateCritMax = v
	}
	if v := floatFromAny(payload["edge_conf_shift_warn_max"], 0); v > 0 {
		c.EdgeConfidenceShiftWarnMax = v
	}
	if v := floatFromAny(payload["edge_conf_shift_crit_max"], 0); v > 0 {
		c.EdgeConfidenceShiftCritMax = v
	}
	if v := boolFromAny(payload["alert_on_warn"], c.AlertOnWarn); v != c.AlertOnWarn {
		c.AlertOnWarn = v
	}
	if v := strings.TrimSpace(stringFromAny(payload["recommendation_status"])); v != "" {
		c.RecommendationStatus = v
	}
	if v := intFromAny(payload["recommendation_cooldown_hours"], 0); v > 0 {
		c.RecommendationCooldownHours = v
	}
	if v := boolFromAny(payload["allow_fallback_graph_version"], c.AllowFallbackGraphVersion); v != c.AllowFallbackGraphVersion {
		c.AllowFallbackGraphVersion = v
	}
	if raw, ok := payload["decision_types"]; ok {
		if v := parseDecisionTypes(raw); len(v) > 0 {
			c.DecisionTypes = v
		}
	}
	if v := boolFromAny(payload["disabled"], c.Disabled); v != c.Disabled {
		c.Disabled = v
	}
	c.ensureCritDefaults()
}

func (c *Config) ensureCritDefaults() {
	c.ScoreMarginMeanCritMin = ensureCritMin(c.ScoreMarginMeanWarnMin, c.ScoreMarginMeanCritMin)
	c.ScoreMarginP10CritMin = ensureCritMin(c.ScoreMarginP10WarnMin, c.ScoreMarginP10CritMin)
	c.NearThresholdRateCritMax = ensureCritMax(c.NearThresholdRateWarnMax, c.NearThresholdRateCritMax)
	c.ReMergeRateCritMax = ensureCritMax(c.ReMergeRateWarnMax, c.ReMergeRateCritMax)
	c.EdgeConfidenceShiftCritMax = ensureCritMax(c.EdgeConfidenceShiftWarnMax, c.EdgeConfidenceShiftCritMax)
}

func ensureCritMin(warn, crit float64) float64 {
	if crit > 0 {
		return crit
	}
	if warn <= 0 {
		return 0
	}
	return warn * 0.5
}

func ensureCritMax(warn, crit float64) float64 {
	if crit > 0 {
		return crit
	}
	if warn <= 0 {
		return 0
	}
	return warn * 2
}

func splitCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseDecisionTypes(raw any) []string {
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s := strings.TrimSpace(fmt.Sprint(item))
			if s != "" && s != "<nil>" {
				out = append(out, s)
			}
		}
		return out
	default:
		return splitCSV(stringFromAny(raw))
	}
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

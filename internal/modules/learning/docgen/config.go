package docgen

import (
	"os"
	"strconv"
	"strings"
)

const (
	EnvDocPolicyVersion         = "DOC_POLICY_VERSION"
	EnvDocBlueprintVersion      = "DOC_BLUEPRINT_VERSION"
	EnvDocLookaheadDefault      = "DOC_LOOKAHEAD_DEFAULT"
	EnvDocLookaheadPath         = "DOC_LOOKAHEAD_PATH"
	EnvDocLookaheadProgram      = "DOC_LOOKAHEAD_PROGRAM"
	EnvDocProgressiveMinGap     = "DOC_PROGRESSIVE_MIN_GAP_MINUTES"
	EnvDocPrereqGateMode        = "DOC_PREREQ_GATE_MODE"
	EnvDocPrereqReadyMin        = "DOC_PREREQ_READY_MIN"
	EnvDocPrereqUncertainMin    = "DOC_PREREQ_UNCERTAIN_MIN"
	EnvDocPrereqMaxMisconReady  = "DOC_PREREQ_MAX_MISCON_READY"
	EnvDocProbeRatePerHour      = "DOC_PROBE_RATE_PER_HOUR"
	EnvDocProbeMaxPerNode       = "DOC_PROBE_MAX_PER_NODE"
	EnvDocProbeMaxPerLookahead  = "DOC_PROBE_MAX_PER_LOOKAHEAD"
	EnvDocProbeMinInfoGain      = "DOC_PROBE_MIN_INFO_GAIN"
	EnvDocProbeTestletWeight    = "DOC_PROBE_TESTLET_WEIGHT"
	EnvDocProbeMisconBoost      = "DOC_PROBE_MISCONCEPTION_BOOST"
	EnvDocProbePrereqBoost      = "DOC_PROBE_PREREQ_BOOST"
	EnvDocVariantPolicyMode     = "DOC_VARIANT_POLICY_MODE"
	EnvDocVariantPolicyKey      = "DOC_VARIANT_POLICY_KEY"
	EnvDocVariantRolloutPct     = "DOC_VARIANT_ROLLOUT_PCT"
	EnvDocVariantEvalMinAge     = "DOC_VARIANT_EVAL_MIN_AGE_MINUTES"
	EnvDocVariantEvalLimit      = "DOC_VARIANT_EVAL_LIMIT"
	EnvDocVariantRequireSafe    = "DOC_VARIANT_REQUIRE_SAFE"
	EnvDocVariantSafeMinSamples = "DOC_VARIANT_SAFE_MIN_SAMPLES"
	EnvDocVariantSafeMinIPS     = "DOC_VARIANT_SAFE_MIN_IPS"
	EnvDocVariantSafeMinLift    = "DOC_VARIANT_SAFE_MIN_LIFT"
)

func DocPolicyVersion() string {
	if v := strings.TrimSpace(os.Getenv(EnvDocPolicyVersion)); v != "" {
		return v
	}
	return "doc_policy_v1.0.0"
}

func DocBlueprintVersion() string {
	if v := strings.TrimSpace(os.Getenv(EnvDocBlueprintVersion)); v != "" {
		return v
	}
	return "doc_blueprint_v1.0.0"
}

func DocLookaheadDefault() int {
	return envInt(EnvDocLookaheadDefault, 2, 0, 10)
}

func DocLookaheadForPathKind(kind string) int {
	k := strings.ToLower(strings.TrimSpace(kind))
	switch k {
	case "program":
		return envInt(EnvDocLookaheadProgram, 3, 0, 20)
	default:
		return envInt(EnvDocLookaheadPath, DocLookaheadDefault(), 0, 20)
	}
}

func DocProgressiveMinGapMinutes() int {
	return envInt(EnvDocProgressiveMinGap, 5, 0, 240)
}

func DocPrereqGateMode() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv(EnvDocPrereqGateMode)))
	switch mode {
	case "hard", "soft":
		return mode
	default:
		return "soft"
	}
}

func DocPrereqReadyMin() float64 {
	return envFloat(EnvDocPrereqReadyMin, 0.75, 0.3, 0.98)
}

func DocPrereqUncertainMin() float64 {
	return envFloat(EnvDocPrereqUncertainMin, 0.55, 0.1, 0.95)
}

func DocPrereqMaxMisconReady() int {
	return envInt(EnvDocPrereqMaxMisconReady, 0, 0, 99)
}

func DocProbeRatePerHour() float64 {
	return envFloat(EnvDocProbeRatePerHour, 6, 0, 30)
}

func DocProbeMaxPerNode() int {
	return envInt(EnvDocProbeMaxPerNode, 2, 0, 10)
}

func DocProbeMaxPerLookahead() int {
	return envInt(EnvDocProbeMaxPerLookahead, 4, 0, 50)
}

func DocProbeMinInfoGain() float64 {
	return envFloat(EnvDocProbeMinInfoGain, 0.05, 0, 1)
}

func DocProbeTestletWeight() float64 {
	return envFloat(EnvDocProbeTestletWeight, 0.25, 0, 5)
}

func DocProbeMisconceptionBoost() float64 {
	return envFloat(EnvDocProbeMisconBoost, 0.3, 0, 5)
}

func DocProbePrereqBoost() float64 {
	return envFloat(EnvDocProbePrereqBoost, 0.35, 0, 5)
}

func DocVariantPolicyMode() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv(EnvDocVariantPolicyMode)))
	switch mode {
	case "off", "shadow", "active":
		return mode
	default:
		return "off"
	}
}

func DocVariantPolicyKey() string {
	if v := strings.TrimSpace(os.Getenv(EnvDocVariantPolicyKey)); v != "" {
		return v
	}
	return "doc_variant_policy_v1"
}

func DocVariantRolloutPct() float64 {
	return envFloat(EnvDocVariantRolloutPct, 0, 0, 1)
}

func DocVariantEvalMinAgeMinutes() int {
	return envInt(EnvDocVariantEvalMinAge, 30, 0, 1440)
}

func DocVariantEvalLimit() int {
	return envInt(EnvDocVariantEvalLimit, 200, 1, 2000)
}

func DocVariantRequireSafe() bool {
	return envBool(EnvDocVariantRequireSafe, true)
}

func DocVariantSafeMinSamples() int {
	return envInt(EnvDocVariantSafeMinSamples, 500, 0, 1000000)
}

func DocVariantSafeMinIPS() float64 {
	return envFloat(EnvDocVariantSafeMinIPS, 0.0, -1, 1)
}

func DocVariantSafeMinLift() float64 {
	return envFloat(EnvDocVariantSafeMinLift, -0.02, -1, 1)
}

func envFloat(key string, def float64, min float64, max float64) float64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	if v, err := strconv.ParseFloat(raw, 64); err == nil {
		if v < min {
			return min
		}
		if v > max {
			return max
		}
		return v
	}
	return def
}

func envInt(key string, def int, min int, max int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	if v, err := strconv.Atoi(raw); err == nil {
		if v < min {
			return min
		}
		if v > max {
			return max
		}
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return def
	}
}

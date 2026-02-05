package policy_eval_refresh

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/envutil"
)

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	payload := jc.Payload()
	policyKey := strings.TrimSpace(fmt.Sprint(payload["policy_key"]))
	if policyKey == "" || policyKey == "<nil>" {
		policyKey = strings.TrimSpace(envString("RUNTIME_RL_POLICY_KEY", "runtime_prompt_policy_v1"))
	}
	if policyKey == "" {
		jc.Fail("validate", fmt.Errorf("missing policy_key"))
		return nil
	}

	windowHours := intFromAny(payload["window_hours"], 0)
	if windowHours <= 0 {
		windowHours = envutil.Int("RUNTIME_RL_EVAL_WINDOW_HOURS", 168)
	}
	maxSamples := intFromAny(payload["max_samples"], 0)
	if maxSamples <= 0 {
		maxSamples = envutil.Int("RUNTIME_RL_EVAL_MAX_SAMPLES", 5000)
	}
	if maxSamples < 100 {
		maxSamples = 100
	}
	if maxSamples > 20000 {
		maxSamples = 20000
	}

	if p.traces == nil || p.evals == nil {
		jc.Fail("deps", fmt.Errorf("missing repos"))
		return nil
	}

	now := time.Now().UTC()
	since := now.Add(-time.Duration(windowHours) * time.Hour)
	jc.Progress("scan", 5, "Scanning decision traces")

	dbc := dbctx.Context{Ctx: jc.Ctx}
	traces, err := p.traces.ListByDecisionTypeSince(dbc, "runtime_prompt", since, maxSamples)
	if err != nil {
		jc.Fail("scan", err)
		return nil
	}

	// OPE stats
	samples := 0
	ipsSum := 0.0
	baselineSum := 0.0
	rewardSum := 0.0
	activeSamples := 0
	shadowSamples := 0
	baselineSamples := 0

	for _, tr := range traces {
		if tr == nil {
			continue
		}
		chosen := decodeJSONMap(tr.Chosen)
		if len(chosen) == 0 {
			continue
		}
		pk := strings.TrimSpace(stringFromAny(chosen["policy_key"]))
		sk := strings.TrimSpace(stringFromAny(chosen["shadow_policy_key"]))
		if pk != policyKey && sk != policyKey {
			continue
		}
		if _, ok := chosen["reward"]; !ok {
			continue
		}
		reward := floatFromAny(chosen["reward"], math.NaN())
		if math.IsNaN(reward) {
			continue
		}
		behaviorProb := floatFromAny(chosen["behavior_prob"], 0)
		if behaviorProb <= 0 {
			continue
		}
		policyProb := floatFromAny(chosen["policy_prob"], 0)
		baselineProb := floatFromAny(chosen["baseline_prob"], 0)

		samples++
		ipsSum += reward * safeDiv(policyProb, behaviorProb)
		baselineSum += reward * safeDiv(baselineProb, behaviorProb)
		rewardSum += reward

		mode := strings.TrimSpace(stringFromAny(chosen["policy_mode"]))
		switch mode {
		case "active":
			activeSamples++
		case "shadow":
			shadowSamples++
		default:
			baselineSamples++
		}
	}

	ips := 0.0
	baselineIPS := 0.0
	rewardMean := 0.0
	if samples > 0 {
		ips = ipsSum / float64(samples)
		baselineIPS = baselineSum / float64(samples)
		rewardMean = rewardSum / float64(samples)
	}
	lift := ips - baselineIPS

	metrics := map[string]any{
		"baseline_ips":    baselineIPS,
		"reward_mean":     rewardMean,
		"samples":         samples,
		"active_samples":  activeSamples,
		"shadow_samples":  shadowSamples,
		"baseline_samples": baselineSamples,
		"window_hours":    windowHours,
	}

	snap := &types.PolicyEvalSnapshot{
		ID:         uuid.New(),
		PolicyKey:  policyKey,
		WindowStart: since,
		WindowEnd:   now,
		Samples:    samples,
		IPS:        ips,
		Lift:       lift,
		MetricsJSON: datatypes.JSON(mustJSON(metrics)),
	}
	if err := p.evals.Create(dbc, snap); err != nil {
		jc.Fail("persist", err)
		return nil
	}

	res := map[string]any{
		"policy_key":   policyKey,
		"window_start": since.Format(time.RFC3339),
		"window_end":   now.Format(time.RFC3339),
		"samples":      samples,
		"ips":          ips,
		"lift":         lift,
	}
	jc.Succeed("done", res)
	return nil
}

func decodeJSONMap(raw datatypes.JSON) map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func floatFromAny(v any, def float64) float64 {
	if v == nil {
		return def
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" {
		return def
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return f
}

func intFromAny(v any, def int) int {
	if v == nil {
		return def
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" {
		return def
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return i
}

func safeDiv(num float64, denom float64) float64 {
	if denom == 0 {
		return 0
	}
	return num / denom
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func envString(name string, def string) string {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	return v
}

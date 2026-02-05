package policy_model_train

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
		windowHours = envutil.Int("RUNTIME_RL_TRAIN_WINDOW_HOURS", 168)
	}
	maxSamples := intFromAny(payload["max_samples"], 0)
	if maxSamples <= 0 {
		maxSamples = envutil.Int("RUNTIME_RL_TRAIN_MAX_SAMPLES", 8000)
	}
	if maxSamples < 200 {
		maxSamples = 200
	}
	if maxSamples > 40000 {
		maxSamples = 40000
	}

	minSamples := envutil.Int("RUNTIME_RL_TRAIN_MIN_SAMPLES", 200)
	lambda := envutil.Float("RUNTIME_RL_TRAIN_L2", 0.1)
	if lambda < 0 {
		lambda = 0.0
	}
	autoActivate := envutil.Bool("RUNTIME_RL_AUTO_ACTIVATE", false)

	if p.traces == nil || p.models == nil {
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

	featureSumXY := map[string]float64{}
	featureSumXX := map[string]float64{}
	count := 0
	rewardSum := 0.0

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
		features := mapFromAny(chosen["policy_features"])
		if len(features) == 0 {
			continue
		}

		count++
		rewardSum += reward
		for k, v := range features {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			fv := floatFromAny(v, 0)
			featureSumXY[k] += reward * fv
			featureSumXX[k] += fv * fv
		}
	}

	if count < minSamples {
		res := map[string]any{
			"policy_key": policyKey,
			"samples":    count,
			"min_samples": minSamples,
			"status":     "insufficient_samples",
		}
		jc.Succeed("done", res)
		return nil
	}

	weights := map[string]float64{}
	for k, sumXY := range featureSumXY {
		denom := featureSumXX[k] + lambda
		if denom <= 0 {
			continue
		}
		weights[k] = sumXY / denom
	}
	bias := 0.0
	if count > 0 {
		bias = rewardSum / float64(count)
	}

	params := map[string]any{
		"bias":        bias,
		"weights":     weights,
		"trained_at":  now.Format(time.RFC3339),
		"sample_count": count,
		"lambda":      lambda,
		"window_hours": windowHours,
	}

	metrics := map[string]any{
		"reward_mean": bias,
		"features":    len(weights),
		"samples":     count,
	}

	version := 1
	if latest, err := p.models.GetLatestByKey(dbc, policyKey); err == nil && latest != nil {
		if latest.Version >= version {
			version = latest.Version + 1
		}
	}

	row := &types.ModelSnapshot{
		ID:         uuid.New(),
		ModelKey:   policyKey,
		Version:    version,
		Active:     autoActivate,
		ParamsJSON: datatypes.JSON(mustJSON(params)),
		MetricsJSON: datatypes.JSON(mustJSON(metrics)),
	}
	if err := p.models.Create(dbc, row); err != nil {
		jc.Fail("persist", err)
		return nil
	}
	if autoActivate {
		_ = p.models.SetActiveByID(dbc, row.ID)
	}

	res := map[string]any{
		"policy_key": policyKey,
		"version":    version,
		"samples":    count,
		"features":   len(weights),
		"auto_active": autoActivate,
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

func mapFromAny(v any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	if m, ok := v.(map[string]interface{}); ok {
		out := map[string]any{}
		for k, val := range m {
			out[k] = val
		}
		return out
	}
	return map[string]any{}
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

package trace_load_test

import (
	"fmt"
	"strconv"
	"strings"

	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	learningmod "github.com/yungbote/neurobridge-backend/internal/modules/learning"
)

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	if p.db == nil {
		jc.Fail("deps", fmt.Errorf("missing db"))
		return nil
	}
	payload := jc.Payload()

	jc.Progress("load_test", 5, "Running trace load test")

	decisionTypes := []string{}
	if raw := strings.TrimSpace(fmt.Sprint(payload["decision_types"])); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				decisionTypes = append(decisionTypes, part)
			}
		}
	}

	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:      p.db,
		Log:     p.log,
		Metrics: p.metrics,
	}).TraceLoadTest(jc.Ctx, learningmod.TraceLoadTestInput{
		DryRun:           boolFromAny(payload["dry_run"], false),
		Count:            intFromAny(payload["count"], 0),
		DecisionCount:    intFromAny(payload["decision_count"], 0),
		CandidateCount:   intFromAny(payload["candidate_count"], 0),
		CandidateBytes:   intFromAny(payload["candidate_bytes"], 0),
		GraphVersion:     strings.TrimSpace(fmt.Sprint(payload["graph_version"])),
		DecisionTypes:    decisionTypes,
		RunDrift:         boolFromAny(payload["run_drift"], false),
		DriftWindowHours: intFromAny(payload["drift_window_hours"], 0),
	})
	if err != nil {
		jc.Fail("load_test", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"dry_run":            boolFromAny(payload["dry_run"], false),
		"structural_written": out.StructuralWritten,
		"decision_written":   out.DecisionWritten,
		"write_duration_ms":  out.WriteDurationMS,
		"drift_duration_ms":  out.DriftDurationMS,
		"drift_error":        out.DriftError,
	})
	return nil
}

func intFromAny(v any, def int) int {
	if v == nil {
		return def
	}
	raw := strings.TrimSpace(fmt.Sprint(v))
	if raw == "" {
		return def
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return val
}

func boolFromAny(v any, def bool) bool {
	if v == nil {
		return def
	}
	switch strings.TrimSpace(strings.ToLower(fmt.Sprint(v))) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "n", "off":
		return false
	default:
		return def
	}
}

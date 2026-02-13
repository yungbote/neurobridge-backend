package structural_trace_backfill

import (
	"fmt"
	"strconv"
	"strings"
	"time"

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

	var fromTime *time.Time
	if raw := strings.TrimSpace(fmt.Sprint(payload["from_time"])); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			tt := t.UTC()
			fromTime = &tt
		}
	}

	jc.Progress("backfill", 5, "Backfilling structural trace version tags")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:  p.db,
		Log: p.log,
	}).StructuralTraceBackfill(jc.Ctx, learningmod.StructuralTraceBackfillInput{
		DryRun:    boolFromAny(payload["dry_run"], false),
		Limit:     intFromAny(payload["limit"], 0),
		BatchSize: intFromAny(payload["batch_size"], 0),
		FromTime:  fromTime,
	})
	if err != nil {
		jc.Fail("backfill", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"dry_run":           boolFromAny(payload["dry_run"], false),
		"scanned":           out.Scanned,
		"updated":           out.Updated,
		"skipped":           out.Skipped,
		"batches":           out.Batches,
		"graph_versions":    out.GraphVersions,
		"graph_version_min": out.GraphVersionMin,
		"graph_version_max": out.GraphVersionMax,
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

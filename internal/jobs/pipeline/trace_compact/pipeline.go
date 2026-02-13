package trace_compact

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

	jc.Progress("compact", 5, "Compacting trace payloads")

	tables := []string{}
	if raw := strings.TrimSpace(fmt.Sprint(payload["tables"])); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				tables = append(tables, part)
			}
		}
	}

	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:  p.db,
		Log: p.log,
	}).TraceCompact(jc.Ctx, learningmod.TraceCompactInput{
		Tables:       tables,
		DryRun:       boolFromAny(payload["dry_run"], false),
		Limit:        intFromAny(payload["limit"], 0),
		BatchSize:    intFromAny(payload["batch_size"], 0),
		MinAgeDays:   intFromAny(payload["min_age_days"], 0),
		MaxJSONBytes: intFromAny(payload["max_json_bytes"], 0),
		MaxItems:     intFromAny(payload["max_items"], 0),
	})
	if err != nil {
		jc.Fail("compact", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"dry_run":       boolFromAny(payload["dry_run"], false),
		"total_scanned": out.TotalScanned,
		"total_updated": out.TotalUpdated,
		"tables":        out.Tables,
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

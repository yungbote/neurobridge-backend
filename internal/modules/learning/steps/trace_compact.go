package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/platform/envutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type TraceCompactDeps struct {
	DB  *gorm.DB
	Log *logger.Logger
}

type TraceCompactInput struct {
	Tables       []string
	DryRun       bool
	Limit        int
	BatchSize    int
	MinAgeDays   int
	MaxJSONBytes int
	MaxItems     int
}

type TraceCompactTableStats struct {
	Table   string `json:"table"`
	Scanned int    `json:"scanned"`
	Updated int    `json:"updated"`
	Skipped int    `json:"skipped"`
}

type TraceCompactOutput struct {
	Tables       []TraceCompactTableStats `json:"tables"`
	TotalScanned int                      `json:"total_scanned"`
	TotalUpdated int                      `json:"total_updated"`
}

type traceCompactRow struct {
	ID         uuid.UUID      `gorm:"column:id"`
	OccurredAt time.Time      `gorm:"column:occurred_at"`
	Candidates datatypes.JSON `gorm:"column:candidates"`
}

func TraceCompact(ctx context.Context, deps TraceCompactDeps, in TraceCompactInput) (TraceCompactOutput, error) {
	out := TraceCompactOutput{}
	if deps.DB == nil {
		return out, fmt.Errorf("trace_compact: missing db")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !envutil.Bool("TRACE_COMPACTION_ENABLED", false) {
		return out, nil
	}
	if in.MinAgeDays <= 0 {
		in.MinAgeDays = envutil.Int("TRACE_COMPACTION_MIN_AGE_DAYS", 30)
	}
	if in.MinAgeDays <= 0 {
		in.MinAgeDays = 30
	}
	if in.BatchSize <= 0 {
		in.BatchSize = envutil.Int("TRACE_COMPACTION_BATCH_SIZE", 500)
	}
	if in.BatchSize <= 0 {
		in.BatchSize = 500
	}
	if in.BatchSize > 5000 {
		in.BatchSize = 5000
	}
	if in.MaxJSONBytes <= 0 {
		in.MaxJSONBytes = envutil.Int("TRACE_COMPACTION_MAX_JSON_BYTES", 20000)
	}
	if in.MaxJSONBytes <= 0 {
		in.MaxJSONBytes = 20000
	}
	if in.MaxItems <= 0 {
		in.MaxItems = envutil.Int("TRACE_COMPACTION_MAX_ITEMS", 50)
	}
	if in.MaxItems <= 0 {
		in.MaxItems = 50
	}

	if len(in.Tables) == 0 {
		raw := strings.TrimSpace(strings.ToLower(os.Getenv("TRACE_COMPACTION_TABLES")))
		if raw == "" {
			in.Tables = []string{"structural_decision_trace", "decision_trace"}
		} else {
			for _, part := range strings.Split(raw, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					in.Tables = append(in.Tables, part)
				}
			}
		}
	}

	cutoff := time.Now().UTC().Add(-time.Duration(in.MinAgeDays) * 24 * time.Hour)
	for _, table := range in.Tables {
		if table == "" {
			continue
		}
		stats, err := compactTraceTable(ctx, deps.DB, table, cutoff, in, deps.Log)
		if err != nil {
			return out, err
		}
		out.Tables = append(out.Tables, stats)
		out.TotalScanned += stats.Scanned
		out.TotalUpdated += stats.Updated
	}
	return out, nil
}

func compactTraceTable(ctx context.Context, db *gorm.DB, table string, cutoff time.Time, in TraceCompactInput, log *logger.Logger) (TraceCompactTableStats, error) {
	stats := TraceCompactTableStats{Table: table}
	var lastTime time.Time
	var lastID uuid.UUID
	limit := in.Limit

	for {
		q := db.WithContext(ctx).Table(table).
			Select("id, occurred_at, candidates").
			Where("occurred_at < ?", cutoff).
			Where("candidates IS NOT NULL AND candidates <> 'null'").
			Where("octet_length(candidates) > ?", in.MaxJSONBytes)
		if !lastTime.IsZero() {
			q = q.Where("(occurred_at > ?) OR (occurred_at = ? AND id > ?)", lastTime, lastTime, lastID)
		}
		if limit > 0 && limit < in.BatchSize {
			q = q.Limit(limit)
		} else {
			q = q.Limit(in.BatchSize)
		}
		q = q.Order("occurred_at ASC, id ASC")
		rows := []traceCompactRow{}
		if err := q.Find(&rows).Error; err != nil {
			return stats, err
		}
		if len(rows) == 0 {
			break
		}

		for _, row := range rows {
			stats.Scanned++
			updated, newJSON := compactCandidatesJSON(row.Candidates, in.MaxJSONBytes, in.MaxItems)
			if !updated {
				stats.Skipped++
			} else {
				if !in.DryRun {
					if err := db.WithContext(ctx).Table(table).Where("id = ?", row.ID).Update("candidates", datatypes.JSON(newJSON)).Error; err != nil {
						return stats, err
					}
				}
				stats.Updated++
			}
			lastTime = row.OccurredAt
			lastID = row.ID
			if limit > 0 && stats.Scanned >= limit {
				return stats, nil
			}
		}
	}

	if log != nil {
		log.Info("trace_compact completed", "table", table, "scanned", stats.Scanned, "updated", stats.Updated)
	}
	return stats, nil
}

func compactCandidatesJSON(raw datatypes.JSON, maxBytes int, maxItems int) (bool, []byte) {
	if maxBytes <= 0 {
		return false, raw
	}
	if len(raw) == 0 || string(raw) == "null" {
		return false, raw
	}
	if len(raw) <= maxBytes {
		// No compaction needed unless item count exceeds maxItems.
		var payload any
		if err := json.Unmarshal(raw, &payload); err == nil {
			if arr, ok := payload.([]any); ok && maxItems > 0 && len(arr) > maxItems {
				return true, compactCandidateArray(arr, len(raw), maxItems, maxBytes)
			}
		}
		return false, raw
	}
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		summary := map[string]any{
			"_compacted":      true,
			"original_bytes":  len(raw),
			"original_format": "unknown",
		}
		out, _ := json.Marshal(summary)
		return true, out
	}
	switch typed := payload.(type) {
	case []any:
		return true, compactCandidateArray(typed, len(raw), maxItems, maxBytes)
	case map[string]any:
		summary := map[string]any{
			"_compacted":     true,
			"original_bytes": len(raw),
			"keys":           len(typed),
		}
		out, _ := json.Marshal(summary)
		return true, out
	default:
		summary := map[string]any{
			"_compacted":     true,
			"original_bytes": len(raw),
			"original_type":  fmt.Sprintf("%T", typed),
		}
		out, _ := json.Marshal(summary)
		return true, out
	}
}

func compactCandidateArray(arr []any, originalBytes int, maxItems int, maxBytes int) []byte {
	kept := arr
	if maxItems > 0 && len(arr) > maxItems {
		kept = arr[:maxItems]
	}
	sentinel := map[string]any{
		"_compacted":     true,
		"original_count": len(arr),
		"original_bytes": originalBytes,
		"kept":           len(kept),
	}
	if len(arr) > len(kept) {
		kept = append(kept, sentinel)
	} else if len(kept) == len(arr) && len(arr) > 0 {
		kept = append(kept, sentinel)
	}
	out, _ := json.Marshal(kept)
	if maxBytes > 0 && len(out) > maxBytes {
		summary := map[string]any{
			"_compacted":     true,
			"original_count": len(arr),
			"original_bytes": originalBytes,
			"kept":           len(kept),
		}
		out, _ = json.Marshal(summary)
	}
	return out
}

package steps

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/envutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type StructuralTraceBackfillDeps struct {
	DB  *gorm.DB
	Log *logger.Logger
}

type StructuralTraceBackfillInput struct {
	DryRun    bool
	Limit     int
	BatchSize int
	FromTime  *time.Time
}

type StructuralTraceBackfillOutput struct {
	Scanned         int
	Updated         int
	Skipped         int
	Batches         int
	GraphVersions   int
	GraphVersionMin string
	GraphVersionMax string
}

type graphVersionStamp struct {
	GraphVersion       string
	EmbeddingVersion   string
	TaxonomyVersion    string
	ClusteringVersion  string
	CalibrationVersion string
	CreatedAt          time.Time
}

func StructuralTraceBackfill(ctx context.Context, deps StructuralTraceBackfillDeps, in StructuralTraceBackfillInput) (StructuralTraceBackfillOutput, error) {
	out := StructuralTraceBackfillOutput{}
	if deps.DB == nil {
		return out, fmt.Errorf("structural_trace_backfill: missing db")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	enabled := envutil.Bool("STRUCTURAL_TRACE_BACKFILL_ENABLED", false)
	if !enabled {
		return out, nil
	}
	if !in.DryRun {
		in.DryRun = envutil.Bool("STRUCTURAL_TRACE_BACKFILL_DRY_RUN", true)
	}

	if in.BatchSize <= 0 {
		in.BatchSize = envutil.Int("STRUCTURAL_TRACE_BACKFILL_BATCH_SIZE", 500)
	}
	if in.BatchSize <= 0 {
		in.BatchSize = 500
	}
	if in.BatchSize > 5000 {
		in.BatchSize = 5000
	}
	if in.Limit <= 0 {
		in.Limit = envutil.Int("STRUCTURAL_TRACE_BACKFILL_LIMIT", 0)
	}

	graphVersions, err := loadGraphVersionStamps(ctx, deps.DB)
	if err != nil {
		return out, err
	}
	if len(graphVersions) == 0 {
		return out, nil
	}
	out.GraphVersions = len(graphVersions)
	out.GraphVersionMin = graphVersions[0].GraphVersion
	out.GraphVersionMax = graphVersions[len(graphVersions)-1].GraphVersion

	cond := missingVersionCondition()

	var lastTime time.Time
	var lastID uuid.UUID
	totalLimit := in.Limit

	for {
		q := deps.DB.WithContext(ctx).Model(&types.StructuralDecisionTrace{}).
			Where(cond)
		if in.FromTime != nil && !in.FromTime.IsZero() {
			q = q.Where("occurred_at >= ?", in.FromTime.UTC())
		}
		if !lastTime.IsZero() {
			q = q.Where("(occurred_at > ?) OR (occurred_at = ? AND id > ?)", lastTime, lastTime, lastID)
		}
		q = q.Order("occurred_at ASC, id ASC").Limit(in.BatchSize)
		if totalLimit > 0 && totalLimit < in.BatchSize {
			q = q.Limit(totalLimit)
		}

		rows := []*types.StructuralDecisionTrace{}
		if err := q.Find(&rows).Error; err != nil {
			return out, err
		}
		if len(rows) == 0 {
			break
		}
		out.Batches++

		for _, row := range rows {
			if row == nil || row.ID == uuid.Nil {
				continue
			}
			out.Scanned++
			version := selectGraphVersionStamp(graphVersions, row.OccurredAt)
			if version == nil || strings.TrimSpace(version.GraphVersion) == "" {
				out.Skipped++
				continue
			}
			updates := map[string]any{}
			if isMissingTag(row.GraphVersion) {
				updates["graph_version"] = version.GraphVersion
			}
			if isMissingTag(row.EmbeddingVersion) && strings.TrimSpace(version.EmbeddingVersion) != "" {
				updates["embedding_version"] = version.EmbeddingVersion
			}
			if isMissingTag(row.TaxonomyVersion) && strings.TrimSpace(version.TaxonomyVersion) != "" {
				updates["taxonomy_version"] = version.TaxonomyVersion
			}
			if isMissingTag(row.ClusteringVersion) && strings.TrimSpace(version.ClusteringVersion) != "" {
				updates["clustering_version"] = version.ClusteringVersion
			}
			if isMissingTag(row.CalibrationVersion) && strings.TrimSpace(version.CalibrationVersion) != "" {
				updates["calibration_version"] = version.CalibrationVersion
			}
			if len(updates) == 0 {
				out.Skipped++
			} else {
				if !in.DryRun {
					if err := deps.DB.WithContext(ctx).
						Model(&types.StructuralDecisionTrace{}).
						Where("id = ?", row.ID).
						Updates(updates).Error; err != nil {
						return out, err
					}
				}
				out.Updated++
			}
			lastTime = row.OccurredAt
			lastID = row.ID
			if totalLimit > 0 && out.Scanned >= totalLimit {
				return out, nil
			}
		}
	}

	return out, nil
}

func loadGraphVersionStamps(ctx context.Context, db *gorm.DB) ([]graphVersionStamp, error) {
	if db == nil {
		return nil, nil
	}
	rows := []graphVersionStamp{}
	if err := db.WithContext(ctx).
		Model(&types.GraphVersion{}).
		Select("graph_version, embedding_version, taxonomy_version, clustering_version, calibration_version, created_at").
		Order("created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	filtered := make([]graphVersionStamp, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.GraphVersion) == "" {
			continue
		}
		filtered = append(filtered, row)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
	})
	return filtered, nil
}

func selectGraphVersionStamp(list []graphVersionStamp, occurredAt time.Time) *graphVersionStamp {
	if len(list) == 0 {
		return nil
	}
	if occurredAt.IsZero() {
		return &list[len(list)-1]
	}
	idx := sort.Search(len(list), func(i int) bool {
		return list[i].CreatedAt.After(occurredAt)
	})
	if idx <= 0 {
		return &list[0]
	}
	if idx >= len(list) {
		return &list[len(list)-1]
	}
	return &list[idx-1]
}

func isMissingTag(v string) bool {
	return strings.TrimSpace(v) == ""
}

func missingVersionCondition() string {
	return strings.TrimSpace(strings.Join([]string{
		"(graph_version IS NULL OR graph_version = '')",
		"(embedding_version IS NULL OR embedding_version = '')",
		"(taxonomy_version IS NULL OR taxonomy_version = '')",
		"(clustering_version IS NULL OR clustering_version = '')",
		"(calibration_version IS NULL OR calibration_version = '')",
	}, " OR "))
}

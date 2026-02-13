package learning

import (
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type GraphVersionRepo interface {
	Create(dbc dbctx.Context, row *types.GraphVersion) error
	Upsert(dbc dbctx.Context, row *types.GraphVersion) error
	Get(dbc dbctx.Context, graphVersion string) (*types.GraphVersion, error)
	SetStatus(dbc dbctx.Context, graphVersion string, status string) error
}

type graphVersionRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewGraphVersionRepo(db *gorm.DB, baseLog *logger.Logger) GraphVersionRepo {
	return &graphVersionRepo{db: db, log: baseLog.With("repo", "GraphVersionRepo")}
}

func (r *graphVersionRepo) Create(dbc dbctx.Context, row *types.GraphVersion) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || strings.TrimSpace(row.GraphVersion) == "" {
		return nil
	}
	now := time.Now().UTC()
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = now
	}
	return t.WithContext(dbc.Ctx).Create(row).Error
}

func (r *graphVersionRepo) Upsert(dbc dbctx.Context, row *types.GraphVersion) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || strings.TrimSpace(row.GraphVersion) == "" {
		return nil
	}
	now := time.Now().UTC()
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	row.UpdatedAt = now

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "graph_version"}},
			DoUpdates: clause.Assignments(map[string]any{
				"status":              row.Status,
				"source_job":          row.SourceJob,
				"embedding_version":   row.EmbeddingVersion,
				"taxonomy_version":    row.TaxonomyVersion,
				"clustering_version":  row.ClusteringVersion,
				"calibration_version": row.CalibrationVersion,
				"snapshot_uri":        row.SnapshotURI,
				"metadata":            row.Metadata,
				"updated_at":          now,
			}),
		}).
		Create(row).Error
}

func (r *graphVersionRepo) Get(dbc dbctx.Context, graphVersion string) (*types.GraphVersion, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	graphVersion = strings.TrimSpace(graphVersion)
	if graphVersion == "" {
		return nil, nil
	}
	row := &types.GraphVersion{}
	if err := t.WithContext(dbc.Ctx).
		Where("graph_version = ?", graphVersion).
		Limit(1).
		Find(row).Error; err != nil {
		return nil, err
	}
	if row.GraphVersion == "" {
		return nil, nil
	}
	return row, nil
}

func (r *graphVersionRepo) SetStatus(dbc dbctx.Context, graphVersion string, status string) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	graphVersion = strings.TrimSpace(graphVersion)
	status = strings.TrimSpace(status)
	if graphVersion == "" || status == "" {
		return nil
	}
	return t.WithContext(dbc.Ctx).
		Model(&types.GraphVersion{}).
		Where("graph_version = ?", graphVersion).
		Updates(map[string]any{
			"status":     status,
			"updated_at": time.Now().UTC(),
		}).Error
}

package learning

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type LearningDrillInstanceRepo interface {
	GetByKey(ctx context.Context, tx *gorm.DB, userID uuid.UUID, pathNodeID uuid.UUID, kind string, count int, sourcesHash string) (*types.LearningDrillInstance, error)
	Upsert(ctx context.Context, tx *gorm.DB, row *types.LearningDrillInstance) error
}

type learningDrillInstanceRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLearningDrillInstanceRepo(db *gorm.DB, baseLog *logger.Logger) LearningDrillInstanceRepo {
	return &learningDrillInstanceRepo{db: db, log: baseLog.With("repo", "LearningDrillInstanceRepo")}
}

func (r *learningDrillInstanceRepo) GetByKey(ctx context.Context, tx *gorm.DB, userID uuid.UUID, pathNodeID uuid.UUID, kind string, count int, sourcesHash string) (*types.LearningDrillInstance, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	sourcesHash = strings.TrimSpace(sourcesHash)
	if userID == uuid.Nil || pathNodeID == uuid.Nil || kind == "" || count <= 0 || sourcesHash == "" {
		return nil, nil
	}
	var row types.LearningDrillInstance
	if err := t.WithContext(ctx).
		Where("user_id = ? AND path_node_id = ? AND kind = ? AND count = ? AND sources_hash = ?", userID, pathNodeID, kind, count, sourcesHash).
		Limit(1).
		Find(&row).Error; err != nil {
		return nil, err
	}
	if row.ID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

func (r *learningDrillInstanceRepo) Upsert(ctx context.Context, tx *gorm.DB, row *types.LearningDrillInstance) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.UserID == uuid.Nil || row.PathNodeID == uuid.Nil {
		return nil
	}
	row.Kind = strings.ToLower(strings.TrimSpace(row.Kind))
	row.SourcesHash = strings.TrimSpace(row.SourcesHash)
	if row.Kind == "" || row.Count <= 0 || row.SourcesHash == "" {
		return nil
	}
	now := time.Now().UTC()
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	row.UpdatedAt = now
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}

	return t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "user_id"},
				{Name: "path_node_id"},
				{Name: "kind"},
				{Name: "count"},
				{Name: "sources_hash"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"path_id",
				"schema_version",
				"payload_json",
				"content_hash",
				"updated_at",
			}),
		}).
		Create(row).Error
}

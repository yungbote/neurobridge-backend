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

type TeachingPatternRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.TeachingPattern) ([]*types.TeachingPattern, error)
	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.TeachingPattern, error)
	GetByPatternKey(ctx context.Context, tx *gorm.DB, patternKey string) (*types.TeachingPattern, error)
	ListAll(ctx context.Context, tx *gorm.DB, limit int) ([]*types.TeachingPattern, error)
	Count(ctx context.Context, tx *gorm.DB) (int64, error)

	UpsertByPatternKey(ctx context.Context, tx *gorm.DB, row *types.TeachingPattern) error
}

type teachingPatternRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewTeachingPatternRepo(db *gorm.DB, baseLog *logger.Logger) TeachingPatternRepo {
	return &teachingPatternRepo{db: db, log: baseLog.With("repo", "TeachingPatternRepo")}
}

func (r *teachingPatternRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.TeachingPattern) ([]*types.TeachingPattern, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.TeachingPattern{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *teachingPatternRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.TeachingPattern, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.TeachingPattern
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *teachingPatternRepo) GetByPatternKey(ctx context.Context, tx *gorm.DB, patternKey string) (*types.TeachingPattern, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	patternKey = strings.TrimSpace(patternKey)
	if patternKey == "" {
		return nil, nil
	}
	var row types.TeachingPattern
	if err := t.WithContext(ctx).Where("pattern_key = ?", patternKey).Limit(1).Find(&row).Error; err != nil {
		return nil, err
	}
	if row.ID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

func (r *teachingPatternRepo) ListAll(ctx context.Context, tx *gorm.DB, limit int) ([]*types.TeachingPattern, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if limit <= 0 {
		limit = 1000
	}
	out := []*types.TeachingPattern{}
	if err := t.WithContext(ctx).Order("updated_at DESC").Limit(limit).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *teachingPatternRepo) Count(ctx context.Context, tx *gorm.DB) (int64, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var n int64
	if err := t.WithContext(ctx).Model(&types.TeachingPattern{}).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func (r *teachingPatternRepo) UpsertByPatternKey(ctx context.Context, tx *gorm.DB, row *types.TeachingPattern) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil || strings.TrimSpace(row.PatternKey) == "" {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	row.UpdatedAt = time.Now().UTC()

	return t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "pattern_key"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"name",
				"when_to_use",
				"pattern_spec",
				"embedding",
				"vector_id",
				"updated_at",
			}),
		}).
		Create(row).Error
}

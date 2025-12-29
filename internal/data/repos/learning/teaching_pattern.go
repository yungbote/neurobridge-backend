package learning

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
)

type TeachingPatternRepo interface {
	Create(dbc dbctx.Context, rows []*types.TeachingPattern) ([]*types.TeachingPattern, error)
	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.TeachingPattern, error)
	GetByPatternKey(dbc dbctx.Context, patternKey string) (*types.TeachingPattern, error)
	ListAll(dbc dbctx.Context, limit int) ([]*types.TeachingPattern, error)
	Count(dbc dbctx.Context) (int64, error)

	UpsertByPatternKey(dbc dbctx.Context, row *types.TeachingPattern) error
}

type teachingPatternRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewTeachingPatternRepo(db *gorm.DB, baseLog *logger.Logger) TeachingPatternRepo {
	return &teachingPatternRepo{db: db, log: baseLog.With("repo", "TeachingPatternRepo")}
}

func (r *teachingPatternRepo) Create(dbc dbctx.Context, rows []*types.TeachingPattern) ([]*types.TeachingPattern, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.TeachingPattern{}, nil
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *teachingPatternRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.TeachingPattern, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.TeachingPattern
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *teachingPatternRepo) GetByPatternKey(dbc dbctx.Context, patternKey string) (*types.TeachingPattern, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	patternKey = strings.TrimSpace(patternKey)
	if patternKey == "" {
		return nil, nil
	}
	var row types.TeachingPattern
	if err := t.WithContext(dbc.Ctx).Where("pattern_key = ?", patternKey).Limit(1).Find(&row).Error; err != nil {
		return nil, err
	}
	if row.ID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

func (r *teachingPatternRepo) ListAll(dbc dbctx.Context, limit int) ([]*types.TeachingPattern, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if limit <= 0 {
		limit = 1000
	}
	out := []*types.TeachingPattern{}
	if err := t.WithContext(dbc.Ctx).Order("updated_at DESC").Limit(limit).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *teachingPatternRepo) Count(dbc dbctx.Context) (int64, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var n int64
	if err := t.WithContext(dbc.Ctx).Model(&types.TeachingPattern{}).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

func (r *teachingPatternRepo) UpsertByPatternKey(dbc dbctx.Context, row *types.TeachingPattern) error {
	t := dbc.Tx
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

	return t.WithContext(dbc.Ctx).
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

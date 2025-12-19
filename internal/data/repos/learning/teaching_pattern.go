package learning

import (
	"context"
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
	GetByName(ctx context.Context, tx *gorm.DB, name string) (*types.TeachingPattern, error)

	UpsertByName(ctx context.Context, tx *gorm.DB, row *types.TeachingPattern) error
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

func (r *teachingPatternRepo) GetByName(ctx context.Context, tx *gorm.DB, name string) (*types.TeachingPattern, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if name == "" {
		return nil, nil
	}
	var row types.TeachingPattern
	if err := t.WithContext(ctx).Where("name = ?", name).Limit(1).Find(&row).Error; err != nil {
		return nil, err
	}
	if row.ID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

func (r *teachingPatternRepo) UpsertByName(ctx context.Context, tx *gorm.DB, row *types.TeachingPattern) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.Name == "" {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	row.UpdatedAt = time.Now().UTC()

	return t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "name"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"when_to_use",
				"pattern_spec",
				"embedding",
				"vector_id",
				"updated_at",
			}),
		}).
		Create(row).Error
}











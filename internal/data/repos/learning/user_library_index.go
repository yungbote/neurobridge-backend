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

type UserLibraryIndexRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.UserLibraryIndex) ([]*types.UserLibraryIndex, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.UserLibraryIndex, error)
	GetByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.UserLibraryIndex, error)
	GetByMaterialSetIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) ([]*types.UserLibraryIndex, error)

	Upsert(ctx context.Context, tx *gorm.DB, row *types.UserLibraryIndex) error

	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	SoftDeleteByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) error
}

type userLibraryIndexRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserLibraryIndexRepo(db *gorm.DB, baseLog *logger.Logger) UserLibraryIndexRepo {
	return &userLibraryIndexRepo{db: db, log: baseLog.With("repo", "UserLibraryIndexRepo")}
}

func (r *userLibraryIndexRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.UserLibraryIndex) ([]*types.UserLibraryIndex, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.UserLibraryIndex{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *userLibraryIndexRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.UserLibraryIndex, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.UserLibraryIndex
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userLibraryIndexRepo) GetByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.UserLibraryIndex, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.UserLibraryIndex
	if len(userIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("user_id IN ?", userIDs).
		Order("user_id ASC, updated_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userLibraryIndexRepo) GetByMaterialSetIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) ([]*types.UserLibraryIndex, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.UserLibraryIndex
	if len(setIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("material_set_id IN ?", setIDs).
		Order("material_set_id ASC, updated_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userLibraryIndexRepo) Upsert(ctx context.Context, tx *gorm.DB, row *types.UserLibraryIndex) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.UserID == uuid.Nil || row.MaterialSetID == uuid.Nil {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	row.UpdatedAt = time.Now().UTC()

	return t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "material_set_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"course_id",
				"path_id",
				"tags",
				"concept_cluster_ids",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *userLibraryIndexRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.UserLibraryIndex{}).Error
}

func (r *userLibraryIndexRepo) SoftDeleteByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(userIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("user_id IN ?", userIDs).Delete(&types.UserLibraryIndex{}).Error
}

func (r *userLibraryIndexRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.UserLibraryIndex{}).Error
}

func (r *userLibraryIndexRepo) FullDeleteByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(userIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("user_id IN ?", userIDs).Delete(&types.UserLibraryIndex{}).Error
}











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

type PathNodeRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.PathNode) ([]*types.PathNode, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.PathNode, error)
	GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.PathNode, error)
	GetByPathIDs(ctx context.Context, tx *gorm.DB, pathIDs []uuid.UUID) ([]*types.PathNode, error)
	GetByPathAndIndex(ctx context.Context, tx *gorm.DB, pathID uuid.UUID, index int) (*types.PathNode, error)

	Upsert(ctx context.Context, tx *gorm.DB, row *types.PathNode) error
	Update(ctx context.Context, tx *gorm.DB, row *types.PathNode) error
	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	SoftDeleteByPathIDs(ctx context.Context, tx *gorm.DB, pathIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByPathIDs(ctx context.Context, tx *gorm.DB, pathIDs []uuid.UUID) error
}

type pathNodeRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewPathNodeRepo(db *gorm.DB, baseLog *logger.Logger) PathNodeRepo {
	return &pathNodeRepo{db: db, log: baseLog.With("repo", "PathNodeRepo")}
}

func (r *pathNodeRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.PathNode) ([]*types.PathNode, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.PathNode{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *pathNodeRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.PathNode, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.PathNode
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *pathNodeRepo) GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.PathNode, error) {
	if id == uuid.Nil {
		return nil, nil
	}
	rows, err := r.GetByIDs(ctx, tx, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (r *pathNodeRepo) GetByPathIDs(ctx context.Context, tx *gorm.DB, pathIDs []uuid.UUID) ([]*types.PathNode, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.PathNode
	if len(pathIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("path_id IN ?", pathIDs).
		Order("path_id ASC, index ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *pathNodeRepo) GetByPathAndIndex(ctx context.Context, tx *gorm.DB, pathID uuid.UUID, index int) (*types.PathNode, error) {
	if pathID == uuid.Nil {
		return nil, nil
	}
	t := tx
	if t == nil {
		t = r.db
	}
	var row types.PathNode
	err := t.WithContext(ctx).
		Where("path_id = ? AND index = ?", pathID, index).
		Limit(1).
		Find(&row).Error
	if err != nil {
		return nil, err
	}
	if row.ID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

func (r *pathNodeRepo) Upsert(ctx context.Context, tx *gorm.DB, row *types.PathNode) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.PathID == uuid.Nil {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	row.UpdatedAt = time.Now().UTC()

	return t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "path_id"}, {Name: "index"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"title",
				"parent_node_id",
				"gating",
				"metadata",
				"content_json",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *pathNodeRepo) Update(ctx context.Context, tx *gorm.DB, row *types.PathNode) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	return t.WithContext(ctx).Save(row).Error
}

func (r *pathNodeRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if id == uuid.Nil {
		return nil
	}
	if updates == nil {
		updates = map[string]interface{}{}
	}
	if _, ok := updates["updated_at"]; !ok {
		updates["updated_at"] = time.Now().UTC()
	}
	return t.WithContext(ctx).
		Model(&types.PathNode{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *pathNodeRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.PathNode{}).Error
}

func (r *pathNodeRepo) SoftDeleteByPathIDs(ctx context.Context, tx *gorm.DB, pathIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(pathIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("path_id IN ?", pathIDs).Delete(&types.PathNode{}).Error
}

func (r *pathNodeRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.PathNode{}).Error
}

func (r *pathNodeRepo) FullDeleteByPathIDs(ctx context.Context, tx *gorm.DB, pathIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(pathIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("path_id IN ?", pathIDs).Delete(&types.PathNode{}).Error
}

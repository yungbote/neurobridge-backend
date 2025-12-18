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

type PathNodeActivityRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.PathNodeActivity) ([]*types.PathNodeActivity, error)
	CreateIgnoreDuplicates(ctx context.Context, tx *gorm.DB, rows []*types.PathNodeActivity) (int, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.PathNodeActivity, error)
	GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.PathNodeActivity, error)
	GetByPathNodeIDs(ctx context.Context, tx *gorm.DB, pathNodeIDs []uuid.UUID) ([]*types.PathNodeActivity, error)
	GetByActivityIDs(ctx context.Context, tx *gorm.DB, activityIDs []uuid.UUID) ([]*types.PathNodeActivity, error)

	Upsert(ctx context.Context, tx *gorm.DB, row *types.PathNodeActivity) error
	Update(ctx context.Context, tx *gorm.DB, row *types.PathNodeActivity) error
	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	SoftDeleteByPathNodeIDs(ctx context.Context, tx *gorm.DB, pathNodeIDs []uuid.UUID) error
	SoftDeleteByActivityIDs(ctx context.Context, tx *gorm.DB, activityIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByPathNodeIDs(ctx context.Context, tx *gorm.DB, pathNodeIDs []uuid.UUID) error
	FullDeleteByActivityIDs(ctx context.Context, tx *gorm.DB, activityIDs []uuid.UUID) error
}

type pathNodeActivityRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewPathNodeActivityRepo(db *gorm.DB, baseLog *logger.Logger) PathNodeActivityRepo {
	return &pathNodeActivityRepo{db: db, log: baseLog.With("repo", "PathNodeActivityRepo")}
}

func (r *pathNodeActivityRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.PathNodeActivity) ([]*types.PathNodeActivity, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.PathNodeActivity{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *pathNodeActivityRepo) CreateIgnoreDuplicates(ctx context.Context, tx *gorm.DB, rows []*types.PathNodeActivity) (int, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return 0, nil
	}
	res := t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "path_node_id"}, {Name: "activity_id"}},
			DoNothing: true,
		}).
		Create(&rows)
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

func (r *pathNodeActivityRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.PathNodeActivity, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.PathNodeActivity
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *pathNodeActivityRepo) GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.PathNodeActivity, error) {
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

func (r *pathNodeActivityRepo) GetByPathNodeIDs(ctx context.Context, tx *gorm.DB, pathNodeIDs []uuid.UUID) ([]*types.PathNodeActivity, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.PathNodeActivity
	if len(pathNodeIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("path_node_id IN ?", pathNodeIDs).
		Order("path_node_id ASC, is_primary DESC, rank ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *pathNodeActivityRepo) GetByActivityIDs(ctx context.Context, tx *gorm.DB, activityIDs []uuid.UUID) ([]*types.PathNodeActivity, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.PathNodeActivity
	if len(activityIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("activity_id IN ?", activityIDs).
		Order("activity_id ASC, path_node_id ASC, is_primary DESC, rank ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *pathNodeActivityRepo) Upsert(ctx context.Context, tx *gorm.DB, row *types.PathNodeActivity) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.PathNodeID == uuid.Nil || row.ActivityID == uuid.Nil {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	row.UpdatedAt = time.Now().UTC()

	return t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "path_node_id"}, {Name: "activity_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"rank",
				"is_primary",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *pathNodeActivityRepo) Update(ctx context.Context, tx *gorm.DB, row *types.PathNodeActivity) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	return t.WithContext(ctx).Save(row).Error
}

func (r *pathNodeActivityRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.PathNodeActivity{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *pathNodeActivityRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.PathNodeActivity{}).Error
}

func (r *pathNodeActivityRepo) SoftDeleteByPathNodeIDs(ctx context.Context, tx *gorm.DB, pathNodeIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(pathNodeIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("path_node_id IN ?", pathNodeIDs).Delete(&types.PathNodeActivity{}).Error
}

func (r *pathNodeActivityRepo) SoftDeleteByActivityIDs(ctx context.Context, tx *gorm.DB, activityIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(activityIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("activity_id IN ?", activityIDs).Delete(&types.PathNodeActivity{}).Error
}

func (r *pathNodeActivityRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.PathNodeActivity{}).Error
}

func (r *pathNodeActivityRepo) FullDeleteByPathNodeIDs(ctx context.Context, tx *gorm.DB, pathNodeIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(pathNodeIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("path_node_id IN ?", pathNodeIDs).Delete(&types.PathNodeActivity{}).Error
}

func (r *pathNodeActivityRepo) FullDeleteByActivityIDs(ctx context.Context, tx *gorm.DB, activityIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(activityIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("activity_id IN ?", activityIDs).Delete(&types.PathNodeActivity{}).Error
}

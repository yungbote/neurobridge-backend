package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type PathNodeActivityRepo interface {
	Create(dbc dbctx.Context, rows []*types.PathNodeActivity) ([]*types.PathNodeActivity, error)
	CreateIgnoreDuplicates(dbc dbctx.Context, rows []*types.PathNodeActivity) (int, error)

	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.PathNodeActivity, error)
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.PathNodeActivity, error)
	GetByPathNodeIDs(dbc dbctx.Context, pathNodeIDs []uuid.UUID) ([]*types.PathNodeActivity, error)
	GetByActivityIDs(dbc dbctx.Context, activityIDs []uuid.UUID) ([]*types.PathNodeActivity, error)

	Upsert(dbc dbctx.Context, row *types.PathNodeActivity) error
	Update(dbc dbctx.Context, row *types.PathNodeActivity) error
	UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	SoftDeleteByPathNodeIDs(dbc dbctx.Context, pathNodeIDs []uuid.UUID) error
	SoftDeleteByActivityIDs(dbc dbctx.Context, activityIDs []uuid.UUID) error
	FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	FullDeleteByPathNodeIDs(dbc dbctx.Context, pathNodeIDs []uuid.UUID) error
	FullDeleteByActivityIDs(dbc dbctx.Context, activityIDs []uuid.UUID) error
}

type pathNodeActivityRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewPathNodeActivityRepo(db *gorm.DB, baseLog *logger.Logger) PathNodeActivityRepo {
	return &pathNodeActivityRepo{db: db, log: baseLog.With("repo", "PathNodeActivityRepo")}
}

func (r *pathNodeActivityRepo) Create(dbc dbctx.Context, rows []*types.PathNodeActivity) ([]*types.PathNodeActivity, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.PathNodeActivity{}, nil
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *pathNodeActivityRepo) CreateIgnoreDuplicates(dbc dbctx.Context, rows []*types.PathNodeActivity) (int, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return 0, nil
	}
	res := t.WithContext(dbc.Ctx).
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

func (r *pathNodeActivityRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.PathNodeActivity, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.PathNodeActivity
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *pathNodeActivityRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.PathNodeActivity, error) {
	if id == uuid.Nil {
		return nil, nil
	}
	rows, err := r.GetByIDs(dbc, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (r *pathNodeActivityRepo) GetByPathNodeIDs(dbc dbctx.Context, pathNodeIDs []uuid.UUID) ([]*types.PathNodeActivity, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.PathNodeActivity
	if len(pathNodeIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("path_node_id IN ?", pathNodeIDs).
		Order("path_node_id ASC, is_primary DESC, rank ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *pathNodeActivityRepo) GetByActivityIDs(dbc dbctx.Context, activityIDs []uuid.UUID) ([]*types.PathNodeActivity, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.PathNodeActivity
	if len(activityIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("activity_id IN ?", activityIDs).
		Order("activity_id ASC, path_node_id ASC, is_primary DESC, rank ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *pathNodeActivityRepo) Upsert(dbc dbctx.Context, row *types.PathNodeActivity) error {
	t := dbc.Tx
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

	return t.WithContext(dbc.Ctx).
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

func (r *pathNodeActivityRepo) Update(dbc dbctx.Context, row *types.PathNodeActivity) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	return t.WithContext(dbc.Ctx).Save(row).Error
}

func (r *pathNodeActivityRepo) UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error {
	t := dbc.Tx
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
	return t.WithContext(dbc.Ctx).
		Model(&types.PathNodeActivity{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *pathNodeActivityRepo) SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("id IN ?", ids).Delete(&types.PathNodeActivity{}).Error
}

func (r *pathNodeActivityRepo) SoftDeleteByPathNodeIDs(dbc dbctx.Context, pathNodeIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(pathNodeIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("path_node_id IN ?", pathNodeIDs).Delete(&types.PathNodeActivity{}).Error
}

func (r *pathNodeActivityRepo) SoftDeleteByActivityIDs(dbc dbctx.Context, activityIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(activityIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("activity_id IN ?", activityIDs).Delete(&types.PathNodeActivity{}).Error
}

func (r *pathNodeActivityRepo) FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("id IN ?", ids).Delete(&types.PathNodeActivity{}).Error
}

func (r *pathNodeActivityRepo) FullDeleteByPathNodeIDs(dbc dbctx.Context, pathNodeIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(pathNodeIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("path_node_id IN ?", pathNodeIDs).Delete(&types.PathNodeActivity{}).Error
}

func (r *pathNodeActivityRepo) FullDeleteByActivityIDs(dbc dbctx.Context, activityIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(activityIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("activity_id IN ?", activityIDs).Delete(&types.PathNodeActivity{}).Error
}

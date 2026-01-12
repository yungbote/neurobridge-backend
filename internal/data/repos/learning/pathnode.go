package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type PathNodeRepo interface {
	Create(dbc dbctx.Context, rows []*types.PathNode) ([]*types.PathNode, error)

	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.PathNode, error)
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.PathNode, error)
	GetByPathIDs(dbc dbctx.Context, pathIDs []uuid.UUID) ([]*types.PathNode, error)
	GetByPathAndIndex(dbc dbctx.Context, pathID uuid.UUID, index int) (*types.PathNode, error)

	Upsert(dbc dbctx.Context, row *types.PathNode) error
	Update(dbc dbctx.Context, row *types.PathNode) error
	UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	SoftDeleteByPathIDs(dbc dbctx.Context, pathIDs []uuid.UUID) error
	FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	FullDeleteByPathIDs(dbc dbctx.Context, pathIDs []uuid.UUID) error
}

type pathNodeRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewPathNodeRepo(db *gorm.DB, baseLog *logger.Logger) PathNodeRepo {
	return &pathNodeRepo{db: db, log: baseLog.With("repo", "PathNodeRepo")}
}

func (r *pathNodeRepo) Create(dbc dbctx.Context, rows []*types.PathNode) ([]*types.PathNode, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.PathNode{}, nil
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *pathNodeRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.PathNode, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.PathNode
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *pathNodeRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.PathNode, error) {
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

func (r *pathNodeRepo) GetByPathIDs(dbc dbctx.Context, pathIDs []uuid.UUID) ([]*types.PathNode, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.PathNode
	if len(pathIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("path_id IN ?", pathIDs).
		Order("path_id ASC, index ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *pathNodeRepo) GetByPathAndIndex(dbc dbctx.Context, pathID uuid.UUID, index int) (*types.PathNode, error) {
	if pathID == uuid.Nil {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var row types.PathNode
	err := t.WithContext(dbc.Ctx).
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

func (r *pathNodeRepo) Upsert(dbc dbctx.Context, row *types.PathNode) error {
	t := dbc.Tx
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

	return t.WithContext(dbc.Ctx).
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

func (r *pathNodeRepo) Update(dbc dbctx.Context, row *types.PathNode) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	return t.WithContext(dbc.Ctx).Save(row).Error
}

func (r *pathNodeRepo) UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.PathNode{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *pathNodeRepo) SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("id IN ?", ids).Delete(&types.PathNode{}).Error
}

func (r *pathNodeRepo) SoftDeleteByPathIDs(dbc dbctx.Context, pathIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(pathIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("path_id IN ?", pathIDs).Delete(&types.PathNode{}).Error
}

func (r *pathNodeRepo) FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("id IN ?", ids).Delete(&types.PathNode{}).Error
}

func (r *pathNodeRepo) FullDeleteByPathIDs(dbc dbctx.Context, pathIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(pathIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("path_id IN ?", pathIDs).Delete(&types.PathNode{}).Error
}

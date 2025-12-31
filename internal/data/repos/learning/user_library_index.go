package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type UserLibraryIndexRepo interface {
	Create(dbc dbctx.Context, rows []*types.UserLibraryIndex) ([]*types.UserLibraryIndex, error)

	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.UserLibraryIndex, error)
	GetByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) ([]*types.UserLibraryIndex, error)
	GetByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) ([]*types.UserLibraryIndex, error)
	GetByUserAndMaterialSet(dbc dbctx.Context, userID uuid.UUID, materialSetID uuid.UUID) (*types.UserLibraryIndex, error)
	GetByUserAndMaterialSetForUpdate(dbc dbctx.Context, userID uuid.UUID, materialSetID uuid.UUID) (*types.UserLibraryIndex, error)
	GetByUserAndPathID(dbc dbctx.Context, userID uuid.UUID, pathID uuid.UUID) (*types.UserLibraryIndex, error)

	Upsert(dbc dbctx.Context, row *types.UserLibraryIndex) error
	UpsertPathID(dbc dbctx.Context, userID uuid.UUID, materialSetID uuid.UUID, pathID uuid.UUID) error

	SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	SoftDeleteByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) error
	FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	FullDeleteByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) error
}

type userLibraryIndexRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserLibraryIndexRepo(db *gorm.DB, baseLog *logger.Logger) UserLibraryIndexRepo {
	return &userLibraryIndexRepo{db: db, log: baseLog.With("repo", "UserLibraryIndexRepo")}
}

func (r *userLibraryIndexRepo) Create(dbc dbctx.Context, rows []*types.UserLibraryIndex) ([]*types.UserLibraryIndex, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.UserLibraryIndex{}, nil
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *userLibraryIndexRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.UserLibraryIndex, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.UserLibraryIndex
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userLibraryIndexRepo) GetByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) ([]*types.UserLibraryIndex, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.UserLibraryIndex
	if len(userIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("user_id IN ?", userIDs).
		Order("user_id ASC, updated_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userLibraryIndexRepo) GetByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) ([]*types.UserLibraryIndex, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.UserLibraryIndex
	if len(setIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("material_set_id IN ?", setIDs).
		Order("material_set_id ASC, updated_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userLibraryIndexRepo) GetByUserAndMaterialSet(dbc dbctx.Context, userID uuid.UUID, materialSetID uuid.UUID) (*types.UserLibraryIndex, error) {
	return r.getByUserAndMaterialSet(dbc, userID, materialSetID, false)
}

func (r *userLibraryIndexRepo) GetByUserAndMaterialSetForUpdate(dbc dbctx.Context, userID uuid.UUID, materialSetID uuid.UUID) (*types.UserLibraryIndex, error) {
	return r.getByUserAndMaterialSet(dbc, userID, materialSetID, true)
}

func (r *userLibraryIndexRepo) GetByUserAndPathID(dbc dbctx.Context, userID uuid.UUID, pathID uuid.UUID) (*types.UserLibraryIndex, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil || pathID == uuid.Nil {
		return nil, nil
	}
	var row types.UserLibraryIndex
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND path_id = ?", userID, pathID).
		Limit(1).
		Find(&row).Error; err != nil {
		return nil, err
	}
	if row.ID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

func (r *userLibraryIndexRepo) getByUserAndMaterialSet(dbc dbctx.Context, userID uuid.UUID, materialSetID uuid.UUID, forUpdate bool) (*types.UserLibraryIndex, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil || materialSetID == uuid.Nil {
		return nil, nil
	}
	q := t.WithContext(dbc.Ctx)
	if forUpdate {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var row types.UserLibraryIndex
	if err := q.Where("user_id = ? AND material_set_id = ?", userID, materialSetID).Limit(1).Find(&row).Error; err != nil {
		return nil, err
	}
	if row.ID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

func (r *userLibraryIndexRepo) Upsert(dbc dbctx.Context, row *types.UserLibraryIndex) error {
	t := dbc.Tx
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

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "material_set_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"path_id",
				"tags",
				"concept_cluster_ids",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *userLibraryIndexRepo) UpsertPathID(dbc dbctx.Context, userID uuid.UUID, materialSetID uuid.UUID, pathID uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil || materialSetID == uuid.Nil || pathID == uuid.Nil {
		return nil
	}
	now := time.Now().UTC()
	row := &types.UserLibraryIndex{
		ID:                uuid.New(),
		UserID:            userID,
		MaterialSetID:     materialSetID,
		PathID:            &pathID,
		Tags:              datatypes.JSON([]byte(`[]`)),
		ConceptClusterIDs: datatypes.JSON([]byte(`[]`)),
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "material_set_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"path_id":    pathID,
				"updated_at": now,
			}),
		}).
		Create(row).Error
}

func (r *userLibraryIndexRepo) SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("id IN ?", ids).Delete(&types.UserLibraryIndex{}).Error
}

func (r *userLibraryIndexRepo) SoftDeleteByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(userIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("user_id IN ?", userIDs).Delete(&types.UserLibraryIndex{}).Error
}

func (r *userLibraryIndexRepo) FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("id IN ?", ids).Delete(&types.UserLibraryIndex{}).Error
}

func (r *userLibraryIndexRepo) FullDeleteByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(userIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("user_id IN ?", userIDs).Delete(&types.UserLibraryIndex{}).Error
}

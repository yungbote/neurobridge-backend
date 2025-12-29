package materials

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type MaterialSetRepo interface {
	Create(dbc dbctx.Context, sets []*types.MaterialSet) ([]*types.MaterialSet, error)
	GetByIDs(dbc dbctx.Context, setIDs []uuid.UUID) ([]*types.MaterialSet, error)
	GetByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) ([]*types.MaterialSet, error)
	GetByStatus(dbc dbctx.Context, statuses []string) ([]*types.MaterialSet, error)
	SoftDeleteByIDs(dbc dbctx.Context, setIDs []uuid.UUID) error
	FullDeleteByIDs(dbc dbctx.Context, setIDs []uuid.UUID) error
}

type materialSetRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewMaterialSetRepo(db *gorm.DB, baseLog *logger.Logger) MaterialSetRepo {
	repoLog := baseLog.With("repo", "MaterialSetRepo")
	return &materialSetRepo{db: db, log: repoLog}
}

func (r *materialSetRepo) Create(dbc dbctx.Context, sets []*types.MaterialSet) ([]*types.MaterialSet, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	if len(sets) == 0 {
		return []*types.MaterialSet{}, nil
	}

	if err := transaction.WithContext(dbc.Ctx).Create(&sets).Error; err != nil {
		return nil, err
	}
	return sets, nil
}

func (r *materialSetRepo) GetByIDs(dbc dbctx.Context, setIDs []uuid.UUID) ([]*types.MaterialSet, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.MaterialSet
	if len(setIDs) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Where("id IN ?", setIDs).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *materialSetRepo) GetByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) ([]*types.MaterialSet, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.MaterialSet
	if len(userIDs) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Where("user_id IN ?", userIDs).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *materialSetRepo) GetByStatus(dbc dbctx.Context, statuses []string) ([]*types.MaterialSet, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.MaterialSet
	if len(statuses) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Where("status IN ?", statuses).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *materialSetRepo) SoftDeleteByIDs(dbc dbctx.Context, setIDs []uuid.UUID) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	if len(setIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Where("id IN ?", setIDs).
		Delete(&types.MaterialSet{}).Error; err != nil {
		return err
	}
	return nil
}

func (r *materialSetRepo) FullDeleteByIDs(dbc dbctx.Context, setIDs []uuid.UUID) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	if len(setIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Unscoped().
		Where("id IN ?", setIDs).
		Delete(&types.MaterialSet{}).Error; err != nil {
		return err
	}
	return nil
}

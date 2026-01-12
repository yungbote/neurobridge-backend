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

type ActivityVariantRepo interface {
	Create(dbc dbctx.Context, rows []*types.ActivityVariant) ([]*types.ActivityVariant, error)

	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.ActivityVariant, error)
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.ActivityVariant, error)

	GetByActivityIDs(dbc dbctx.Context, activityIDs []uuid.UUID) ([]*types.ActivityVariant, error)
	GetByActivityAndVariants(dbc dbctx.Context, activityID uuid.UUID, variants []string) ([]*types.ActivityVariant, error)
	GetByActivityAndVariant(dbc dbctx.Context, activityID uuid.UUID, variant string) (*types.ActivityVariant, error)

	Upsert(dbc dbctx.Context, row *types.ActivityVariant) error
	Update(dbc dbctx.Context, row *types.ActivityVariant) error
	UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	SoftDeleteByActivityIDs(dbc dbctx.Context, activityIDs []uuid.UUID) error
	SoftDeleteByActivityAndVariants(dbc dbctx.Context, activityID uuid.UUID, variants []string) error
	FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	FullDeleteByActivityIDs(dbc dbctx.Context, activityIDs []uuid.UUID) error
	FullDeleteByActivityAndVariants(dbc dbctx.Context, activityID uuid.UUID, variants []string) error
}

type activityVariantRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewActivityVariantRepo(db *gorm.DB, baseLog *logger.Logger) ActivityVariantRepo {
	return &activityVariantRepo{db: db, log: baseLog.With("repo", "ActivityVariantRepo")}
}

func (r *activityVariantRepo) Create(dbc dbctx.Context, rows []*types.ActivityVariant) ([]*types.ActivityVariant, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.ActivityVariant{}, nil
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *activityVariantRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.ActivityVariant, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ActivityVariant
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("id IN ?", ids).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityVariantRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.ActivityVariant, error) {
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

func (r *activityVariantRepo) GetByActivityIDs(dbc dbctx.Context, activityIDs []uuid.UUID) ([]*types.ActivityVariant, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ActivityVariant
	if len(activityIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("activity_id IN ?", activityIDs).
		Order("activity_id ASC, variant ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityVariantRepo) GetByActivityAndVariants(dbc dbctx.Context, activityID uuid.UUID, variants []string) ([]*types.ActivityVariant, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ActivityVariant
	if activityID == uuid.Nil || len(variants) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("activity_id = ? AND variant IN ?", activityID, variants).
		Order("variant ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityVariantRepo) GetByActivityAndVariant(dbc dbctx.Context, activityID uuid.UUID, variant string) (*types.ActivityVariant, error) {
	if activityID == uuid.Nil || variant == "" {
		return nil, nil
	}
	rows, err := r.GetByActivityAndVariants(dbc, activityID, []string{variant})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (r *activityVariantRepo) Upsert(dbc dbctx.Context, row *types.ActivityVariant) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.ActivityID == uuid.Nil || row.Variant == "" {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = time.Now().UTC()
	}

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "activity_id"}, {Name: "variant"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"content_md",
				"content_json",
				"render_spec",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *activityVariantRepo) Update(dbc dbctx.Context, row *types.ActivityVariant) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	return t.WithContext(dbc.Ctx).Save(row).Error
}

func (r *activityVariantRepo) UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.ActivityVariant{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *activityVariantRepo) SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("id IN ?", ids).Delete(&types.ActivityVariant{}).Error
}

func (r *activityVariantRepo) SoftDeleteByActivityIDs(dbc dbctx.Context, activityIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(activityIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("activity_id IN ?", activityIDs).Delete(&types.ActivityVariant{}).Error
}

func (r *activityVariantRepo) SoftDeleteByActivityAndVariants(dbc dbctx.Context, activityID uuid.UUID, variants []string) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if activityID == uuid.Nil || len(variants) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).
		Where("activity_id = ? AND variant IN ?", activityID, variants).
		Delete(&types.ActivityVariant{}).Error
}

func (r *activityVariantRepo) FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("id IN ?", ids).Delete(&types.ActivityVariant{}).Error
}

func (r *activityVariantRepo) FullDeleteByActivityIDs(dbc dbctx.Context, activityIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(activityIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("activity_id IN ?", activityIDs).Delete(&types.ActivityVariant{}).Error
}

func (r *activityVariantRepo) FullDeleteByActivityAndVariants(dbc dbctx.Context, activityID uuid.UUID, variants []string) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if activityID == uuid.Nil || len(variants) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).
		Unscoped().
		Where("activity_id = ? AND variant IN ?", activityID, variants).
		Delete(&types.ActivityVariant{}).Error
}

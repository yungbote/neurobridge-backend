package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
)

type ActivityConceptRepo interface {
	Create(dbc dbctx.Context, rows []*types.ActivityConcept) ([]*types.ActivityConcept, error)
	CreateIgnoreDuplicates(dbc dbctx.Context, rows []*types.ActivityConcept) (int, error)

	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.ActivityConcept, error)
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.ActivityConcept, error)

	GetByActivityIDs(dbc dbctx.Context, activityIDs []uuid.UUID) ([]*types.ActivityConcept, error)
	GetByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) ([]*types.ActivityConcept, error)

	Upsert(dbc dbctx.Context, row *types.ActivityConcept) error
	Update(dbc dbctx.Context, row *types.ActivityConcept) error
	UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	SoftDeleteByActivityIDs(dbc dbctx.Context, activityIDs []uuid.UUID) error
	SoftDeleteByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) error
	FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	FullDeleteByActivityIDs(dbc dbctx.Context, activityIDs []uuid.UUID) error
	FullDeleteByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) error
}

type activityConceptRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewActivityConceptRepo(db *gorm.DB, baseLog *logger.Logger) ActivityConceptRepo {
	return &activityConceptRepo{db: db, log: baseLog.With("repo", "ActivityConceptRepo")}
}

func (r *activityConceptRepo) Create(dbc dbctx.Context, rows []*types.ActivityConcept) ([]*types.ActivityConcept, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.ActivityConcept{}, nil
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *activityConceptRepo) CreateIgnoreDuplicates(dbc dbctx.Context, rows []*types.ActivityConcept) (int, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return 0, nil
	}
	res := t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "activity_id"}, {Name: "concept_id"}},
			DoNothing: true,
		}).
		Create(&rows)
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

func (r *activityConceptRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.ActivityConcept, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ActivityConcept
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

func (r *activityConceptRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.ActivityConcept, error) {
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

func (r *activityConceptRepo) GetByActivityIDs(dbc dbctx.Context, activityIDs []uuid.UUID) ([]*types.ActivityConcept, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ActivityConcept
	if len(activityIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("activity_id IN ?", activityIDs).
		Order("activity_id ASC, weight DESC, role ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityConceptRepo) GetByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) ([]*types.ActivityConcept, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ActivityConcept
	if len(conceptIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("concept_id IN ?", conceptIDs).
		Order("concept_id ASC, weight DESC, role ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityConceptRepo) Upsert(dbc dbctx.Context, row *types.ActivityConcept) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.ActivityID == uuid.Nil || row.ConceptID == uuid.Nil {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	row.UpdatedAt = time.Now().UTC()

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "activity_id"}, {Name: "concept_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"role",
				"weight",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *activityConceptRepo) Update(dbc dbctx.Context, row *types.ActivityConcept) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	return t.WithContext(dbc.Ctx).Save(row).Error
}

func (r *activityConceptRepo) UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.ActivityConcept{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *activityConceptRepo) SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("id IN ?", ids).Delete(&types.ActivityConcept{}).Error
}

func (r *activityConceptRepo) SoftDeleteByActivityIDs(dbc dbctx.Context, activityIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(activityIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("activity_id IN ?", activityIDs).Delete(&types.ActivityConcept{}).Error
}

func (r *activityConceptRepo) SoftDeleteByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(conceptIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("concept_id IN ?", conceptIDs).Delete(&types.ActivityConcept{}).Error
}

func (r *activityConceptRepo) FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("id IN ?", ids).Delete(&types.ActivityConcept{}).Error
}

func (r *activityConceptRepo) FullDeleteByActivityIDs(dbc dbctx.Context, activityIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(activityIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("activity_id IN ?", activityIDs).Delete(&types.ActivityConcept{}).Error
}

func (r *activityConceptRepo) FullDeleteByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(conceptIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("concept_id IN ?", conceptIDs).Delete(&types.ActivityConcept{}).Error
}

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

type ActivityConceptRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.ActivityConcept) ([]*types.ActivityConcept, error)
	CreateIgnoreDuplicates(ctx context.Context, tx *gorm.DB, rows []*types.ActivityConcept) (int, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.ActivityConcept, error)
	GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.ActivityConcept, error)

	GetByActivityIDs(ctx context.Context, tx *gorm.DB, activityIDs []uuid.UUID) ([]*types.ActivityConcept, error)
	GetByConceptIDs(ctx context.Context, tx *gorm.DB, conceptIDs []uuid.UUID) ([]*types.ActivityConcept, error)

	Upsert(ctx context.Context, tx *gorm.DB, row *types.ActivityConcept) error
	Update(ctx context.Context, tx *gorm.DB, row *types.ActivityConcept) error
	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	SoftDeleteByActivityIDs(ctx context.Context, tx *gorm.DB, activityIDs []uuid.UUID) error
	SoftDeleteByConceptIDs(ctx context.Context, tx *gorm.DB, conceptIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByActivityIDs(ctx context.Context, tx *gorm.DB, activityIDs []uuid.UUID) error
	FullDeleteByConceptIDs(ctx context.Context, tx *gorm.DB, conceptIDs []uuid.UUID) error
}

type activityConceptRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewActivityConceptRepo(db *gorm.DB, baseLog *logger.Logger) ActivityConceptRepo {
	return &activityConceptRepo{db: db, log: baseLog.With("repo", "ActivityConceptRepo")}
}

func (r *activityConceptRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.ActivityConcept) ([]*types.ActivityConcept, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.ActivityConcept{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *activityConceptRepo) CreateIgnoreDuplicates(ctx context.Context, tx *gorm.DB, rows []*types.ActivityConcept) (int, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return 0, nil
	}
	res := t.WithContext(ctx).
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

func (r *activityConceptRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.ActivityConcept, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ActivityConcept
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("id IN ?", ids).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityConceptRepo) GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.ActivityConcept, error) {
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

func (r *activityConceptRepo) GetByActivityIDs(ctx context.Context, tx *gorm.DB, activityIDs []uuid.UUID) ([]*types.ActivityConcept, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ActivityConcept
	if len(activityIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("activity_id IN ?", activityIDs).
		Order("activity_id ASC, weight DESC, role ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityConceptRepo) GetByConceptIDs(ctx context.Context, tx *gorm.DB, conceptIDs []uuid.UUID) ([]*types.ActivityConcept, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ActivityConcept
	if len(conceptIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("concept_id IN ?", conceptIDs).
		Order("concept_id ASC, weight DESC, role ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityConceptRepo) Upsert(ctx context.Context, tx *gorm.DB, row *types.ActivityConcept) error {
	t := tx
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

	return t.WithContext(ctx).
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

func (r *activityConceptRepo) Update(ctx context.Context, tx *gorm.DB, row *types.ActivityConcept) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	return t.WithContext(ctx).Save(row).Error
}

func (r *activityConceptRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.ActivityConcept{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *activityConceptRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.ActivityConcept{}).Error
}

func (r *activityConceptRepo) SoftDeleteByActivityIDs(ctx context.Context, tx *gorm.DB, activityIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(activityIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("activity_id IN ?", activityIDs).Delete(&types.ActivityConcept{}).Error
}

func (r *activityConceptRepo) SoftDeleteByConceptIDs(ctx context.Context, tx *gorm.DB, conceptIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(conceptIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("concept_id IN ?", conceptIDs).Delete(&types.ActivityConcept{}).Error
}

func (r *activityConceptRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.ActivityConcept{}).Error
}

func (r *activityConceptRepo) FullDeleteByActivityIDs(ctx context.Context, tx *gorm.DB, activityIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(activityIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("activity_id IN ?", activityIDs).Delete(&types.ActivityConcept{}).Error
}

func (r *activityConceptRepo) FullDeleteByConceptIDs(ctx context.Context, tx *gorm.DB, conceptIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(conceptIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("concept_id IN ?", conceptIDs).Delete(&types.ActivityConcept{}).Error
}

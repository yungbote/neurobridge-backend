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

type ConceptEdgeRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.ConceptEdge) ([]*types.ConceptEdge, error)
	CreateIgnoreDuplicates(ctx context.Context, tx *gorm.DB, rows []*types.ConceptEdge) (int, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.ConceptEdge, error)

	GetByFromConceptIDs(ctx context.Context, tx *gorm.DB, fromIDs []uuid.UUID) ([]*types.ConceptEdge, error)
	GetByToConceptIDs(ctx context.Context, tx *gorm.DB, toIDs []uuid.UUID) ([]*types.ConceptEdge, error)
	GetByConceptIDs(ctx context.Context, tx *gorm.DB, conceptIDs []uuid.UUID) ([]*types.ConceptEdge, error)

	Upsert(ctx context.Context, tx *gorm.DB, row *types.ConceptEdge) error

	SoftDeleteByConceptIDs(ctx context.Context, tx *gorm.DB, conceptIDs []uuid.UUID) error
	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByConceptIDs(ctx context.Context, tx *gorm.DB, conceptIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
}

type conceptEdgeRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewConceptEdgeRepo(db *gorm.DB, baseLog *logger.Logger) ConceptEdgeRepo {
	return &conceptEdgeRepo{db: db, log: baseLog.With("repo", "ConceptEdgeRepo")}
}

func (r *conceptEdgeRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.ConceptEdge) ([]*types.ConceptEdge, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.ConceptEdge{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *conceptEdgeRepo) CreateIgnoreDuplicates(ctx context.Context, tx *gorm.DB, rows []*types.ConceptEdge) (int, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return 0, nil
	}
	res := t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "from_concept_id"}, {Name: "to_concept_id"}, {Name: "edge_type"}},
			DoNothing: true,
		}).
		Create(&rows)
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

func (r *conceptEdgeRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.ConceptEdge, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptEdge
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptEdgeRepo) GetByFromConceptIDs(ctx context.Context, tx *gorm.DB, fromIDs []uuid.UUID) ([]*types.ConceptEdge, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptEdge
	if len(fromIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("from_concept_id IN ?", fromIDs).
		Order("from_concept_id ASC, edge_type ASC, strength DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptEdgeRepo) GetByToConceptIDs(ctx context.Context, tx *gorm.DB, toIDs []uuid.UUID) ([]*types.ConceptEdge, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptEdge
	if len(toIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("to_concept_id IN ?", toIDs).
		Order("to_concept_id ASC, edge_type ASC, strength DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptEdgeRepo) GetByConceptIDs(ctx context.Context, tx *gorm.DB, conceptIDs []uuid.UUID) ([]*types.ConceptEdge, error) {
	// union: from in ids OR to in ids
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptEdge
	if len(conceptIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("from_concept_id IN ? OR to_concept_id IN ?", conceptIDs, conceptIDs).
		Order("edge_type ASC, strength DESC, created_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptEdgeRepo) Upsert(ctx context.Context, tx *gorm.DB, row *types.ConceptEdge) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.FromConceptID == uuid.Nil || row.ToConceptID == uuid.Nil || row.EdgeType == "" {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	row.UpdatedAt = time.Now().UTC()

	return t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "from_concept_id"}, {Name: "to_concept_id"}, {Name: "edge_type"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"strength",
				"evidence",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *conceptEdgeRepo) SoftDeleteByConceptIDs(ctx context.Context, tx *gorm.DB, conceptIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(conceptIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).
		Where("from_concept_id IN ? OR to_concept_id IN ?", conceptIDs, conceptIDs).
		Delete(&types.ConceptEdge{}).Error
}

func (r *conceptEdgeRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.ConceptEdge{}).Error
}

func (r *conceptEdgeRepo) FullDeleteByConceptIDs(ctx context.Context, tx *gorm.DB, conceptIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(conceptIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().
		Where("from_concept_id IN ? OR to_concept_id IN ?", conceptIDs, conceptIDs).
		Delete(&types.ConceptEdge{}).Error
}

func (r *conceptEdgeRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.ConceptEdge{}).Error
}











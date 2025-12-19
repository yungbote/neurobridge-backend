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

type ConceptEvidenceRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.ConceptEvidence) ([]*types.ConceptEvidence, error)
	CreateIgnoreDuplicates(ctx context.Context, tx *gorm.DB, rows []*types.ConceptEvidence) (int, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.ConceptEvidence, error)
	GetByConceptIDs(ctx context.Context, tx *gorm.DB, conceptIDs []uuid.UUID) ([]*types.ConceptEvidence, error)
	GetByMaterialChunkIDs(ctx context.Context, tx *gorm.DB, chunkIDs []uuid.UUID) ([]*types.ConceptEvidence, error)

	Upsert(ctx context.Context, tx *gorm.DB, row *types.ConceptEvidence) error
	SoftDeleteByConceptIDs(ctx context.Context, tx *gorm.DB, conceptIDs []uuid.UUID) error
	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByConceptIDs(ctx context.Context, tx *gorm.DB, conceptIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
}

type conceptEvidenceRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewConceptEvidenceRepo(db *gorm.DB, baseLog *logger.Logger) ConceptEvidenceRepo {
	return &conceptEvidenceRepo{db: db, log: baseLog.With("repo", "ConceptEvidenceRepo")}
}

func (r *conceptEvidenceRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.ConceptEvidence) ([]*types.ConceptEvidence, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.ConceptEvidence{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *conceptEvidenceRepo) CreateIgnoreDuplicates(ctx context.Context, tx *gorm.DB, rows []*types.ConceptEvidence) (int, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return 0, nil
	}
	res := t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "concept_id"}, {Name: "material_chunk_id"}},
			DoNothing: true,
		}).
		Create(&rows)
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

func (r *conceptEvidenceRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.ConceptEvidence, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptEvidence
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptEvidenceRepo) GetByConceptIDs(ctx context.Context, tx *gorm.DB, conceptIDs []uuid.UUID) ([]*types.ConceptEvidence, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptEvidence
	if len(conceptIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("concept_id IN ?", conceptIDs).
		Order("concept_id ASC, created_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptEvidenceRepo) GetByMaterialChunkIDs(ctx context.Context, tx *gorm.DB, chunkIDs []uuid.UUID) ([]*types.ConceptEvidence, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptEvidence
	if len(chunkIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("material_chunk_id IN ?", chunkIDs).
		Order("material_chunk_id ASC, created_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptEvidenceRepo) Upsert(ctx context.Context, tx *gorm.DB, row *types.ConceptEvidence) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.ConceptID == uuid.Nil || row.MaterialChunkID == uuid.Nil {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	row.UpdatedAt = time.Now().UTC()

	return t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "concept_id"}, {Name: "material_chunk_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"kind",
				"weight",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *conceptEvidenceRepo) SoftDeleteByConceptIDs(ctx context.Context, tx *gorm.DB, conceptIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(conceptIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("concept_id IN ?", conceptIDs).Delete(&types.ConceptEvidence{}).Error
}

func (r *conceptEvidenceRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.ConceptEvidence{}).Error
}

func (r *conceptEvidenceRepo) FullDeleteByConceptIDs(ctx context.Context, tx *gorm.DB, conceptIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(conceptIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("concept_id IN ?", conceptIDs).Delete(&types.ConceptEvidence{}).Error
}

func (r *conceptEvidenceRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.ConceptEvidence{}).Error
}











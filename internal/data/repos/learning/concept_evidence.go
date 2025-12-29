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

type ConceptEvidenceRepo interface {
	Create(dbc dbctx.Context, rows []*types.ConceptEvidence) ([]*types.ConceptEvidence, error)
	CreateIgnoreDuplicates(dbc dbctx.Context, rows []*types.ConceptEvidence) (int, error)

	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.ConceptEvidence, error)
	GetByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) ([]*types.ConceptEvidence, error)
	GetByMaterialChunkIDs(dbc dbctx.Context, chunkIDs []uuid.UUID) ([]*types.ConceptEvidence, error)

	Upsert(dbc dbctx.Context, row *types.ConceptEvidence) error
	SoftDeleteByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) error
	SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	FullDeleteByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) error
	FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
}

type conceptEvidenceRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewConceptEvidenceRepo(db *gorm.DB, baseLog *logger.Logger) ConceptEvidenceRepo {
	return &conceptEvidenceRepo{db: db, log: baseLog.With("repo", "ConceptEvidenceRepo")}
}

func (r *conceptEvidenceRepo) Create(dbc dbctx.Context, rows []*types.ConceptEvidence) ([]*types.ConceptEvidence, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.ConceptEvidence{}, nil
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *conceptEvidenceRepo) CreateIgnoreDuplicates(dbc dbctx.Context, rows []*types.ConceptEvidence) (int, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return 0, nil
	}
	res := t.WithContext(dbc.Ctx).
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

func (r *conceptEvidenceRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.ConceptEvidence, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptEvidence
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptEvidenceRepo) GetByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) ([]*types.ConceptEvidence, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptEvidence
	if len(conceptIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("concept_id IN ?", conceptIDs).
		Order("concept_id ASC, created_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptEvidenceRepo) GetByMaterialChunkIDs(dbc dbctx.Context, chunkIDs []uuid.UUID) ([]*types.ConceptEvidence, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptEvidence
	if len(chunkIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("material_chunk_id IN ?", chunkIDs).
		Order("material_chunk_id ASC, created_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptEvidenceRepo) Upsert(dbc dbctx.Context, row *types.ConceptEvidence) error {
	t := dbc.Tx
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

	return t.WithContext(dbc.Ctx).
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

func (r *conceptEvidenceRepo) SoftDeleteByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(conceptIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("concept_id IN ?", conceptIDs).Delete(&types.ConceptEvidence{}).Error
}

func (r *conceptEvidenceRepo) SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("id IN ?", ids).Delete(&types.ConceptEvidence{}).Error
}

func (r *conceptEvidenceRepo) FullDeleteByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(conceptIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("concept_id IN ?", conceptIDs).Delete(&types.ConceptEvidence{}).Error
}

func (r *conceptEvidenceRepo) FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("id IN ?", ids).Delete(&types.ConceptEvidence{}).Error
}

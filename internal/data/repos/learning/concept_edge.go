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

type ConceptEdgeRepo interface {
	Create(dbc dbctx.Context, rows []*types.ConceptEdge) ([]*types.ConceptEdge, error)
	CreateIgnoreDuplicates(dbc dbctx.Context, rows []*types.ConceptEdge) (int, error)

	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.ConceptEdge, error)

	GetByFromConceptIDs(dbc dbctx.Context, fromIDs []uuid.UUID) ([]*types.ConceptEdge, error)
	GetByToConceptIDs(dbc dbctx.Context, toIDs []uuid.UUID) ([]*types.ConceptEdge, error)
	GetByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) ([]*types.ConceptEdge, error)

	Upsert(dbc dbctx.Context, row *types.ConceptEdge) error

	SoftDeleteByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) error
	SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	FullDeleteByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) error
	FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
}

type conceptEdgeRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewConceptEdgeRepo(db *gorm.DB, baseLog *logger.Logger) ConceptEdgeRepo {
	return &conceptEdgeRepo{db: db, log: baseLog.With("repo", "ConceptEdgeRepo")}
}

func (r *conceptEdgeRepo) Create(dbc dbctx.Context, rows []*types.ConceptEdge) ([]*types.ConceptEdge, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.ConceptEdge{}, nil
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *conceptEdgeRepo) CreateIgnoreDuplicates(dbc dbctx.Context, rows []*types.ConceptEdge) (int, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return 0, nil
	}
	res := t.WithContext(dbc.Ctx).
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

func (r *conceptEdgeRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.ConceptEdge, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptEdge
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptEdgeRepo) GetByFromConceptIDs(dbc dbctx.Context, fromIDs []uuid.UUID) ([]*types.ConceptEdge, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptEdge
	if len(fromIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("from_concept_id IN ?", fromIDs).
		Order("from_concept_id ASC, edge_type ASC, strength DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptEdgeRepo) GetByToConceptIDs(dbc dbctx.Context, toIDs []uuid.UUID) ([]*types.ConceptEdge, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptEdge
	if len(toIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("to_concept_id IN ?", toIDs).
		Order("to_concept_id ASC, edge_type ASC, strength DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptEdgeRepo) GetByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) ([]*types.ConceptEdge, error) {
	// union: from in ids OR to in ids
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptEdge
	if len(conceptIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("from_concept_id IN ? OR to_concept_id IN ?", conceptIDs, conceptIDs).
		Order("edge_type ASC, strength DESC, created_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptEdgeRepo) Upsert(dbc dbctx.Context, row *types.ConceptEdge) error {
	t := dbc.Tx
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

	return t.WithContext(dbc.Ctx).
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

func (r *conceptEdgeRepo) SoftDeleteByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(conceptIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).
		Where("from_concept_id IN ? OR to_concept_id IN ?", conceptIDs, conceptIDs).
		Delete(&types.ConceptEdge{}).Error
}

func (r *conceptEdgeRepo) SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("id IN ?", ids).Delete(&types.ConceptEdge{}).Error
}

func (r *conceptEdgeRepo) FullDeleteByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(conceptIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().
		Where("from_concept_id IN ? OR to_concept_id IN ?", conceptIDs, conceptIDs).
		Delete(&types.ConceptEdge{}).Error
}

func (r *conceptEdgeRepo) FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("id IN ?", ids).Delete(&types.ConceptEdge{}).Error
}

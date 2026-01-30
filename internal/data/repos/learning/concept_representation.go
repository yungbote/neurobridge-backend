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

type ConceptRepresentationRepo interface {
	Upsert(dbc dbctx.Context, row *types.ConceptRepresentation) error
	GetByPathConceptID(dbc dbctx.Context, pathConceptID uuid.UUID) (*types.ConceptRepresentation, error)
	ListByPathConceptIDs(dbc dbctx.Context, pathConceptIDs []uuid.UUID) ([]*types.ConceptRepresentation, error)
}

type conceptRepresentationRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewConceptRepresentationRepo(db *gorm.DB, baseLog *logger.Logger) ConceptRepresentationRepo {
	return &conceptRepresentationRepo{
		db:  db,
		log: baseLog.With("repo", "ConceptRepresentationRepo"),
	}
}

func (r *conceptRepresentationRepo) Upsert(dbc dbctx.Context, row *types.ConceptRepresentation) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	if row == nil || row.PathConceptID == uuid.Nil || row.CanonicalConceptID == uuid.Nil {
		return nil
	}
	now := time.Now().UTC()
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	row.UpdatedAt = now

	return transaction.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "path_concept_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"canonical_concept_id",
				"path_id",
				"representation_facets",
				"representation_summary",
				"representation_aliases",
				"mapping_confidence",
				"mapping_method",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *conceptRepresentationRepo) GetByPathConceptID(dbc dbctx.Context, pathConceptID uuid.UUID) (*types.ConceptRepresentation, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	if pathConceptID == uuid.Nil {
		return nil, nil
	}
	var row types.ConceptRepresentation
	err := transaction.WithContext(dbc.Ctx).
		Where("path_concept_id = ?", pathConceptID).
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

func (r *conceptRepresentationRepo) ListByPathConceptIDs(dbc dbctx.Context, pathConceptIDs []uuid.UUID) ([]*types.ConceptRepresentation, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	out := []*types.ConceptRepresentation{}
	if len(pathConceptIDs) == 0 {
		return out, nil
	}
	if err := transaction.WithContext(dbc.Ctx).
		Where("path_concept_id IN ?", pathConceptIDs).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

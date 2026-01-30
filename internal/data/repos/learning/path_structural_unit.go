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

type PathStructuralUnitRepo interface {
	Upsert(dbc dbctx.Context, row *types.PathStructuralUnit) error
	ListByPathID(dbc dbctx.Context, pathID uuid.UUID) ([]*types.PathStructuralUnit, error)
	ListByPathIDs(dbc dbctx.Context, pathIDs []uuid.UUID) ([]*types.PathStructuralUnit, error)
}

type pathStructuralUnitRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewPathStructuralUnitRepo(db *gorm.DB, baseLog *logger.Logger) PathStructuralUnitRepo {
	return &pathStructuralUnitRepo{
		db:  db,
		log: baseLog.With("repo", "PathStructuralUnitRepo"),
	}
}

func (r *pathStructuralUnitRepo) Upsert(dbc dbctx.Context, row *types.PathStructuralUnit) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	if row == nil || row.PathID == uuid.Nil || row.PsuKey == "" {
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
			Columns: []clause.Column{{Name: "path_id"}, {Name: "psu_key"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"pattern_kind",
				"member_node_ids",
				"structure_enc",
				"derived_canonical_concept_ids",
				"chain_signature_id",
				"local_role",
				"evidence_state",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *pathStructuralUnitRepo) ListByPathID(dbc dbctx.Context, pathID uuid.UUID) ([]*types.PathStructuralUnit, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	out := []*types.PathStructuralUnit{}
	if pathID == uuid.Nil {
		return out, nil
	}
	if err := transaction.WithContext(dbc.Ctx).
		Where("path_id = ?", pathID).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *pathStructuralUnitRepo) ListByPathIDs(dbc dbctx.Context, pathIDs []uuid.UUID) ([]*types.PathStructuralUnit, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	out := []*types.PathStructuralUnit{}
	if len(pathIDs) == 0 {
		return out, nil
	}
	if err := transaction.WithContext(dbc.Ctx).
		Where("path_id IN ?", pathIDs).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

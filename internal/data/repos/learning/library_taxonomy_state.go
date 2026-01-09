package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type LibraryTaxonomyStateRepo interface {
	GetByUserID(dbc dbctx.Context, userID uuid.UUID) (*types.LibraryTaxonomyState, error)
	Upsert(dbc dbctx.Context, row *types.LibraryTaxonomyState) error
	UpdateFields(dbc dbctx.Context, userID uuid.UUID, fields map[string]interface{}) error
}

type libraryTaxonomyStateRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLibraryTaxonomyStateRepo(db *gorm.DB, baseLog *logger.Logger) LibraryTaxonomyStateRepo {
	return &libraryTaxonomyStateRepo{db: db, log: baseLog.With("repo", "LibraryTaxonomyStateRepo")}
}

func (r *libraryTaxonomyStateRepo) GetByUserID(dbc dbctx.Context, userID uuid.UUID) (*types.LibraryTaxonomyState, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil {
		return nil, nil
	}
	var row types.LibraryTaxonomyState
	if err := t.WithContext(dbc.Ctx).Where("user_id = ?", userID).Limit(1).Find(&row).Error; err != nil {
		return nil, err
	}
	if row.ID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

func (r *libraryTaxonomyStateRepo) Upsert(dbc dbctx.Context, row *types.LibraryTaxonomyState) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.UserID == uuid.Nil {
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

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"version":                gorm.Expr("EXCLUDED.version"),
				"dirty":                  gorm.Expr("EXCLUDED.dirty"),
				"new_paths_since_refine": gorm.Expr("EXCLUDED.new_paths_since_refine"),
				"last_routed_at":         gorm.Expr("EXCLUDED.last_routed_at"),
				"last_refined_at":        gorm.Expr("EXCLUDED.last_refined_at"),
				"last_snapshot_built_at": gorm.Expr("EXCLUDED.last_snapshot_built_at"),
				"refine_lock_until":      gorm.Expr("EXCLUDED.refine_lock_until"),
				"pending_unsorted_paths": gorm.Expr("EXCLUDED.pending_unsorted_paths"),
				"updated_at":             now,
				"deleted_at":             nil,
			}),
		}).
		Create(row).Error
}

func (r *libraryTaxonomyStateRepo) UpdateFields(dbc dbctx.Context, userID uuid.UUID, fields map[string]interface{}) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil || len(fields) == 0 {
		return nil
	}
	fields["updated_at"] = time.Now().UTC()
	return t.WithContext(dbc.Ctx).Model(&types.LibraryTaxonomyState{}).Where("user_id = ?", userID).Updates(fields).Error
}

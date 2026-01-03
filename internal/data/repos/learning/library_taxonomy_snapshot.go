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

type LibraryTaxonomySnapshotRepo interface {
	GetByUserID(dbc dbctx.Context, userID uuid.UUID) (*types.LibraryTaxonomySnapshot, error)
	Upsert(dbc dbctx.Context, row *types.LibraryTaxonomySnapshot) error
}

type libraryTaxonomySnapshotRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLibraryTaxonomySnapshotRepo(db *gorm.DB, baseLog *logger.Logger) LibraryTaxonomySnapshotRepo {
	return &libraryTaxonomySnapshotRepo{db: db, log: baseLog.With("repo", "LibraryTaxonomySnapshotRepo")}
}

func (r *libraryTaxonomySnapshotRepo) GetByUserID(dbc dbctx.Context, userID uuid.UUID) (*types.LibraryTaxonomySnapshot, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil {
		return nil, nil
	}
	var row types.LibraryTaxonomySnapshot
	if err := t.WithContext(dbc.Ctx).Where("user_id = ?", userID).Limit(1).Find(&row).Error; err != nil {
		return nil, err
	}
	if row.ID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

func (r *libraryTaxonomySnapshotRepo) Upsert(dbc dbctx.Context, row *types.LibraryTaxonomySnapshot) error {
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
				"version":       gorm.Expr("EXCLUDED.version"),
				"snapshot_json": gorm.Expr("EXCLUDED.snapshot_json"),
				"updated_at":    now,
				"deleted_at":    nil,
			}),
		}).
		Create(row).Error
}


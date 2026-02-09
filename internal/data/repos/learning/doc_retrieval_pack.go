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

type DocRetrievalPackRepo interface {
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.DocRetrievalPack, error)
	GetByNodeAndPackID(dbc dbctx.Context, pathNodeID uuid.UUID, packID string) (*types.DocRetrievalPack, error)
	GetLatestByNode(dbc dbctx.Context, pathNodeID uuid.UUID) (*types.DocRetrievalPack, error)
	Upsert(dbc dbctx.Context, row *types.DocRetrievalPack) error
}

type docRetrievalPackRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewDocRetrievalPackRepo(db *gorm.DB, baseLog *logger.Logger) DocRetrievalPackRepo {
	return &docRetrievalPackRepo{db: db, log: baseLog.With("repo", "DocRetrievalPackRepo")}
}

func (r *docRetrievalPackRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.DocRetrievalPack, error) {
	if id == uuid.Nil {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.DocRetrievalPack
	if err := t.WithContext(dbc.Ctx).First(&out, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

func (r *docRetrievalPackRepo) GetByNodeAndPackID(dbc dbctx.Context, pathNodeID uuid.UUID, packID string) (*types.DocRetrievalPack, error) {
	if pathNodeID == uuid.Nil || packID == "" {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.DocRetrievalPack
	if err := t.WithContext(dbc.Ctx).
		First(&out, "path_node_id = ? AND pack_id = ?", pathNodeID, packID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

func (r *docRetrievalPackRepo) GetLatestByNode(dbc dbctx.Context, pathNodeID uuid.UUID) (*types.DocRetrievalPack, error) {
	if pathNodeID == uuid.Nil {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.DocRetrievalPack
	if err := t.WithContext(dbc.Ctx).
		Where("path_node_id = ?", pathNodeID).
		Order("created_at DESC").
		Limit(1).
		Find(&out).Error; err != nil {
		return nil, err
	}
	if out.ID == uuid.Nil {
		return nil, nil
	}
	return &out, nil
}

func (r *docRetrievalPackRepo) Upsert(dbc dbctx.Context, row *types.DocRetrievalPack) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.PathID == uuid.Nil || row.PathNodeID == uuid.Nil {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "path_node_id"}, {Name: "pack_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"path_id",
				"policy_version",
				"blueprint_version",
				"schema_version",
				"pack_json",
			}),
		}).
		Create(row).Error
}

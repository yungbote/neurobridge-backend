package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type MaterialFileSignatureRepo interface {
	UpsertByMaterialFileID(dbc dbctx.Context, row *types.MaterialFileSignature) error
	GetByMaterialFileIDs(dbc dbctx.Context, fileIDs []uuid.UUID) ([]*types.MaterialFileSignature, error)
	GetByMaterialSetID(dbc dbctx.Context, setID uuid.UUID) ([]*types.MaterialFileSignature, error)
	UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error
}

type materialFileSignatureRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewMaterialFileSignatureRepo(db *gorm.DB, baseLog *logger.Logger) MaterialFileSignatureRepo {
	return &materialFileSignatureRepo{
		db:  db,
		log: baseLog.With("repo", "MaterialFileSignatureRepo"),
	}
}

func (r *materialFileSignatureRepo) UpsertByMaterialFileID(dbc dbctx.Context, row *types.MaterialFileSignature) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.MaterialFileID == uuid.Nil {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	row.UpdatedAt = time.Now().UTC()

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "material_file_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"material_set_id",
				"version",
				"language",
				"quality",
				"difficulty",
				"domain_tags",
				"topics",
				"concept_keys",
				"summary_md",
				"summary_embedding",
				"outline_json",
				"outline_confidence",
				"citations",
				"fingerprint",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *materialFileSignatureRepo) GetByMaterialFileIDs(dbc dbctx.Context, fileIDs []uuid.UUID) ([]*types.MaterialFileSignature, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.MaterialFileSignature
	if len(fileIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("material_file_id IN ?", fileIDs).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *materialFileSignatureRepo) GetByMaterialSetID(dbc dbctx.Context, setID uuid.UUID) ([]*types.MaterialFileSignature, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.MaterialFileSignature
	if setID == uuid.Nil {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("material_set_id = ?", setID).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *materialFileSignatureRepo) UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error {
	t := dbc.Tx
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
	return t.WithContext(dbc.Ctx).
		Model(&types.MaterialFileSignature{}).
		Where("id = ?", id).
		Updates(updates).Error
}

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

type MaterialFileSectionRepo interface {
	BulkUpsert(dbc dbctx.Context, rows []*types.MaterialFileSection) error
	DeleteByMaterialFileID(dbc dbctx.Context, fileID uuid.UUID) error
	GetByMaterialFileIDs(dbc dbctx.Context, fileIDs []uuid.UUID) ([]*types.MaterialFileSection, error)
}

type materialFileSectionRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewMaterialFileSectionRepo(db *gorm.DB, baseLog *logger.Logger) MaterialFileSectionRepo {
	return &materialFileSectionRepo{
		db:  db,
		log: baseLog.With("repo", "MaterialFileSectionRepo"),
	}
}

func (r *materialFileSectionRepo) BulkUpsert(dbc dbctx.Context, rows []*types.MaterialFileSection) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return nil
	}
	now := time.Now().UTC()
	for _, row := range rows {
		if row == nil {
			continue
		}
		if row.ID == uuid.Nil {
			row.ID = uuid.New()
		}
		if row.CreatedAt.IsZero() {
			row.CreatedAt = now
		}
		row.UpdatedAt = now
	}
	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "material_file_id"}, {Name: "section_index"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"title",
				"path",
				"start_page",
				"end_page",
				"start_sec",
				"end_sec",
				"text_excerpt",
				"embedding",
				"metadata",
				"updated_at",
			}),
		}).
		Create(&rows).Error
}

func (r *materialFileSectionRepo) DeleteByMaterialFileID(dbc dbctx.Context, fileID uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if fileID == uuid.Nil {
		return nil
	}
	return t.WithContext(dbc.Ctx).
		Where("material_file_id = ?", fileID).
		Delete(&types.MaterialFileSection{}).Error
}

func (r *materialFileSectionRepo) GetByMaterialFileIDs(dbc dbctx.Context, fileIDs []uuid.UUID) ([]*types.MaterialFileSection, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.MaterialFileSection
	if len(fileIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("material_file_id IN ?", fileIDs).
		Order("material_file_id ASC, section_index ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

package materials

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type MaterialFileRepo interface {
	Create(dbc dbctx.Context, files []*types.MaterialFile) ([]*types.MaterialFile, error)
	GetByIDs(dbc dbctx.Context, fileIDs []uuid.UUID) ([]*types.MaterialFile, error)
	GetByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) ([]*types.MaterialFile, error)
	GetByMaterialSetID(dbc dbctx.Context, setID uuid.UUID) ([]*types.MaterialFile, error)
	SoftDeleteByIDs(dbc dbctx.Context, fileIDs []uuid.UUID) error
	SoftDeleteByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) error
	FullDeleteByIDs(dbc dbctx.Context, fileIDs []uuid.UUID) error
	FullDeleteByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) error
}

type materialFileRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewMaterialFileRepo(db *gorm.DB, baseLog *logger.Logger) MaterialFileRepo {
	repoLog := baseLog.With("repo", "MaterialFileRepo")
	return &materialFileRepo{db: db, log: repoLog}
}

func (r *materialFileRepo) Create(dbc dbctx.Context, files []*types.MaterialFile) ([]*types.MaterialFile, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	if len(files) == 0 {
		return []*types.MaterialFile{}, nil
	}

	if err := transaction.WithContext(dbc.Ctx).Create(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}

func (r *materialFileRepo) GetByIDs(dbc dbctx.Context, fileIDs []uuid.UUID) ([]*types.MaterialFile, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.MaterialFile
	if len(fileIDs) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Where("id IN ?", fileIDs).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *materialFileRepo) GetByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) ([]*types.MaterialFile, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	if len(setIDs) == 0 {
		return []*types.MaterialFile{}, nil
	}

	// Back-compat: if migrations haven't created the join table yet, fall back to the legacy behavior.
	if !transaction.Migrator().HasTable(&types.MaterialSetFile{}) {
		var results []*types.MaterialFile
		if err := transaction.WithContext(dbc.Ctx).
			Where("material_set_id IN ?", setIDs).
			Find(&results).Error; err != nil {
			return nil, err
		}
		return results, nil
	}

	// Derived material sets declare membership via material_set_file rows.
	// Legacy upload batches use material_file.material_set_id.
	type linkRow struct {
		MaterialSetID  uuid.UUID `gorm:"column:material_set_id"`
		MaterialFileID uuid.UUID `gorm:"column:material_file_id"`
	}

	type setRow struct {
		ID                  uuid.UUID  `gorm:"column:id"`
		SourceMaterialSetID *uuid.UUID `gorm:"column:source_material_set_id"`
	}

	var setRows []setRow
	if err := transaction.WithContext(dbc.Ctx).
		Model(&types.MaterialSet{}).
		Select("id, source_material_set_id").
		Where("id IN ?", setIDs).
		Find(&setRows).Error; err != nil {
		if r.log != nil {
			r.log.Warn("GetByMaterialSetIDs: failed to load material_set rows; falling back to legacy membership", "error", err)
		}
		var results []*types.MaterialFile
		if err := transaction.WithContext(dbc.Ctx).
			Where("material_set_id IN ?", setIDs).
			Find(&results).Error; err != nil {
			return nil, err
		}
		return results, nil
	}

	derivedSetIDs := make([]uuid.UUID, 0, len(setRows))
	legacySetIDs := make([]uuid.UUID, 0, len(setRows))
	seenSet := map[uuid.UUID]bool{}
	for _, row := range setRows {
		if row.ID == uuid.Nil {
			continue
		}
		seenSet[row.ID] = true
		if row.SourceMaterialSetID != nil && *row.SourceMaterialSetID != uuid.Nil {
			derivedSetIDs = append(derivedSetIDs, row.ID)
		} else {
			legacySetIDs = append(legacySetIDs, row.ID)
		}
	}
	for _, sid := range setIDs {
		if sid == uuid.Nil || seenSet[sid] {
			continue
		}
		legacySetIDs = append(legacySetIDs, sid)
	}

	var links []linkRow
	if len(derivedSetIDs) > 0 {
		if err := transaction.WithContext(dbc.Ctx).
			Model(&types.MaterialSetFile{}).
			Select("material_set_id, material_file_id").
			Where("material_set_id IN ?", derivedSetIDs).
			Find(&links).Error; err != nil {
			return nil, err
		}
	}

	fileIDs := make([]uuid.UUID, 0, len(links))
	seenFile := map[uuid.UUID]bool{}
	for _, l := range links {
		if l.MaterialSetID == uuid.Nil || l.MaterialFileID == uuid.Nil {
			continue
		}
		if !seenFile[l.MaterialFileID] {
			seenFile[l.MaterialFileID] = true
			fileIDs = append(fileIDs, l.MaterialFileID)
		}
	}

	results := make([]*types.MaterialFile, 0, 64)

	// 1) Legacy: files are owned by the set.
	if len(legacySetIDs) > 0 {
		var direct []*types.MaterialFile
		if err := transaction.WithContext(dbc.Ctx).
			Where("material_set_id IN ?", legacySetIDs).
			Find(&direct).Error; err != nil {
			return nil, err
		}
		results = append(results, direct...)
	}

	// 2) Derived: membership links point at existing files.
	if len(fileIDs) > 0 {
		var linked []*types.MaterialFile
		if err := transaction.WithContext(dbc.Ctx).
			Where("id IN ?", fileIDs).
			Find(&linked).Error; err != nil {
			return nil, err
		}
		results = append(results, linked...)
	}

	// De-dupe by file ID (defensive).
	if len(results) <= 1 {
		return results, nil
	}
	byID := map[uuid.UUID]*types.MaterialFile{}
	for _, f := range results {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		if byID[f.ID] == nil {
			byID[f.ID] = f
		}
	}
	out := make([]*types.MaterialFile, 0, len(byID))
	for _, f := range byID {
		out = append(out, f)
	}
	return out, nil
}

func (r *materialFileRepo) GetByMaterialSetID(dbc dbctx.Context, setID uuid.UUID) ([]*types.MaterialFile, error) {
	return r.GetByMaterialSetIDs(dbc, []uuid.UUID{setID})
}

func (r *materialFileRepo) SoftDeleteByIDs(dbc dbctx.Context, fileIDs []uuid.UUID) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	if len(fileIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Where("id IN ?", fileIDs).
		Delete(&types.MaterialFile{}).Error; err != nil {
		return err
	}
	return nil
}

func (r *materialFileRepo) SoftDeleteByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	if len(setIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Where("material_set_id IN ?", setIDs).
		Delete(&types.MaterialFile{}).Error; err != nil {
		return err
	}
	return nil
}

func (r *materialFileRepo) FullDeleteByIDs(dbc dbctx.Context, fileIDs []uuid.UUID) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	if len(fileIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Unscoped().
		Where("id IN ?", fileIDs).
		Delete(&types.MaterialFile{}).Error; err != nil {
		return err
	}
	return nil
}

func (r *materialFileRepo) FullDeleteByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	if len(setIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Unscoped().
		Where("material_set_id IN ?", setIDs).
		Delete(&types.MaterialFile{}).Error; err != nil {
		return err
	}
	return nil
}

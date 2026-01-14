package materialsetctx

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

// SetContext describes how to interpret a material_set in a world where we support
// "derived material sets" (subsets of an upload batch).
//
// EffectiveMaterialSetID: the material_set_id the caller is operating on (e.g., a subpath's set).
// SourceMaterialSetID: the upload batch that owns the physical files/chunks and chunk vector namespace.
// AllowFileIDs: for derived sets, a material_file allowlist that restricts retrieval/graph expansion.
type SetContext struct {
	EffectiveMaterialSetID uuid.UUID
	SourceMaterialSetID    uuid.UUID
	IsDerived              bool
	AllowFileIDs           map[uuid.UUID]bool
}

func Resolve(ctx context.Context, db *gorm.DB, materialSetID uuid.UUID) (SetContext, error) {
	out := SetContext{
		EffectiveMaterialSetID: materialSetID,
		SourceMaterialSetID:    materialSetID,
		IsDerived:              false,
		AllowFileIDs:           nil,
	}
	if db == nil || materialSetID == uuid.Nil {
		return out, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var ms types.MaterialSet
	if err := db.WithContext(ctx).
		Model(&types.MaterialSet{}).
		Select("id, source_material_set_id").
		Where("id = ?", materialSetID).
		Limit(1).
		Find(&ms).Error; err != nil {
		return out, err
	}
	if ms.ID == uuid.Nil {
		// Treat unknown as non-derived.
		return out, nil
	}

	if ms.SourceMaterialSetID == nil || *ms.SourceMaterialSetID == uuid.Nil {
		return out, nil
	}

	out.IsDerived = true
	out.SourceMaterialSetID = *ms.SourceMaterialSetID

	// Load membership allowlist (required for correct retrieval in the shared source namespace).
	type linkRow struct {
		MaterialFileID uuid.UUID `gorm:"column:material_file_id"`
	}
	var links []linkRow
	if err := db.WithContext(ctx).
		Model(&types.MaterialSetFile{}).
		Select("material_file_id").
		Where("material_set_id = ?", materialSetID).
		Find(&links).Error; err != nil {
		return out, err
	}

	allow := make(map[uuid.UUID]bool, len(links))
	for _, l := range links {
		if l.MaterialFileID != uuid.Nil {
			allow[l.MaterialFileID] = true
		}
	}
	if len(allow) == 0 {
		return out, fmt.Errorf("derived material_set has no members: %s", materialSetID.String())
	}
	out.AllowFileIDs = allow
	return out, nil
}

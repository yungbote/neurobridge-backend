package steps

import (
	"sort"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func intakeMaterialAllowlistFromPathMeta(meta map[string]any) map[uuid.UUID]bool {
	if meta == nil {
		return nil
	}
	mf := mapFromAny(meta["intake_material_filter"])
	if mf == nil {
		return nil
	}
	ids := uuidSliceFromStrings(stringSliceFromAny(mf["include_file_ids"]))
	if len(ids) == 0 {
		return nil
	}
	out := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		if id != uuid.Nil {
			out[id] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func filterMaterialFilesByAllowlist(files []*types.MaterialFile, allow map[uuid.UUID]bool) []*types.MaterialFile {
	if len(allow) == 0 {
		return files
	}
	out := make([]*types.MaterialFile, 0, len(files))
	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		if !allow[f.ID] {
			continue
		}
		out = append(out, f)
	}
	return out
}

func pineconeChunkFilterWithAllowlist(allow map[uuid.UUID]bool) map[string]any {
	filter := map[string]any{"type": "chunk"}
	if len(allow) == 0 {
		return filter
	}
	ids := make([]string, 0, len(allow))
	for id := range allow {
		if id == uuid.Nil {
			continue
		}
		ids = append(ids, id.String())
	}
	sort.Strings(ids)
	if len(ids) > 0 {
		filter["material_file_id"] = map[string]any{"$in": ids}
	}
	return filter
}

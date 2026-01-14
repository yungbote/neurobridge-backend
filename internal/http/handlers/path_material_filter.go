package handlers

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

func materialFileAllowlistFromPathMetaJSON(metaJSON []byte) map[uuid.UUID]bool {
	if len(metaJSON) == 0 {
		return nil
	}
	raw := strings.TrimSpace(string(metaJSON))
	if raw == "" || raw == "null" {
		return nil
	}

	var meta map[string]any
	if err := json.Unmarshal(metaJSON, &meta); err != nil || meta == nil {
		return nil
	}
	mf, ok := meta["intake_material_filter"].(map[string]any)
	if !ok || mf == nil {
		return nil
	}
	arr, ok := mf["include_file_ids"].([]any)
	if !ok || len(arr) == 0 {
		return nil
	}
	out := map[uuid.UUID]bool{}
	for _, v := range arr {
		s := strings.TrimSpace(strings.TrimSpace(stringFromAny(v)))
		if s == "" {
			continue
		}
		id, err := uuid.Parse(s)
		if err != nil || id == uuid.Nil {
			continue
		}
		out[id] = true
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

package steps

import (
	"encoding/json"
	"sort"
	"strings"
)

// IntakePathsBriefJSONFromPathMeta returns a compact JSON blob suitable for downstream planning prompts.
// It is intentionally lossy (names over ids) to keep token usage low.
func IntakePathsBriefJSONFromPathMeta(meta map[string]any, maxFilesPerPath int) string {
	if meta == nil {
		return ""
	}
	intake := mapFromAny(meta["intake"])
	if intake == nil {
		return ""
	}
	ma := mapFromAny(intake["material_alignment"])
	mode := strings.ToLower(strings.TrimSpace(stringFromAny(ma["mode"])))

	paths := sliceAny(intake["paths"])
	if len(paths) == 0 {
		return ""
	}
	if maxFilesPerPath <= 0 {
		maxFilesPerPath = 4
	}

	// Only include the brief blob when it materially affects planning (multi-path).
	if mode != "multi_goal" && len(paths) <= 1 {
		return ""
	}

	// Map file_id -> display name (from intake.file_intents).
	nameByID := map[string]string{}
	for _, it := range sliceAny(intake["file_intents"]) {
		m, ok := it.(map[string]any)
		if !ok || m == nil {
			continue
		}
		id := strings.TrimSpace(stringFromAny(m["file_id"]))
		name := strings.TrimSpace(stringFromAny(m["original_name"]))
		if id == "" || name == "" {
			continue
		}
		nameByID[id] = name
	}

	namesForIDs := func(ids []string, max int) []string {
		out := make([]string, 0, min(max, len(ids)))
		seen := map[string]bool{}
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			name := strings.TrimSpace(nameByID[id])
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
			if len(out) >= max {
				break
			}
		}
		return out
	}

	outPaths := make([]map[string]any, 0, len(paths))
	for _, p := range paths {
		m, ok := p.(map[string]any)
		if !ok || m == nil {
			continue
		}
		pathID := strings.TrimSpace(stringFromAny(m["path_id"]))
		if pathID == "" {
			pathID = strings.TrimSpace(stringFromAny(m["id"]))
		}
		title := strings.TrimSpace(stringFromAny(m["title"]))
		goal := strings.TrimSpace(stringFromAny(m["goal"]))
		if title == "" && goal == "" {
			continue
		}
		core := dedupeStrings(stringSliceFromAny(m["core_file_ids"]))
		support := dedupeStrings(stringSliceFromAny(m["support_file_ids"]))
		conf := floatFromAny(m["confidence"], 0)
		notes := strings.TrimSpace(stringFromAny(m["notes"]))

		outPaths = append(outPaths, map[string]any{
			"path_id":       pathID,
			"title":         title,
			"goal":          goal,
			"core_files":    namesForIDs(core, maxFilesPerPath),
			"support_files": namesForIDs(support, maxFilesPerPath),
			"confidence":    conf,
			"notes":         notes,
		})
	}
	if len(outPaths) == 0 {
		return ""
	}

	// Stabilize ordering for determinism.
	sort.Slice(outPaths, func(i, j int) bool {
		ai := strings.TrimSpace(stringFromAny(outPaths[i]["path_id"]))
		aj := strings.TrimSpace(stringFromAny(outPaths[j]["path_id"]))
		if ai != "" && aj != "" && ai != aj {
			return ai < aj
		}
		ti := strings.TrimSpace(stringFromAny(outPaths[i]["title"]))
		tj := strings.TrimSpace(stringFromAny(outPaths[j]["title"]))
		return ti < tj
	})

	out := map[string]any{
		"mode":            mode,
		"combined_goal":   strings.TrimSpace(stringFromAny(intake["combined_goal"])),
		"primary_path_id": strings.TrimSpace(stringFromAny(intake["primary_path_id"])),
		"paths":           outPaths,
	}
	b, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(b)
}

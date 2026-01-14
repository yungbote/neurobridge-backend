package steps

import (
	"encoding/json"
	"sort"
	"strings"
)

// IntakeTracksBriefJSONFromPathMeta returns a compact JSON blob suitable for downstream planning prompts.
// It is intentionally lossy (names over ids) to keep token usage low.
func IntakeTracksBriefJSONFromPathMeta(meta map[string]any, maxFilesPerTrack int) string {
	if meta == nil {
		return ""
	}
	intake := mapFromAny(meta["intake"])
	if intake == nil {
		return ""
	}
	ma := mapFromAny(intake["material_alignment"])
	mode := strings.ToLower(strings.TrimSpace(stringFromAny(ma["mode"])))

	tracks := sliceAny(intake["tracks"])
	if len(tracks) == 0 {
		return ""
	}
	if maxFilesPerTrack <= 0 {
		maxFilesPerTrack = 4
	}

	// Only include the brief blob when it materially affects planning (multi-track).
	if mode != "multi_goal" && len(tracks) <= 1 {
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

	outTracks := make([]map[string]any, 0, len(tracks))
	for _, tr := range tracks {
		m, ok := tr.(map[string]any)
		if !ok || m == nil {
			continue
		}
		trackID := strings.TrimSpace(stringFromAny(m["track_id"]))
		if trackID == "" {
			trackID = strings.TrimSpace(stringFromAny(m["id"]))
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

		outTracks = append(outTracks, map[string]any{
			"track_id":      trackID,
			"title":         title,
			"goal":          goal,
			"core_files":    namesForIDs(core, maxFilesPerTrack),
			"support_files": namesForIDs(support, maxFilesPerTrack),
			"confidence":    conf,
			"notes":         notes,
		})
	}
	if len(outTracks) == 0 {
		return ""
	}

	// Stabilize ordering for determinism.
	sort.Slice(outTracks, func(i, j int) bool {
		ai := strings.TrimSpace(stringFromAny(outTracks[i]["track_id"]))
		aj := strings.TrimSpace(stringFromAny(outTracks[j]["track_id"]))
		if ai != "" && aj != "" && ai != aj {
			return ai < aj
		}
		ti := strings.TrimSpace(stringFromAny(outTracks[i]["title"]))
		tj := strings.TrimSpace(stringFromAny(outTracks[j]["title"]))
		return ti < tj
	})

	out := map[string]any{
		"mode":             mode,
		"combined_goal":    strings.TrimSpace(stringFromAny(intake["combined_goal"])),
		"primary_track_id": strings.TrimSpace(stringFromAny(intake["primary_track_id"])),
		"tracks":           outTracks,
	}
	b, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(b)
}

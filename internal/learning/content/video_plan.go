package content

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

type VideoPlanDocV1 struct {
	SchemaVersion int               `json:"schema_version"`
	Videos        []VideoPlanItemV1 `json:"videos"`
}

type VideoPlanItemV1 struct {
	SemanticType  string   `json:"semantic_type"`
	Prompt        string   `json:"prompt"`
	Caption       string   `json:"caption"`
	AltText       string   `json:"alt_text"`
	PlacementHint string   `json:"placement_hint"`
	DurationSec   int      `json:"duration_sec"`
	Citations     []string `json:"citations"`
}

func ValidateVideoPlanV1(doc VideoPlanDocV1, allowedChunkIDs map[string]bool, subjectHints []string) ([]string, map[string]any) {
	errs := make([]string, 0)

	if doc.SchemaVersion != 1 {
		errs = append(errs, fmt.Sprintf("schema_version must be 1 (got %d)", doc.SchemaVersion))
	}

	// Keep cost + latency bounded: max 1 video per node for now.
	if len(doc.Videos) > 1 {
		errs = append(errs, fmt.Sprintf("too many videos (%d > 1)", len(doc.Videos)))
	}

	semOK := map[string]bool{
		"real_world_demo":     true,
		"process_animation":   true,
		"intuition_animation": true,
		"spatial_walkthrough": true,
		"before_after":        true,
	}
	allowedDurations := map[int]bool{
		4:  true,
		8:  true,
		12: true,
	}

	containsAny := func(haystack string, needles []string) bool {
		for _, n := range needles {
			if n == "" {
				continue
			}
			if strings.Contains(haystack, n) {
				return true
			}
		}
		return false
	}

	prompts := 0
	subjectHints = NormalizeConceptKeys(subjectHints)
	subjectHits := 0
	durationTotal := 0

	for i := range doc.Videos {
		v := doc.Videos[i]
		prefix := fmt.Sprintf("videos[%d]", i)

		st := strings.TrimSpace(v.SemanticType)
		if !semOK[st] {
			errs = append(errs, prefix+".semantic_type invalid")
		}

		if strings.TrimSpace(v.Prompt) == "" {
			errs = append(errs, prefix+".prompt missing")
		} else {
			prompts++
			lp := strings.ToLower(v.Prompt)
			if !strings.Contains(lp, "no watermarks") {
				errs = append(errs, prefix+".prompt must include 'no watermarks'")
			}
			if !strings.Contains(lp, "no logos") {
				errs = append(errs, prefix+".prompt must include 'no logos'")
			}
			if !containsAny(lp, []string{"no brand names", "no brands"}) {
				errs = append(errs, prefix+".prompt must include 'no brand names'")
			}
			if !containsAny(lp, []string{"avoid identifiable people", "avoid identifiable faces", "no identifiable faces", "no faces"}) {
				errs = append(errs, prefix+".prompt must avoid identifiable people/faces")
			}

			// Ensure prompts are grounded in concrete subjects pulled from the provided text (noun/thing based).
			if len(subjectHints) > 0 {
				combined := strings.ToLower(strings.TrimSpace(v.Prompt + "\n" + v.Caption + "\n" + v.AltText))
				found := false
				for _, s := range subjectHints {
					s = strings.ToLower(strings.TrimSpace(s))
					if s == "" {
						continue
					}
					if strings.Contains(combined, s) {
						found = true
						break
					}
				}
				if !found {
					errs = append(errs, prefix+".prompt/caption must include at least one subject from VIDEO_SUBJECT_CANDIDATES")
				} else {
					subjectHits++
				}
			}
		}

		if strings.TrimSpace(v.Caption) == "" {
			errs = append(errs, prefix+".caption missing")
		}
		if strings.TrimSpace(v.AltText) == "" {
			errs = append(errs, prefix+".alt_text missing")
		}
		if strings.TrimSpace(v.PlacementHint) == "" {
			errs = append(errs, prefix+".placement_hint missing")
		}

		if !allowedDurations[v.DurationSec] {
			errs = append(errs, prefix+".duration_sec must be 4, 8, or 12")
		} else {
			durationTotal += v.DurationSec
		}

		if len(v.Citations) == 0 {
			errs = append(errs, prefix+".citations missing")
			continue
		}
		seen := map[string]bool{}
		for _, cid := range v.Citations {
			cid = strings.TrimSpace(cid)
			if cid == "" {
				errs = append(errs, prefix+".citations contains empty chunk_id")
				continue
			}
			if seen[cid] {
				continue
			}
			seen[cid] = true
			if _, err := uuid.Parse(cid); err != nil {
				errs = append(errs, prefix+".citations contains invalid uuid: "+cid)
				continue
			}
			if allowedChunkIDs != nil && len(allowedChunkIDs) > 0 && !allowedChunkIDs[cid] {
				errs = append(errs, prefix+".citations contains chunk_id not allowed: "+cid)
			}
		}
	}

	metrics := map[string]any{
		"videos_count":   len(doc.Videos),
		"has_prompt":     prompts > 0,
		"subject_hits":   subjectHits,
		"duration_total": durationTotal,
	}

	return dedupeStrings(errs), metrics
}

func VideoPlanChunkIDs(doc VideoPlanDocV1) []string {
	out := make([]string, 0)
	seen := map[string]bool{}
	for _, v := range doc.Videos {
		for _, id := range v.Citations {
			id = strings.TrimSpace(id)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

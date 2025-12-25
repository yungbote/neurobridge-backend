package content

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

type FigurePlanDocV1 struct {
	SchemaVersion int                `json:"schema_version"`
	Figures       []FigurePlanItemV1 `json:"figures"`
}

type FigurePlanItemV1 struct {
	SemanticType  string   `json:"semantic_type"`
	Prompt        string   `json:"prompt"`
	Caption       string   `json:"caption"`
	AltText       string   `json:"alt_text"`
	PlacementHint string   `json:"placement_hint"`
	Citations     []string `json:"citations"`
}

func ValidateFigurePlanV1(doc FigurePlanDocV1, allowedChunkIDs map[string]bool, subjectHints []string) ([]string, map[string]any) {
	errs := make([]string, 0)

	if doc.SchemaVersion != 1 {
		errs = append(errs, fmt.Sprintf("schema_version must be 1 (got %d)", doc.SchemaVersion))
	}

	if len(doc.Figures) > 2 {
		errs = append(errs, fmt.Sprintf("too many figures (%d > 2)", len(doc.Figures)))
	}

	semOK := map[string]bool{
		"setup_illustration":  true,
		"real_world_example":  true,
		"intuition_picture":   true,
		"anatomy_schematic":   true,
		"graphical_metaphor":  true,
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
	for i := range doc.Figures {
		f := doc.Figures[i]
		prefix := fmt.Sprintf("figures[%d]", i)

		st := strings.TrimSpace(f.SemanticType)
		if !semOK[st] {
			errs = append(errs, prefix+".semantic_type invalid")
		}
		if strings.TrimSpace(f.Prompt) == "" {
			errs = append(errs, prefix+".prompt missing")
		} else {
			prompts++
			// Planner should produce photorealistic (non-diagram) prompts with explicit guardrails.
			lp := strings.ToLower(f.Prompt)
			if !strings.Contains(lp, "no text") {
				errs = append(errs, prefix+".prompt must include 'no text' (labels go in captions, not in-image)")
			}
			if !containsAny(lp, []string{"not a diagram", "not diagram", "not a schematic", "not schematic", "not an infographic", "not infographic"}) {
				errs = append(errs, prefix+".prompt must state it's NOT a diagram/schematic/infographic")
			}
			if !containsAny(lp, []string{"photorealistic", "photograph", "photo", "realistic lighting", "high-resolution", "high resolution", "stock photo", "dslr", "macro"}) {
				errs = append(errs, prefix+".prompt must request a photorealistic/high-fidelity style (photo-like)")
			}
			if !strings.Contains(lp, "no watermarks") {
				errs = append(errs, prefix+".prompt must include 'no watermarks'")
			}
			if !strings.Contains(lp, "no logos") {
				errs = append(errs, prefix+".prompt must include 'no logos'")
			}
			if !strings.Contains(lp, "no brand") {
				errs = append(errs, prefix+".prompt must include 'no brand names'")
			}

			// Ensure prompts are grounded in concrete subjects pulled from the provided text (noun/thing based).
			if len(subjectHints) > 0 {
				combined := strings.ToLower(strings.TrimSpace(f.Prompt + "\n" + f.Caption + "\n" + f.AltText))
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
					errs = append(errs, prefix+".prompt/caption must include at least one subject from VISUAL_SUBJECT_CANDIDATES")
				} else {
					subjectHits++
				}
			}
		}
		if strings.TrimSpace(f.Caption) == "" {
			errs = append(errs, prefix+".caption missing")
		}
		if strings.TrimSpace(f.AltText) == "" {
			errs = append(errs, prefix+".alt_text missing")
		}
		if strings.TrimSpace(f.PlacementHint) == "" {
			errs = append(errs, prefix+".placement_hint missing")
		}

		if len(f.Citations) == 0 {
			errs = append(errs, prefix+".citations missing")
			continue
		}
		seen := map[string]bool{}
		for _, cid := range f.Citations {
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
		"figures_count": len(doc.Figures),
		"has_prompt":    prompts > 0,
		"subject_hits":  subjectHits,
	}

	return dedupeStrings(errs), metrics
}

func FigurePlanChunkIDs(doc FigurePlanDocV1) []string {
	out := make([]string, 0)
	seen := map[string]bool{}
	for _, f := range doc.Figures {
		for _, id := range f.Citations {
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

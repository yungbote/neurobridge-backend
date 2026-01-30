package steps

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

type ConceptKnowledgeV1 struct {
	Key                string   `json:"key"`
	CanonicalConceptID string   `json:"canonical_concept_id,omitempty"`
	Mastery            float64  `json:"mastery"`
	Confidence         float64  `json:"confidence"`
	Attempts           int      `json:"attempts,omitempty"`
	Correct            int      `json:"correct,omitempty"`
	LastSeenAt         string   `json:"last_seen_at,omitempty"`
	NextReviewAt       string   `json:"next_review_at,omitempty"`
	Status             string   `json:"status"` // known|learning|weak|unseen
	MisconceptionHints []string `json:"misconception_hints,omitempty"`
	Active             bool     `json:"active,omitempty"`
}

type UserKnowledgeContextV1 struct {
	Version              int                  `json:"version"`
	KnownConceptKeys     []string             `json:"known_concept_keys,omitempty"`
	WeakConceptKeys      []string             `json:"weak_concept_keys,omitempty"`
	DueReviewConceptKeys []string             `json:"due_review_concept_keys,omitempty"`
	UnseenConceptKeys    []string             `json:"unseen_concept_keys,omitempty"`
	Concepts             []ConceptKnowledgeV1 `json:"concepts,omitempty"`
	KnownRatio           float64              `json:"known_ratio,omitempty"`
	SeenOrAssessedRatio  float64              `json:"seen_or_assessed_ratio,omitempty"`
}

type UserKnowledgeContextV2 struct {
	Version              int                  `json:"version"`
	KnownConceptKeys     []string             `json:"known_concept_keys,omitempty"`
	WeakConceptKeys      []string             `json:"weak_concept_keys,omitempty"`
	DueReviewConceptKeys []string             `json:"due_review_concept_keys,omitempty"`
	UnseenConceptKeys    []string             `json:"unseen_concept_keys,omitempty"`
	Concepts             []ConceptKnowledgeV1 `json:"concepts,omitempty"`
	KnownRatio           float64              `json:"known_ratio,omitempty"`
	SeenOrAssessedRatio  float64              `json:"seen_or_assessed_ratio,omitempty"`

	TopFrames            []string `json:"top_frames,omitempty"`
	ActiveMisconceptions []string `json:"active_misconceptions,omitempty"`
	UncertaintyRegions   []string `json:"uncertainty_regions,omitempty"`
	LastStrongEvidenceAt string   `json:"last_strong_evidence_at,omitempty"`
	ProbeTargets         []string `json:"probe_targets,omitempty"`
}

type KnowledgeContextOptions struct {
	ActiveOnly      bool
	ForceActiveKeys map[string]bool
}

func (c UserKnowledgeContextV1) JSON() string {
	b, _ := json.Marshal(c)
	return string(b)
}

func (c UserKnowledgeContextV2) JSON() string {
	b, _ := json.Marshal(c)
	return string(b)
}

// BuildUserKnowledgeContextV1 creates a compact, prompt-friendly view of a user's mastery/exposure
// for a set of concept keys.
//
// canonicalIDByKey: concept_key(lowercase) -> canonical concept UUID (preferred) or path concept UUID.
// stateByConceptID: canonical concept UUID -> user concept state (may be sparse).
func BuildUserKnowledgeContextV1(
	conceptKeys []string,
	canonicalIDByKey map[string]uuid.UUID,
	stateByConceptID map[uuid.UUID]*types.UserConceptState,
	now time.Time,
) UserKnowledgeContextV1 {
	const (
		knownMasteryMin    = 0.85
		knownConfidenceMin = 0.60
		weakMasteryMax     = 0.50
		weakConfidenceMax  = 0.35
	)

	if now.IsZero() {
		now = time.Now().UTC()
	}

	seenKeys := map[string]bool{}
	keys := make([]string, 0, len(conceptKeys))
	for _, k := range conceptKeys {
		k = strings.TrimSpace(strings.ToLower(k))
		if k == "" || seenKeys[k] {
			continue
		}
		seenKeys[k] = true
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := UserKnowledgeContextV1{
		Version:  1,
		Concepts: make([]ConceptKnowledgeV1, 0, len(keys)),
	}

	known := make([]string, 0, 8)
	weak := make([]string, 0, 8)
	unseen := make([]string, 0, 8)
	due := make([]string, 0, 6)
	seenOrAssessed := 0

	for _, k := range keys {
		cid := uuid.Nil
		if canonicalIDByKey != nil {
			cid = canonicalIDByKey[k]
		}
		st := (*types.UserConceptState)(nil)
		if stateByConceptID != nil && cid != uuid.Nil {
			st = stateByConceptID[cid]
		}

		entry := ConceptKnowledgeV1{
			Key:    k,
			Status: "unseen",
		}
		if cid != uuid.Nil {
			entry.CanonicalConceptID = cid.String()
		}

		if st == nil || st.UserID == uuid.Nil || st.ConceptID == uuid.Nil {
			unseen = append(unseen, k)
			out.Concepts = append(out.Concepts, entry)
			continue
		}

		entry.Mastery = clamp01(st.Mastery)
		entry.Confidence = clamp01(st.Confidence)
		entry.Attempts = st.Attempts
		entry.Correct = st.Correct
		if st.LastSeenAt != nil && !st.LastSeenAt.IsZero() {
			entry.LastSeenAt = st.LastSeenAt.UTC().Format(time.RFC3339Nano)
		}
		if st.NextReviewAt != nil && !st.NextReviewAt.IsZero() {
			entry.NextReviewAt = st.NextReviewAt.UTC().Format(time.RFC3339Nano)
			if st.NextReviewAt.Before(now) || st.NextReviewAt.Equal(now) {
				due = append(due, k)
			}
		}

		entry.MisconceptionHints = misconceptionHintsFromState(st, 2)

		status := "learning"
		if entry.Mastery >= knownMasteryMin && entry.Confidence >= knownConfidenceMin {
			status = "known"
			known = append(known, k)
		} else if entry.Mastery <= weakMasteryMax || entry.Confidence <= weakConfidenceMax {
			status = "weak"
			weak = append(weak, k)
		}
		entry.Status = status

		if entry.Attempts > 0 || entry.LastSeenAt != "" {
			seenOrAssessed++
		}

		out.Concepts = append(out.Concepts, entry)
	}

	out.KnownConceptKeys = known
	out.WeakConceptKeys = weak
	out.UnseenConceptKeys = unseen
	out.DueReviewConceptKeys = due

	// Ratios are helpful for "fast-track vs full curriculum" decisions.
	if len(keys) > 0 {
		out.KnownRatio = float64(len(known)) / float64(len(keys))
		out.SeenOrAssessedRatio = float64(seenOrAssessed) / float64(len(keys))
	}

	return out
}

type frameSignal struct {
	Frame      string  `json:"frame"`
	Confidence float64 `json:"confidence"`
}

type uncertaintySignal struct {
	Kind       string  `json:"kind"`
	Confidence float64 `json:"confidence"`
}

// BuildUserKnowledgeContextV2 augments V1 with minimal structural signals.
func BuildUserKnowledgeContextV2(
	conceptKeys []string,
	canonicalIDByKey map[string]uuid.UUID,
	stateByConceptID map[uuid.UUID]*types.UserConceptState,
	modelByConceptID map[uuid.UUID]*types.UserConceptModel,
	misconByConceptID map[uuid.UUID][]*types.UserMisconceptionInstance,
	now time.Time,
	opts *KnowledgeContextOptions,
) UserKnowledgeContextV2 {
	v1 := BuildUserKnowledgeContextV1(conceptKeys, canonicalIDByKey, stateByConceptID, now)
	out := UserKnowledgeContextV2{
		Version:              2,
		KnownConceptKeys:     v1.KnownConceptKeys,
		WeakConceptKeys:      v1.WeakConceptKeys,
		DueReviewConceptKeys: v1.DueReviewConceptKeys,
		UnseenConceptKeys:    v1.UnseenConceptKeys,
		Concepts:             v1.Concepts,
		KnownRatio:           v1.KnownRatio,
		SeenOrAssessedRatio:  v1.SeenOrAssessedRatio,
	}

	type scored struct {
		Text  string
		Score float64
	}

	var (
		frameCandidates []scored
		uncCandidates   []scored
		misCandidates   []scored
		lastEvidence    time.Time
	)
	activeOnly := false
	forceActive := map[string]bool{}
	if opts != nil {
		activeOnly = opts.ActiveOnly
		if opts.ForceActiveKeys != nil {
			for forceKey := range opts.ForceActiveKeys {
				forceActive[strings.TrimSpace(strings.ToLower(forceKey))] = true
			}
		}
	}
	recencyDays := envIntAllowZero("ACTIVE_CONCEPT_RECENCY_DAYS", 90)
	if recencyDays < 7 {
		recencyDays = 7
	}
	confMin := envFloatAllowZero("ACTIVE_CONCEPT_CONFIDENCE_MIN", 0.25)
	if confMin < 0 {
		confMin = 0
	}

	for _, k := range conceptKeys {
		kNorm := strings.TrimSpace(strings.ToLower(k))
		if kNorm == "" {
			continue
		}
		cid := uuid.Nil
		if canonicalIDByKey != nil {
			cid = canonicalIDByKey[kNorm]
		}
		if cid == uuid.Nil {
			continue
		}
		if modelByConceptID != nil {
			if m := modelByConceptID[cid]; m != nil {
				if m.LastStructuralAt != nil && !m.LastStructuralAt.IsZero() {
					if m.LastStructuralAt.After(lastEvidence) {
						lastEvidence = *m.LastStructuralAt
					}
				}
				if len(m.ActiveFrames) > 0 {
					var frames []frameSignal
					_ = json.Unmarshal(m.ActiveFrames, &frames)
					for _, f := range frames {
						if strings.TrimSpace(f.Frame) == "" {
							continue
						}
						frameCandidates = append(frameCandidates, scored{
							Text:  kNorm + ": " + strings.TrimSpace(f.Frame),
							Score: f.Confidence,
						})
					}
				}
				if len(m.Uncertainty) > 0 {
					var unc []uncertaintySignal
					_ = json.Unmarshal(m.Uncertainty, &unc)
					for _, u := range unc {
						if strings.TrimSpace(u.Kind) == "" {
							continue
						}
						uncCandidates = append(uncCandidates, scored{
							Text:  kNorm + ": " + strings.TrimSpace(u.Kind),
							Score: u.Confidence,
						})
					}
				}
			}
		}
		if misconByConceptID != nil {
			if rows := misconByConceptID[cid]; len(rows) > 0 {
				for _, r := range rows {
					if r == nil || strings.TrimSpace(r.Description) == "" {
						continue
					}
					misCandidates = append(misCandidates, scored{
						Text:  kNorm + ": " + strings.TrimSpace(r.Description),
						Score: clamp01(r.Confidence),
					})
				}
			}
		}
	}

	filtered := make([]ConceptKnowledgeV1, 0, len(out.Concepts))
	for _, entry := range out.Concepts {
		cid := uuid.Nil
		if entry.CanonicalConceptID != "" {
			_ = cid.Scan(entry.CanonicalConceptID)
		}
		var model *types.UserConceptModel
		if cid != uuid.Nil && modelByConceptID != nil {
			model = modelByConceptID[cid]
		}
		active := false
		if forceActive[strings.TrimSpace(strings.ToLower(entry.Key))] {
			active = true
		}
		if !active {
			active = isConceptActive(entry, model, recencyDays, confMin, now)
		}
		entry.Active = active
		if activeOnly && !active {
			continue
		}
		filtered = append(filtered, entry)
	}
	out.Concepts = filtered
	if activeOnly {
		known := []string{}
		weak := []string{}
		unseen := []string{}
		due := []string{}
		seenOrAssessed := 0
		for _, entry := range out.Concepts {
			switch entry.Status {
			case "known":
				known = append(known, entry.Key)
			case "weak":
				weak = append(weak, entry.Key)
			case "unseen":
				unseen = append(unseen, entry.Key)
			default:
				// learning
			}
			if entry.NextReviewAt != "" {
				if t, err := time.Parse(time.RFC3339Nano, entry.NextReviewAt); err == nil {
					if !t.After(now) {
						due = append(due, entry.Key)
					}
				}
			}
			if entry.Attempts > 0 || entry.LastSeenAt != "" {
				seenOrAssessed++
			}
		}
		out.KnownConceptKeys = known
		out.WeakConceptKeys = weak
		out.UnseenConceptKeys = unseen
		out.DueReviewConceptKeys = due
		if len(out.Concepts) > 0 {
			out.KnownRatio = float64(len(known)) / float64(len(out.Concepts))
			out.SeenOrAssessedRatio = float64(seenOrAssessed) / float64(len(out.Concepts))
		}
	}

	sort.Slice(frameCandidates, func(i, j int) bool { return frameCandidates[i].Score > frameCandidates[j].Score })
	sort.Slice(uncCandidates, func(i, j int) bool { return uncCandidates[i].Score > uncCandidates[j].Score })
	sort.Slice(misCandidates, func(i, j int) bool { return misCandidates[i].Score > misCandidates[j].Score })

	maxPick := func(list []scored, n int) []string {
		out := []string{}
		for i := 0; i < len(list) && i < n; i++ {
			if strings.TrimSpace(list[i].Text) != "" {
				out = append(out, list[i].Text)
			}
		}
		return out
	}

	out.TopFrames = maxPick(frameCandidates, 3)
	out.ActiveMisconceptions = maxPick(misCandidates, 3)
	out.UncertaintyRegions = maxPick(uncCandidates, 3)
	if !lastEvidence.IsZero() {
		out.LastStrongEvidenceAt = lastEvidence.UTC().Format(time.RFC3339Nano)
	}
	if len(out.ActiveMisconceptions) > 0 {
		out.ProbeTargets = out.ActiveMisconceptions
	} else if len(out.UncertaintyRegions) > 0 {
		out.ProbeTargets = out.UncertaintyRegions
	}

	return out
}

func isConceptActive(entry ConceptKnowledgeV1, model *types.UserConceptModel, recencyDays int, confMin float64, now time.Time) bool {
	if strings.TrimSpace(entry.Key) == "" {
		return false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	seenAt := time.Time{}
	if entry.LastSeenAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, entry.LastSeenAt); err == nil {
			seenAt = t
		}
	}
	if model != nil && model.LastStructuralAt != nil && !model.LastStructuralAt.IsZero() {
		if seenAt.IsZero() || model.LastStructuralAt.After(seenAt) {
			seenAt = *model.LastStructuralAt
		}
	}
	if !seenAt.IsZero() {
		age := now.Sub(seenAt)
		if age >= 0 && age <= time.Duration(recencyDays)*24*time.Hour {
			return true
		}
	}
	if entry.Confidence >= confMin {
		return true
	}
	if entry.Attempts > 0 || entry.Correct > 0 {
		return true
	}
	if model != nil && len(model.Support) > 0 && string(model.Support) != "null" {
		return true
	}
	return false
}

func misconceptionHintsFromState(st *types.UserConceptState, max int) []string {
	if st == nil || max <= 0 || len(st.Misconceptions) == 0 || strings.TrimSpace(string(st.Misconceptions)) == "" || strings.TrimSpace(string(st.Misconceptions)) == "null" {
		return nil
	}

	var arr []map[string]any
	if err := json.Unmarshal(st.Misconceptions, &arr); err != nil || len(arr) == 0 {
		return nil
	}
	if len(arr) > max {
		arr = arr[len(arr)-max:]
	}

	out := make([]string, 0, len(arr))
	for _, m := range arr {
		if m == nil {
			continue
		}
		kind := strings.TrimSpace(strings.ToLower(strings.TrimSpace(stringFromAny(m["kind"]))))
		qid := strings.TrimSpace(stringFromAny(m["question_id"]))
		if kind == "" && qid == "" {
			continue
		}
		if qid != "" {
			out = append(out, kind+" question_id="+qid)
		} else {
			out = append(out, kind)
		}
	}
	return out
}

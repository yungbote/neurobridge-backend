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

func (c UserKnowledgeContextV1) JSON() string {
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

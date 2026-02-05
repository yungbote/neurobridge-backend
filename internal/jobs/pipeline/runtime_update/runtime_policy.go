package runtime_update

import (
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type readinessSnapshot struct {
	Status                string   `json:"status"`
	Score                 float64  `json:"score"`
	AvgMastery            float64  `json:"avg_mastery"`
	MinMastery            float64  `json:"min_mastery"`
	MaxUncertainty        float64  `json:"max_uncertainty"`
	ConceptsTotal         int      `json:"concepts_total"`
	ConceptsMissing       int      `json:"concepts_missing"`
	MisconceptionsActive  int      `json:"misconceptions_active"`
	WeakConcepts          []string `json:"weak_concepts"`
	UncertainConcepts     []string `json:"uncertain_concepts"`
	MisconceptionConcepts []string `json:"misconception_concepts"`
	ComputedAt            string   `json:"computed_at"`
}

type readinessResult struct {
	Snapshot        *readinessSnapshot
	ConceptByKey    map[string]*types.Concept
	ConceptKeyByID  map[uuid.UUID]string
	ConceptState    map[uuid.UUID]*types.UserConceptState
	MisconceptionBy map[uuid.UUID]float64
}

type promptCandidate struct {
	BlockID         string
	Kind            string
	Index           int
	ConceptIDs      []uuid.UUID
	ConceptKeys     []string
	TestletID       string
	TestletType     string
	TestletUncertainty float64
	InfoGain        float64
	Counterfactual  bool
	Score           float64
	PolicyScore     float64
	BaselineProb    float64
	PolicyProb      float64
	BehaviorProb    float64
	ShadowProb      float64
	PolicyFeatures  map[string]float64
	ScoreComponents map[string]float64
	Reason          string
}

func readinessEnabled() bool {
	return envBool("RUNTIME_READINESS_ENABLED", true)
}

func readinessCacheSeconds() int {
	return envInt("RUNTIME_READINESS_CACHE_SECONDS", 30, 0, 600)
}

func readinessReadyMin() float64 {
	return envFloat("RUNTIME_READINESS_READY_MIN", 0.75, 0.3, 0.98)
}

func readinessUncertainMin() float64 {
	return envFloat("RUNTIME_READINESS_UNCERTAIN_MIN", 0.55, 0.1, 0.95)
}

func readinessMinMastery() float64 {
	return envFloat("RUNTIME_READINESS_MIN_MASTERY", 0.6, 0.1, 0.95)
}

func readinessMaxMisconceptionsReady() int {
	return envInt("RUNTIME_READINESS_MAX_MISCONCEPTIONS_READY", 0, 0, 99)
}

func readinessPromptBoost() float64 {
	return envFloat("RUNTIME_READINESS_PROMPT_BOOST", 0.2, 0, 2)
}

func readinessUseBlockConcepts() bool {
	return envBool("RUNTIME_READINESS_USE_BLOCK_CONCEPTS", true)
}

func banditEnabled() bool {
	return envBool("RUNTIME_BANDIT_ENABLED", true)
}

func runtimeRLMode() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("RUNTIME_RL_MODE")))
	if mode == "" {
		return "shadow"
	}
	switch mode {
	case "off", "shadow", "active":
		return mode
	default:
		return "shadow"
	}
}

func runtimeRLPolicyKey() string {
	key := strings.TrimSpace(os.Getenv("RUNTIME_RL_POLICY_KEY"))
	if key == "" {
		return "runtime_prompt_policy_v1"
	}
	return key
}

func runtimeRLSoftmaxTemp() float64 {
	return envFloat("RUNTIME_RL_SOFTMAX_TEMP", 1.0, 0.05, 10)
}

func runtimeRLRolloutPct() float64 {
	return envFloat("RUNTIME_RL_ROLLOUT_PCT", 1.0, 0, 1)
}

func runtimeRLSafeMinSamples() int {
	return envInt("RUNTIME_RL_SAFE_MIN_SAMPLES", 500, 0, 1000000)
}

func runtimeRLSafeMinIPS() float64 {
	return envFloat("RUNTIME_RL_SAFE_MIN_IPS", 0.0, -1, 1)
}

func runtimeRLSafeMinLift() float64 {
	return envFloat("RUNTIME_RL_SAFE_MIN_LIFT", -0.02, -1, 1)
}

func testletEnabled() bool {
	return envBool("RUNTIME_TESTLET_ENABLED", true)
}

func testletUncertaintyWeight() float64 {
	return envFloat("RUNTIME_TESTLET_UNCERTAINTY_WEIGHT", 0.25, 0, 5)
}

func banditExplorationWeight() float64 {
	return envFloat("RUNTIME_BANDIT_EXPLORATION_WEIGHT", 0.35, 0, 5)
}

func banditRecencyPenaltyMinutes() int {
	return envInt("RUNTIME_BANDIT_RECENCY_PENALTY_MINUTES", 10, 0, 240)
}

func banditMinInfoGain() float64 {
	return envFloat("RUNTIME_BANDIT_MIN_INFO_GAIN", 0.05, 0, 1)
}

func counterfactualEnabled() bool {
	return envBool("RUNTIME_COUNTERFACTUAL_ENABLED", true)
}

func counterfactualFailStreak() int {
	return envInt("RUNTIME_COUNTERFACTUAL_FAIL_STREAK", 2, 0, 20)
}

func counterfactualBoost() float64 {
	return envFloat("RUNTIME_COUNTERFACTUAL_BOOST", 0.3, 0, 5)
}

func fatigueEnabled() bool {
	return envBool("RUNTIME_FATIGUE_ENABLED", true)
}

func fatigueTimeWeight() float64 {
	return envFloat("RUNTIME_FATIGUE_TIME_WEIGHT", 0.45, 0, 1)
}

func fatiguePromptWeight() float64 {
	return envFloat("RUNTIME_FATIGUE_PROMPT_WEIGHT", 0.35, 0, 1)
}

func fatigueFailWeight() float64 {
	return envFloat("RUNTIME_FATIGUE_FAIL_WEIGHT", 0.2, 0, 1)
}

func fatigueBreakThreshold() float64 {
	return envFloat("RUNTIME_FATIGUE_BREAK_THRESHOLD", 0.7, 0.2, 1.0)
}

func fatigueSuppressThreshold() float64 {
	return envFloat("RUNTIME_FATIGUE_SUPPRESS_THRESHOLD", 0.85, 0.2, 1.0)
}

func fatiguePromptRatePerHour() float64 {
	return envFloat("RUNTIME_FATIGUE_PROMPT_RATE_PER_HOUR", 6, 1, 30)
}

func fatigueMaxSessionMinutes() float64 {
	return envFloat("RUNTIME_FATIGUE_MAX_SESSION_MINUTES", 45, 5, 240)
}

func fatigueMinBreakGapMinutes() float64 {
	return envFloat("RUNTIME_FATIGUE_MIN_BREAK_GAP_MINUTES", 10, 1, 120)
}

func envBool(name string, def bool) bool {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "y":
		return true
	case "0", "false", "no", "n":
		return false
	default:
		return def
	}
}

func envFloat(name string, def float64, min float64, max float64) float64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	if v, err := strconv.ParseFloat(raw, 64); err == nil {
		if v < min {
			return min
		}
		if v > max {
			return max
		}
		return v
	}
	return def
}

func envInt(name string, def int, min int, max int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	if v, err := strconv.Atoi(raw); err == nil {
		if v < min {
			return min
		}
		if v > max {
			return max
		}
		return v
	}
	return def
}

func readinessToMap(r *readinessSnapshot) map[string]any {
	if r == nil {
		return nil
	}
	return map[string]any{
		"status":                 r.Status,
		"score":                  r.Score,
		"avg_mastery":            r.AvgMastery,
		"min_mastery":            r.MinMastery,
		"max_uncertainty":        r.MaxUncertainty,
		"concepts_total":         r.ConceptsTotal,
		"concepts_missing":       r.ConceptsMissing,
		"misconceptions_active":  r.MisconceptionsActive,
		"weak_concepts":          r.WeakConcepts,
		"uncertain_concepts":     r.UncertainConcepts,
		"misconception_concepts": r.MisconceptionConcepts,
		"computed_at":            r.ComputedAt,
	}
}

func collectConceptKeysFromDoc(doc content.NodeDocV1) []string {
	keys := []string{}
	for _, k := range doc.ConceptKeys {
		if s := strings.TrimSpace(k); s != "" {
			keys = append(keys, s)
		}
	}
	if readinessUseBlockConcepts() {
		for _, block := range doc.Blocks {
			for _, k := range stringSliceFromAny(block["concept_keys"]) {
				if s := strings.TrimSpace(k); s != "" {
					keys = append(keys, s)
				}
			}
		}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func extractConceptIDs(block map[string]any, conceptByKey map[string]*types.Concept) []uuid.UUID {
	out := []uuid.UUID{}
	for _, k := range stringSliceFromAny(block["concept_keys"]) {
		if c := conceptByKey[strings.TrimSpace(k)]; c != nil && c.ID != uuid.Nil {
			out = append(out, c.ID)
		}
	}
	for _, raw := range stringSliceFromAny(block["concept_ids"]) {
		if id, err := uuid.Parse(strings.TrimSpace(raw)); err == nil {
			out = append(out, id)
		}
	}
	if idStr := strings.TrimSpace(stringFromAny(block["concept_id"])); idStr != "" {
		if id, err := uuid.Parse(idStr); err == nil {
			out = append(out, id)
		}
	}
	seen := map[uuid.UUID]bool{}
	uniq := make([]uuid.UUID, 0, len(out))
	for _, id := range out {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		uniq = append(uniq, id)
	}
	return uniq
}

func computeReadiness(
	dbc dbctx.Context,
	userID uuid.UUID,
	pathID uuid.UUID,
	doc content.NodeDocV1,
	conceptsRepo interface {
		GetByScopeAndKeys(dbc dbctx.Context, scope string, scopeID *uuid.UUID, keys []string) ([]*types.Concept, error)
	},
	stateRepo interface {
		ListByUserAndConceptIDs(dbc dbctx.Context, userID uuid.UUID, conceptIDs []uuid.UUID) ([]*types.UserConceptState, error)
	},
	misconRepo interface {
		ListActiveByUserAndConceptIDs(dbc dbctx.Context, userID uuid.UUID, conceptIDs []uuid.UUID) ([]*types.UserMisconceptionInstance, error)
	},
) readinessResult {
	result := readinessResult{
		ConceptByKey:    map[string]*types.Concept{},
		ConceptKeyByID:  map[uuid.UUID]string{},
		ConceptState:    map[uuid.UUID]*types.UserConceptState{},
		MisconceptionBy: map[uuid.UUID]float64{},
	}
	if userID == uuid.Nil || pathID == uuid.Nil || conceptsRepo == nil || stateRepo == nil {
		return result
	}

	keys := collectConceptKeysFromDoc(doc)
	if len(keys) == 0 {
		return result
	}

	concepts, _ := conceptsRepo.GetByScopeAndKeys(dbc, "path", &pathID, keys)
	conceptIDs := []uuid.UUID{}
	for _, c := range concepts {
		if c == nil || c.ID == uuid.Nil {
			continue
		}
		key := strings.TrimSpace(c.Key)
		if key != "" {
			result.ConceptByKey[key] = c
			result.ConceptKeyByID[c.ID] = key
		}
		conceptIDs = append(conceptIDs, c.ID)
	}
	if len(conceptIDs) == 0 {
		return result
	}

	states, _ := stateRepo.ListByUserAndConceptIDs(dbc, userID, conceptIDs)
	for _, st := range states {
		if st != nil && st.ConceptID != uuid.Nil {
			result.ConceptState[st.ConceptID] = st
		}
	}

	if misconRepo != nil {
		miscons, _ := misconRepo.ListActiveByUserAndConceptIDs(dbc, userID, conceptIDs)
		for _, m := range miscons {
			if m == nil || m.CanonicalConceptID == uuid.Nil {
				continue
			}
			conf := m.Confidence
			if conf <= 0 {
				conf = 0.5
			}
			if prev, ok := result.MisconceptionBy[m.CanonicalConceptID]; !ok || conf > prev {
				result.MisconceptionBy[m.CanonicalConceptID] = conf
			}
		}
	}

	score := 0.0
	total := 0.0
	minMastery := 1.0
	maxUnc := 0.0
	missing := 0
	weak := []string{}
	uncertain := []string{}
	misconcepts := []string{}

	for _, id := range conceptIDs {
		key := result.ConceptKeyByID[id]
		st := result.ConceptState[id]
		if st == nil {
			missing++
			minMastery = math.Min(minMastery, 0)
			if key != "" {
				weak = append(weak, key)
			}
			continue
		}
		mastery := clamp01(st.Mastery)
		conf := clamp01(st.Confidence)
		unc := math.Max(clamp01(st.EpistemicUncertainty), clamp01(st.AleatoricUncertainty))
		effective := mastery * (0.4 + 0.6*conf)
		score += effective
		total += 1
		minMastery = math.Min(minMastery, mastery)
		if unc > maxUnc {
			maxUnc = unc
		}
		if mastery < readinessMinMastery() && key != "" {
			weak = append(weak, key)
		}
		if unc > 0.6 && key != "" {
			uncertain = append(uncertain, key)
		}
		if _, ok := result.MisconceptionBy[id]; ok && key != "" {
			misconcepts = append(misconcepts, key)
		}
	}

	if total > 0 {
		score = score / total
	}

	misconActive := len(result.MisconceptionBy)
	status := "uncertain"
	if score >= readinessReadyMin() && minMastery >= readinessMinMastery() && misconActive <= readinessMaxMisconceptionsReady() {
		status = "ready"
	} else if score < readinessUncertainMin() || misconActive > readinessMaxMisconceptionsReady() {
		status = "not_ready"
	}

	result.Snapshot = &readinessSnapshot{
		Status:                status,
		Score:                 clamp01(score),
		AvgMastery:            clamp01(score),
		MinMastery:            clamp01(minMastery),
		MaxUncertainty:        clamp01(maxUnc),
		ConceptsTotal:         len(conceptIDs),
		ConceptsMissing:       missing,
		MisconceptionsActive:  misconActive,
		WeakConcepts:          uniqueStrings(weak),
		UncertainConcepts:     uniqueStrings(uncertain),
		MisconceptionConcepts: uniqueStrings(misconcepts),
		ComputedAt:            time.Now().UTC().Format(time.RFC3339),
	}
	return result
}

func uniqueStrings(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func computeInfoGain(conceptIDs []uuid.UUID, states map[uuid.UUID]*types.UserConceptState) float64 {
	if len(conceptIDs) == 0 {
		return 0.1
	}
	gain := 0.0
	for _, id := range conceptIDs {
		st := states[id]
		if st == nil {
			gain += 0.5
			continue
		}
		mastery := clamp01(st.Mastery)
		unc := math.Max(clamp01(st.EpistemicUncertainty), clamp01(st.AleatoricUncertainty))
		conf := clamp01(st.Confidence)
		gain += (1.0-mastery)*(0.5+0.5*math.Max(unc, 1.0-conf))
	}
	return gain / float64(len(conceptIDs))
}

func computeFatigue(prRuntime map[string]any, promptsInWindow int, failStreak int, now time.Time) float64 {
	sessionStarted := timeFromAny(prRuntime["session_started_at"])
	if sessionStarted == nil {
		return 0
	}
	elapsedMin := now.Sub(*sessionStarted).Minutes()
	if elapsedMin < 0 {
		elapsedMin = 0
	}
	timeComponent := clamp01(elapsedMin / math.Max(fatigueMaxSessionMinutes(), 1))
	promptRate := 0.0
	if elapsedMin > 0 {
		promptRate = (float64(promptsInWindow) / elapsedMin) * 60.0
	}
	promptComponent := clamp01(promptRate / math.Max(fatiguePromptRatePerHour(), 1))
	failComponent := clamp01(float64(failStreak) / 4.0)

	score := 0.0
	score += fatigueTimeWeight() * timeComponent
	score += fatiguePromptWeight() * promptComponent
	score += fatigueFailWeight() * failComponent
	return clamp01(score)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func banditStore(runtime map[string]any) (map[string]any, map[string]any) {
	store := mapFromAny(runtime["bandit"])
	blocks := mapFromAny(store["blocks"])
	if blocks == nil {
		blocks = map[string]any{}
	}
	store["blocks"] = blocks
	runtime["bandit"] = store
	return store, blocks
}

func banditBlock(blocks map[string]any, id string) map[string]any {
	if id == "" {
		return map[string]any{}
	}
	if raw, ok := blocks[id]; ok {
		if m := mapFromAny(raw); m != nil {
			blocks[id] = m
			return m
		}
	}
	m := map[string]any{}
	blocks[id] = m
	return m
}

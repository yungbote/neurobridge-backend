package user_model_update

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/envutil"
)

const (
	defaultDecayRate      = 0.015
	maxExposureConfidence = 0.35
	defaultBktLearn       = 0.18
	defaultBktGuess       = 0.20
	defaultBktSlip        = 0.08
	defaultBktForget      = 0.02
	minHalfLifeDays       = 0.20
	maxHalfLifeDays       = 240.0
)

var (
	ktBktLearnDefault       = clampRange(envutil.Float("USER_KT_BKT_P_LEARN", defaultBktLearn), 0.01, 0.9)
	ktBktGuessDefault       = clampRange(envutil.Float("USER_KT_BKT_P_GUESS", defaultBktGuess), 0.01, 0.5)
	ktBktSlipDefault        = clampRange(envutil.Float("USER_KT_BKT_P_SLIP", defaultBktSlip), 0.01, 0.5)
	ktBktForgetDefault      = clampRange(envutil.Float("USER_KT_BKT_P_FORGET", defaultBktForget), 0.001, 0.3)
	ktMinHalfLifeDays       = clampRange(envutil.Float("USER_KT_MIN_HALF_LIFE_DAYS", minHalfLifeDays), 0.05, 365)
	ktMaxHalfLifeDays       = clampRange(envutil.Float("USER_KT_MAX_HALF_LIFE_DAYS", maxHalfLifeDays), 1.0, 3650)
	ktReviewTargetCorrect   = clampRange(envutil.Float("USER_KT_REVIEW_TARGET_CORRECT", 0.87), 0.6, 0.99)
	ktReviewTargetIncorrect = clampRange(envutil.Float("USER_KT_REVIEW_TARGET_INCORRECT", 0.65), 0.3, 0.95)
	ktHierarchyEnabled      = envutil.Bool("USER_KT_HIERARCHY_ENABLED", true)
	ktClusterPriorWeight    = clampRange(envutil.Float("USER_KT_CLUSTER_PRIOR_WEIGHT", 0.2), 0, 1)
	ktParentPriorWeight     = clampRange(envutil.Float("USER_KT_PARENT_PRIOR_WEIGHT", 0.15), 0, 1)
	ktPriorApplyConfBelow   = clampRange(envutil.Float("USER_KT_PRIOR_APPLY_CONF_BELOW", 0.3), 0, 1)
	ktPriorApplyWeight      = clampRange(envutil.Float("USER_KT_PRIOR_APPLY_WEIGHT", 0.6), 0, 1)

	propagationPrereqWeight  = clampRange(envutil.Float("USER_PROPAGATION_PREREQ_WEIGHT", 0.06), 0, 1)
	propagationRelatedWeight = clampRange(envutil.Float("USER_PROPAGATION_RELATED_WEIGHT", 0.04), 0, 1)
	propagationAnalogyWeight = clampRange(envutil.Float("USER_PROPAGATION_ANALOGY_WEIGHT", 0.05), 0, 1)
	propagationMaxDelta      = clampRange(envutil.Float("USER_PROPAGATION_MAX_DELTA", 0.08), 0, 0.5)
	propagationMinMastery    = clampRange(envutil.Float("USER_PROPAGATION_MIN_MASTERY", 0.65), 0, 1)
	propagationMinConfidence = clampRange(envutil.Float("USER_PROPAGATION_MIN_CONF", 0.45), 0, 1)
	propagationMaxEpi        = clampRange(envutil.Float("USER_PROPAGATION_MAX_EPI", 0.75), 0, 1)
	propagationMaxAlea       = clampRange(envutil.Float("USER_PROPAGATION_MAX_ALEA", 0.75), 0, 1)

	bridgeMinScore                = clampRange(envutil.Float("USER_BRIDGE_MIN_SCORE", 0.86), 0, 1)
	bridgePropagationWeight       = clampRange(envutil.Float("USER_BRIDGE_PROPAGATION_WEIGHT", 0.04), 0, 1)
	bridgeMinMastery              = clampRange(envutil.Float("USER_BRIDGE_MIN_MASTERY", 0.7), 0, 1)
	bridgeMinConfidence           = clampRange(envutil.Float("USER_BRIDGE_MIN_CONF", 0.5), 0, 1)
	bridgeMaxEpi                  = clampRange(envutil.Float("USER_BRIDGE_MAX_EPI", 0.6), 0, 1)
	bridgeMaxAlea                 = clampRange(envutil.Float("USER_BRIDGE_MAX_ALEA", 0.6), 0, 1)
	bridgeFalseWindowHours        = clampRange(envutil.Float("USER_BRIDGE_FALSE_WINDOW_HOURS", 48), 1, 720)
	bridgeFalseRateTighten        = clampRange(envutil.Float("USER_BRIDGE_FALSE_RATE_TIGHTEN", 0.25), 0, 1)
	bridgeHardBlockRate           = clampRange(envutil.Float("USER_BRIDGE_HARD_BLOCK_RATE", 0.5), 0, 1)
	bridgeBlockHours              = clampRange(envutil.Float("USER_BRIDGE_BLOCK_HOURS", 72), 1, 720)
	bridgeValidationCooldownHours = clampRange(envutil.Float("USER_BRIDGE_VALIDATION_COOLDOWN_HOURS", 72), 1, 720)
	bridgeThresholdBoostStep      = clampRange(envutil.Float("USER_BRIDGE_THRESHOLD_BOOST_STEP", 0.04), 0, 0.5)

	calibMinSamples = func() int {
		v := envutil.Int("USER_CALIB_MIN_SAMPLES", 8)
		if v < 1 {
			return 1
		}
		return v
	}()
	calibGapWarn    = clampRange(envutil.Float("USER_CALIB_GAP_WARN", 0.2), 0, 1)
	calibGapCrit    = clampRange(envutil.Float("USER_CALIB_GAP_CRIT", 0.3), 0, 1)
	calibAbsErrWarn = clampRange(envutil.Float("USER_CALIB_ABS_ERR_WARN", 0.25), 0, 1)
	calibAbsErrCrit = clampRange(envutil.Float("USER_CALIB_ABS_ERR_CRIT", 0.35), 0, 1)
	calibBrierWarn  = clampRange(envutil.Float("USER_CALIB_BRIER_WARN", 0.25), 0, 1)
	calibBrierCrit  = clampRange(envutil.Float("USER_CALIB_BRIER_CRIT", 0.35), 0, 1)

	testletBetaPrior = clampRange(envutil.Float("USER_TESTLET_BETA_PRIOR", 1.0), 0.1, 10)
	testletEmaAlpha  = clampRange(envutil.Float("USER_TESTLET_EMA_ALPHA", 0.18), 0.01, 0.8)

	itemCalibLR       = clampRange(envutil.Float("USER_ITEM_CALIB_LR", 0.05), 0.001, 0.4)
	itemCalibDiscMin  = clampRange(envutil.Float("USER_ITEM_CALIB_DISC_MIN", 0.2), 0.1, 1.0)
	itemCalibDiscMax  = clampRange(envutil.Float("USER_ITEM_CALIB_DISC_MAX", 3.0), 1.0, 6.0)
	itemCalibDiffMin  = clampRange(envutil.Float("USER_ITEM_CALIB_DIFF_MIN", -2.5), -6.0, -0.5)
	itemCalibDiffMax  = clampRange(envutil.Float("USER_ITEM_CALIB_DIFF_MAX", 2.5), 0.5, 6.0)
	itemCalibGuessMax = clampRange(envutil.Float("USER_ITEM_CALIB_GUESS_MAX", 0.5), 0.1, 0.7)

	skillLR         = clampRange(envutil.Float("USER_SKILL_LR", 0.08), 0.005, 0.4)
	skillSigmaDecay = clampRange(envutil.Float("USER_SKILL_SIGMA_DECAY", 0.06), 0.01, 0.4)
	skillSigmaMin   = clampRange(envutil.Float("USER_SKILL_SIGMA_MIN", 0.15), 0.05, 1.0)
	skillSigmaMax   = clampRange(envutil.Float("USER_SKILL_SIGMA_MAX", 3.0), 0.5, 6.0)
)

func ensureConceptState(prev *types.UserConceptState, userID uuid.UUID, conceptID uuid.UUID) *types.UserConceptState {
	if prev != nil {
		return prev
	}
	return &types.UserConceptState{
		ID:                   uuid.New(),
		UserID:               userID,
		ConceptID:            conceptID,
		Mastery:              0,
		Confidence:           0,
		DecayRate:            defaultDecayRate,
		BktPLearn:            ktBktLearnDefault,
		BktPGuess:            ktBktGuessDefault,
		BktPSlip:             ktBktSlipDefault,
		BktPForget:           ktBktForgetDefault,
		EpistemicUncertainty: 1,
		AleatoricUncertainty: 0.5,
		HalfLifeDays:         1,
		Attempts:             0,
		Correct:              0,
	}
}

func setLastSeen(st *types.UserConceptState, seenAt time.Time) {
	if st == nil || seenAt.IsZero() {
		return
	}
	if st.LastSeenAt == nil || st.LastSeenAt.IsZero() || seenAt.After(*st.LastSeenAt) {
		t := seenAt.UTC()
		st.LastSeenAt = &t
	}
}

func applyQuestionAnsweredToState(st *types.UserConceptState, seenAt time.Time, data map[string]any) {
	if st == nil {
		return
	}

	isCorrect := boolFromAny(data["is_correct"], false)
	latencyMS := intFromAny(data["latency_ms"], 0)
	selfConf := clamp01(floatFromAny(data["confidence"], 0))
	graderConf := clamp01(floatFromAny(data["grader_confidence"], 0))
	if graderConf == 0 {
		graderConf = clamp01(floatFromAny(data["grader_confidence_pct"], 0) / 100.0)
	}

	// Attempts/correct counters (queryable evidence).
	st.Attempts += 1
	if isCorrect {
		st.Correct += 1
	}

	prevM := clamp01(st.Mastery)
	prevC := clamp01(st.Confidence)

	itemType := strings.ToLower(strings.TrimSpace(fmt.Sprint(data["item_type"])))
	itemOptions := intFromAny(data["item_options"], 0)
	itemGuess := clamp01(floatFromAny(data["item_guess"], 0))
	if itemGuess <= 0 {
		if itemOptions > 1 {
			itemGuess = 1.0 / float64(itemOptions)
		} else if itemType == "true_false" {
			itemGuess = 0.5
		}
	}
	if itemGuess <= 0 {
		itemGuess = defaultBktGuess
	}

	itemDisc := floatFromAny(data["item_discrimination"], 1.0)
	if itemDisc <= 0 {
		itemDisc = 1.0
	}
	if itemDisc > 3 {
		itemDisc = 3
	}

	itemDiff := floatFromAny(data["item_difficulty"], math.NaN())
	if math.IsNaN(itemDiff) {
		itemDiff = floatFromAny(data["node_difficulty"], math.NaN())
	}
	if math.IsNaN(itemDiff) {
		itemDiff = 0
	}
	itemDiff = normalizeItemDifficulty(itemDiff)

	pLearn, pGuess, pSlip, pForget := ensureBktParams(st)
	if itemGuess > 0 {
		pGuess = clamp01(0.6*pGuess + 0.4*itemGuess)
	}

	pKnown := prevM
	if st.LastSeenAt != nil && !st.LastSeenAt.IsZero() && !seenAt.IsZero() && seenAt.After(*st.LastSeenAt) {
		deltaDays := seenAt.Sub(*st.LastSeenAt).Hours() / 24.0
		if deltaDays > 0 && pForget > 0 {
			pKnown = pKnown * math.Exp(-pForget*deltaDays)
		}
	}
	pKnown = bktPosterior(pKnown, isCorrect, pGuess, pSlip)
	learnFactor := 1.0
	if !isCorrect {
		learnFactor = 0.55
	}
	evidenceStrength := clamp01(floatFromAny(data["evidence_strength"], 0))
	if evidenceStrength > 0 {
		learnFactor = learnFactor * (0.7 + 0.6*evidenceStrength)
	}
	pKnown = pKnown + (1.0-pKnown)*pLearn*learnFactor
	pKnown = clamp01(pKnown)

	theta := logit(pKnown)
	pCorrect := irtProb(theta, itemDisc, itemDiff, itemGuess)
	err := 0.0
	if isCorrect {
		err = 1.0
	}
	err = err - pCorrect
	lr := 0.12 * (0.6 + 0.4*selfConf)
	if evidenceStrength > 0 {
		lr *= 0.6 + 0.6*evidenceStrength
	}
	if latencyMS > 0 && latencyMS > 12000 {
		lr *= 0.85
	}
	if st.EpistemicUncertainty > 0 {
		lr *= clamp01(1.0 - (st.EpistemicUncertainty * 0.6))
	}
	theta = theta + lr*err
	pIrt := sigmoid(theta)

	mix := clamp01(0.35 + 0.15*itemDisc)
	m := clamp01((1.0-mix)*pKnown + mix*pIrt)
	if prevM > 0 {
		m = clamp01(0.2*prevM + 0.8*m)
	}

	st.Mastery = clamp01(m)

	epi, alea := updateUncertainty(st, selfConf, graderConf, seenAt)
	st.EpistemicUncertainty = epi
	st.AleatoricUncertainty = alea

	combinedConf := clamp01(1.0 - 0.5*(epi+alea))
	if selfConf > 0 {
		combinedConf = clamp01(0.7*combinedConf + 0.3*selfConf)
	} else if graderConf > 0 {
		combinedConf = clamp01(0.7*combinedConf + 0.3*graderConf)
	}
	if prevC > 0 {
		combinedConf = clamp01(0.6*prevC + 0.4*combinedConf)
	}
	st.Confidence = combinedConf

	st.BktPLearn = pLearn
	st.BktPGuess = pGuess
	st.BktPSlip = pSlip
	st.BktPForget = pForget

	setLastSeen(st, seenAt)
	scheduleNextReview(st, seenAt, isCorrect)
	now := seenAt
	if now.IsZero() {
		now = time.Now().UTC()
	}

	// Track a small rolling misconception log on incorrect answers (best-effort).
	if !isCorrect {
		var arr []map[string]any
		if len(st.Misconceptions) > 0 && string(st.Misconceptions) != "null" {
			_ = json.Unmarshal(st.Misconceptions, &arr)
		}
		arr = append(arr, map[string]any{
			"kind":        "incorrect_answer",
			"question_id": strings.TrimSpace(fmt.Sprint(data["question_id"])),
			"selected_id": strings.TrimSpace(fmt.Sprint(data["selected_id"])),
			"answer_id":   strings.TrimSpace(fmt.Sprint(data["answer_id"])),
			"occurred_at": now.UTC().Format(time.RFC3339Nano),
			"confidence":  selfConf,
			"latency_ms":  latencyMS,
		})
		if len(arr) > 20 {
			arr = arr[len(arr)-20:]
		}
		if b, err := json.Marshal(arr); err == nil {
			st.Misconceptions = datatypes.JSON(b)
		}
	}
}

func expectedCorrectnessForQuestion(st *types.UserConceptState, seenAt time.Time, data map[string]any) float64 {
	if st == nil {
		st = &types.UserConceptState{
			Mastery:    0,
			BktPGuess:  ktBktGuessDefault,
			BktPForget: ktBktForgetDefault,
		}
	}
	itemType := strings.ToLower(strings.TrimSpace(fmt.Sprint(data["item_type"])))
	itemOptions := intFromAny(data["item_options"], 0)
	itemGuess := clamp01(floatFromAny(data["item_guess"], 0))
	if itemGuess <= 0 {
		if itemOptions > 1 {
			itemGuess = 1.0 / float64(itemOptions)
		} else if itemType == "true_false" {
			itemGuess = 0.5
		}
	}
	if itemGuess <= 0 {
		itemGuess = defaultBktGuess
	}
	itemDisc := floatFromAny(data["item_discrimination"], 1.0)
	if itemDisc <= 0 {
		itemDisc = 1.0
	}
	if itemDisc > 3 {
		itemDisc = 3
	}
	itemDiff := floatFromAny(data["item_difficulty"], math.NaN())
	if math.IsNaN(itemDiff) {
		itemDiff = floatFromAny(data["node_difficulty"], math.NaN())
	}
	if math.IsNaN(itemDiff) {
		itemDiff = 0
	}
	itemDiff = normalizeItemDifficulty(itemDiff)

	if theta := floatFromAny(data["user_theta"], math.NaN()); !math.IsNaN(theta) {
		return clamp01(irtProb(theta, itemDisc, itemDiff, itemGuess))
	}

	prevM := clamp01(st.Mastery)

	_, _, _, pForget := ensureBktParams(st)
	pKnown := prevM
	if st.LastSeenAt != nil && !st.LastSeenAt.IsZero() && !seenAt.IsZero() && seenAt.After(*st.LastSeenAt) {
		deltaDays := seenAt.Sub(*st.LastSeenAt).Hours() / 24.0
		if deltaDays > 0 && pForget > 0 {
			pKnown = pKnown * math.Exp(-pForget*deltaDays)
		}
	}
	pKnown = clamp01(pKnown)
	theta := logit(pKnown)
	pCorrect := irtProb(theta, itemDisc, itemDiff, itemGuess)
	return clamp01(pCorrect)
}

func evidenceSignalForEvent(typ string, data map[string]any) (float64, float64) {
	typ = strings.TrimSpace(typ)
	conf := clamp01(floatFromAny(data["confidence"], 0))
	if conf == 0 {
		conf = clamp01(floatFromAny(data["grader_confidence"], 0))
	}
	if conf == 0 {
		conf = clamp01(floatFromAny(data["verdict_confidence"], 0))
	}
	strength := 0.3
	switch typ {
	case types.EventQuestionAnswered:
		strength = 1.0
		if conf == 0 {
			conf = 0.65
		}
	case types.EventConceptClaimEvaluated:
		strength = 0.7
		if conf == 0 {
			conf = 0.5
		}
	case types.EventHintUsed:
		strength = 0.35
		if conf == 0 {
			conf = 0.5
		}
	case types.EventActivityCompleted:
		strength = 0.55
		if conf == 0 {
			conf = 0.6
		}
	case types.EventScrollDepth, types.EventBlockViewed, types.EventBlockRead:
		strength = 0.2
		if conf == 0 {
			conf = 0.35
		}
	case "propagation":
		strength = 0.15
		if conf == 0 {
			conf = 0.5
		}
	case "bridge_transfer":
		strength = 0.22
		if conf == 0 {
			conf = 0.6
		}
	}
	if evStrength := clamp01(floatFromAny(data["evidence_strength"], 0)); evStrength > 0 {
		strength = clamp01(strength * (0.5 + 0.8*evStrength))
		if conf == 0 {
			conf = evStrength
		} else {
			conf = clamp01(0.8*conf + 0.2*evStrength)
		}
	}
	return clamp01(strength), clamp01(conf)
}

func applyClaimEvaluatedToState(st *types.UserConceptState, seenAt time.Time, data map[string]any) {
	if st == nil {
		return
	}
	if !boolFromAny(data["has_truth"], false) {
		setLastSeen(st, seenAt)
		return
	}
	isCorrect := boolFromAny(data["is_correct"], false)
	strength := clamp01(floatFromAny(data["signal_strength"], 0))
	if strength == 0 {
		strength = clamp01(floatFromAny(data["attribution_score"], 0))
	}
	if strength == 0 {
		strength = 0.25
	}

	m := clamp01(st.Mastery)
	c := clamp01(st.Confidence)
	delta := 0.02 + 0.05*strength
	if isCorrect {
		m = clamp01(m + (1.0-m)*delta)
		c = clamp01(c + 0.2*delta)
	} else {
		m = clamp01(m - (m * delta * 0.6))
		c = clamp01(c - (c * delta * 0.4))
	}
	if boolFromAny(data["is_confusion"], false) {
		c = clamp01(c * 0.9)
		st.EpistemicUncertainty = clamp01(st.EpistemicUncertainty + 0.08)
	}
	st.Mastery = m
	st.Confidence = c
	setLastSeen(st, seenAt)
}

func applyActivityCompletedToState(st *types.UserConceptState, seenAt time.Time, data map[string]any) {
	if st == nil {
		return
	}
	score := clamp01(floatFromAny(data["score"], 0))
	if score == 0 {
		score = 0.6 // weak positive default when completion has no explicit score
	}

	m := clamp01(st.Mastery)
	c := clamp01(st.Confidence)

	// Completion is weaker evidence than assessment; nudge upward conservatively.
	alpha := 0.02 + 0.04*score
	m = m + (1.0-m)*(alpha*0.60)
	c = c + (1.0-c)*(0.02*score)

	st.Mastery = clamp01(m)
	st.Confidence = clamp01(c)
	setLastSeen(st, seenAt)
}

func applyExposureToState(st *types.UserConceptState, seenAt time.Time, data map[string]any) {
	if st == nil {
		return
	}
	dwellMS := float64(intFromAny(data["dwell_ms"], 0))
	maxPct := clamp01(floatFromAny(data["max_percent"], floatFromAny(data["percent"], 0)) / 100.0)
	if maxPct <= 0 {
		maxPct = 0.3
	}
	// Scale exposure weight by dwell time (cap at ~2 minutes) and scroll depth.
	w := clamp01((dwellMS / 120000.0) * maxPct)
	readCredit := clamp01(floatFromAny(data["read_credit"], 0))
	if readCredit > 0 {
		// Read credit is a stronger signal than raw dwell. Blend conservatively.
		w = clamp01(w + 0.45*readCredit)
	}
	c := clamp01(st.Confidence)
	c = clamp01(c + 0.06*w)
	if c > maxExposureConfidence {
		c = maxExposureConfidence
	}
	st.Confidence = c
	setLastSeen(st, seenAt)
}

func applyHintUsedToState(st *types.UserConceptState, seenAt time.Time, data map[string]any) {
	if st == nil {
		return
	}

	m := clamp01(st.Mastery)
	c := clamp01(st.Confidence)

	// Hints are a negative signal: user needed help to proceed.
	//
	// We penalize confidence more than mastery (hints imply uncertainty). The penalty is larger when
	// the user's current strength is low, and smaller when they're already confident/mastered.
	strength := clamp01(m * c)
	penalty := 0.03 + 0.05*(1.0-strength)
	c = clamp01(c - penalty)

	// If the user was previously "very mastered" but still needed hints, nudge mastery down slightly.
	if m >= 0.90 {
		m = clamp01(m - 0.02)
	}

	st.Mastery = m
	st.Confidence = c
	setLastSeen(st, seenAt)

	// Schedule near-term review (but do not delay an already-sooner review).
	now := seenAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	soon := now.Add(6 * time.Hour).UTC()
	if st.NextReviewAt == nil || st.NextReviewAt.IsZero() || soon.Before(*st.NextReviewAt) {
		st.NextReviewAt = &soon
	}

	// Track a small rolling "help needed" log for prompt-time misconception hints (best-effort).
	var arr []map[string]any
	if len(st.Misconceptions) > 0 && string(st.Misconceptions) != "null" {
		_ = json.Unmarshal(st.Misconceptions, &arr)
	}
	arr = append(arr, map[string]any{
		"kind":        "hint_used",
		"question_id": strings.TrimSpace(fmt.Sprint(data["question_id"])),
		"block_id":    strings.TrimSpace(fmt.Sprint(data["block_id"])),
		"occurred_at": now.UTC().Format(time.RFC3339Nano),
	})
	if len(arr) > 20 {
		arr = arr[len(arr)-20:]
	}
	if b, err := json.Marshal(arr); err == nil {
		st.Misconceptions = datatypes.JSON(b)
	}
}

func ensureSkillState(prev *types.UserSkillState, userID uuid.UUID, conceptID uuid.UUID, thetaSeed float64) *types.UserSkillState {
	if prev != nil {
		return prev
	}
	if math.IsNaN(thetaSeed) {
		thetaSeed = 0
	}
	return &types.UserSkillState{
		ID:        uuid.New(),
		UserID:    userID,
		ConceptID: conceptID,
		Theta:     clampRange(thetaSeed, -3.0, 3.0),
		Sigma:     1.0,
		Count:     0,
	}
}

func itemParamsForData(data map[string]any) (float64, float64, float64) {
	itemType := strings.ToLower(strings.TrimSpace(fmt.Sprint(data["item_type"])))
	itemOptions := intFromAny(data["item_options"], 0)
	itemGuess := clamp01(floatFromAny(data["item_guess"], 0))
	if itemGuess <= 0 {
		if itemOptions > 1 {
			itemGuess = 1.0 / float64(itemOptions)
		} else if itemType == "true_false" {
			itemGuess = 0.5
		}
	}
	if itemGuess <= 0 {
		itemGuess = defaultBktGuess
	}
	itemGuess = clampRange(itemGuess, 0, itemCalibGuessMax)

	itemDisc := floatFromAny(data["item_discrimination"], 1.0)
	if itemDisc <= 0 {
		itemDisc = 1.0
	}
	itemDisc = clampRange(itemDisc, itemCalibDiscMin, itemCalibDiscMax)

	itemDiff := floatFromAny(data["item_difficulty"], math.NaN())
	if math.IsNaN(itemDiff) {
		itemDiff = floatFromAny(data["node_difficulty"], math.NaN())
	}
	if math.IsNaN(itemDiff) {
		itemDiff = 0
	}
	itemDiff = normalizeItemDifficulty(itemDiff)
	itemDiff = clampRange(itemDiff, itemCalibDiffMin, itemCalibDiffMax)
	return itemGuess, itemDisc, itemDiff
}

func ensureItemCalibration(prev *types.ItemCalibration, itemID string, itemType string, conceptID *uuid.UUID, data map[string]any) *types.ItemCalibration {
	if prev != nil {
		return prev
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return nil
	}
	itemType = strings.TrimSpace(itemType)
	if itemType == "" {
		itemType = "question"
	}
	guess, disc, diff := itemParamsForData(data)
	slip := clamp01(floatFromAny(data["item_slip"], 0))
	row := &types.ItemCalibration{
		ID:             uuid.New(),
		ItemID:         itemID,
		ItemType:       itemType,
		ConceptID:      conceptID,
		Difficulty:     diff,
		Discrimination: disc,
		Guess:          guess,
		Slip:           slip,
		Count:          0,
		Correct:        0,
	}
	return row
}

func computeEvidenceStrength(data map[string]any, item *types.ItemCalibration) float64 {
	conf := clamp01(floatFromAny(data["confidence"], 0))
	if conf == 0 {
		conf = clamp01(floatFromAny(data["grader_confidence"], 0))
	}
	if conf == 0 {
		conf = clamp01(floatFromAny(data["grader_confidence_pct"], 0) / 100.0)
	}
	if conf == 0 {
		conf = clamp01(floatFromAny(data["verdict_confidence"], 0))
	}

	guess, disc, _ := itemParamsForData(data)
	if item != nil {
		if item.Discrimination > 0 {
			disc = item.Discrimination
		}
		if item.Guess > 0 {
			guess = item.Guess
		}
	}

	discRange := itemCalibDiscMax - itemCalibDiscMin
	discNorm := 0.5
	if discRange > 0 {
		discNorm = clamp01((disc - itemCalibDiscMin) / discRange)
	}
	guessQuality := clamp01(1.0 - 1.4*guess)
	if guessQuality < 0.2 {
		guessQuality = 0.2
	}

	latencyMS := intFromAny(data["latency_ms"], 0)
	latQuality := 0.7
	if latencyMS > 0 {
		switch {
		case latencyMS < 800:
			latQuality = 0.4
		case latencyMS < 1500:
			latQuality = 0.6
		case latencyMS <= 12000:
			latQuality = 1.0
		case latencyMS <= 30000:
			latQuality = 0.75
		case latencyMS <= 60000:
			latQuality = 0.5
		default:
			latQuality = 0.3
		}
	}

	sampleQuality := 0.0
	if item != nil && item.Count > 0 {
		sampleQuality = clamp01(math.Sqrt(float64(item.Count) / 30.0))
	}

	strength := 0.2 +
		0.25*conf +
		0.20*discNorm +
		0.15*latQuality +
		0.15*sampleQuality +
		0.05*guessQuality
	return clamp01(strength)
}

func applyQuestionAnsweredToSkill(st *types.UserSkillState, isCorrect bool, seenAt time.Time, data map[string]any, evidenceStrength float64) {
	if st == nil {
		return
	}
	guess, disc, diff := itemParamsForData(data)
	theta := st.Theta
	if math.IsNaN(theta) {
		theta = 0
	}
	y := 0.0
	if isCorrect {
		y = 1.0
	}
	s := sigmoid(disc * (theta - diff))
	p := guess + (1.0-guess)*s
	err := y - p
	slope := s * (1.0 - s) * (1.0 - guess)
	grad := err * disc * slope
	lr := skillLR
	if evidenceStrength > 0 {
		lr *= 0.6 + 0.7*evidenceStrength
	}
	if st.Sigma > 0 {
		lr *= clampRange(1.0-(0.4*st.Sigma), 0.35, 1.0)
	}
	theta = theta + lr*grad
	st.Theta = clampRange(theta, -3.5, 3.5)
	st.Count += 1

	sigma := st.Sigma
	if sigma <= 0 {
		sigma = 1.0
	}
	decay := skillSigmaDecay * (0.4 + 0.6*clamp01(evidenceStrength))
	sigma = sigma*(1.0-decay) + 0.08*math.Abs(err)
	st.Sigma = clampRange(sigma, skillSigmaMin, skillSigmaMax)

	if !seenAt.IsZero() {
		t := seenAt.UTC()
		st.LastEventAt = &t
	}
}

func applyQuestionAnsweredToItemCalibration(item *types.ItemCalibration, theta float64, isCorrect bool, seenAt time.Time, data map[string]any, evidenceStrength float64) {
	if item == nil {
		return
	}
	if math.IsNaN(theta) {
		theta = 0
	}

	guessTarget, _, _ := itemParamsForData(data)
	if guessTarget > 0 {
		if item.Guess <= 0 {
			item.Guess = guessTarget
		} else {
			item.Guess = clampRange(0.9*item.Guess+0.1*guessTarget, 0, itemCalibGuessMax)
		}
	}
	slipTarget := clamp01(floatFromAny(data["item_slip"], 0))
	if slipTarget > 0 {
		if item.Slip <= 0 {
			item.Slip = slipTarget
		} else {
			item.Slip = clampRange(0.9*item.Slip+0.1*slipTarget, 0, 0.5)
		}
	}

	disc := item.Discrimination
	if disc <= 0 {
		disc = 1.0
	}
	diff := item.Difficulty
	guess := item.Guess
	if guess <= 0 {
		guess = guessTarget
	}
	guess = clampRange(guess, 0, itemCalibGuessMax)

	y := 0.0
	if isCorrect {
		y = 1.0
	}
	s := sigmoid(disc * (theta - diff))
	p := guess + (1.0-guess)*s
	err := y - p

	lr := itemCalibLR
	if evidenceStrength > 0 {
		lr *= 0.6 + 0.6*evidenceStrength
	}
	if item.Count > 0 {
		lr *= 1.0 / math.Sqrt(1.0+(float64(item.Count)/10.0))
	}

	grad := err * s * (1.0 - s) * (1.0 - guess)
	diff0 := diff
	diff = diff + lr*(-disc)*grad
	disc = disc + lr*(theta-diff0)*grad

	item.Difficulty = clampRange(diff, itemCalibDiffMin, itemCalibDiffMax)
	item.Discrimination = clampRange(disc, itemCalibDiscMin, itemCalibDiscMax)
	item.Guess = clampRange(guess, 0, itemCalibGuessMax)

	item.Count += 1
	if isCorrect {
		item.Correct += 1
	}
	if !seenAt.IsZero() {
		t := seenAt.UTC()
		item.LastEventAt = &t
	}
}

// ---- helpers ----

// extractUUIDsFromAny supports []any, []string, single string, etc.
func extractUUIDsFromAny(v any) []uuid.UUID {
	if v == nil {
		return nil
	}

	// []string
	if ss, ok := v.([]string); ok {
		out := make([]uuid.UUID, 0, len(ss))
		for _, s := range ss {
			id, err := uuid.Parse(strings.TrimSpace(s))
			if err == nil && id != uuid.Nil {
				out = append(out, id)
			}
		}
		return out
	}

	// []any
	if arr, ok := v.([]any); ok {
		out := make([]uuid.UUID, 0, len(arr))
		for _, x := range arr {
			id, err := uuid.Parse(strings.TrimSpace(fmt.Sprint(x)))
			if err == nil && id != uuid.Nil {
				out = append(out, id)
			}
		}
		return out
	}

	// single string/uuid-like
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" {
		return nil
	}
	if id, err := uuid.Parse(s); err == nil && id != uuid.Nil {
		return []uuid.UUID{id}
	}
	return nil
}

func boolFromAny(v any, def bool) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "true" || s == "1" || s == "yes"
	case float64:
		return t != 0
	default:
		return def
	}
}

func stringFromAny(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func intFromAny(v any, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		i, _ := t.Int64()
		return int(i)
	default:
		return def
	}
}

func floatFromAny(v any, def float64) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, err := t.Float64()
		if err == nil {
			return f
		}
		return def
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return def
		}
		n := json.Number(s)
		f, err := n.Float64()
		if err == nil {
			return f
		}
		return def
	default:
		return def
	}
}

func clamp01(x float64) float64 {
	if math.IsNaN(x) {
		return 0
	}
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func clampRange(x float64, lo float64, hi float64) float64 {
	if math.IsNaN(x) {
		return lo
	}
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

func ensureBktParams(st *types.UserConceptState) (float64, float64, float64, float64) {
	pLearn := clamp01(st.BktPLearn)
	if pLearn <= 0 {
		pLearn = ktBktLearnDefault
	}
	pGuess := clamp01(st.BktPGuess)
	if pGuess <= 0 {
		pGuess = ktBktGuessDefault
	}
	pSlip := clamp01(st.BktPSlip)
	if pSlip <= 0 {
		pSlip = ktBktSlipDefault
	}
	pForget := clamp01(st.BktPForget)
	if pForget <= 0 {
		pForget = ktBktForgetDefault
	}
	return pLearn, pGuess, pSlip, pForget
}

func normalizeItemDifficulty(x float64) float64 {
	if math.IsNaN(x) {
		return 0
	}
	if x >= 0 && x <= 1 {
		// Map [0,1] -> [-1,1] on theta scale.
		return clampRange((x-0.5)*2.0, -1.5, 1.5)
	}
	return clampRange(x, -2.5, 2.5)
}

func bktPosterior(pKnown float64, isCorrect bool, pGuess float64, pSlip float64) float64 {
	pKnown = clamp01(pKnown)
	pGuess = clamp01(pGuess)
	pSlip = clamp01(pSlip)
	if isCorrect {
		num := pKnown * (1.0 - pSlip)
		den := num + (1.0-pKnown)*pGuess
		if den > 0 {
			return clamp01(num / den)
		}
		return pKnown
	}
	num := pKnown * pSlip
	den := num + (1.0-pKnown)*(1.0-pGuess)
	if den > 0 {
		return clamp01(num / den)
	}
	return pKnown
}

func logit(p float64) float64 {
	p = clampRange(p, 0.001, 0.999)
	return math.Log(p / (1.0 - p))
}

func sigmoid(x float64) float64 {
	if x >= 0 {
		z := math.Exp(-x)
		return 1.0 / (1.0 + z)
	}
	z := math.Exp(x)
	return z / (1.0 + z)
}

func irtProb(theta float64, a float64, b float64, g float64) float64 {
	a = clampRange(a, 0.2, 3.0)
	b = clampRange(b, -3.0, 3.0)
	g = clampRange(g, 0.0, 0.45)
	p := sigmoid(a * (theta - b))
	return g + (1.0-g)*p
}

func updateUncertainty(st *types.UserConceptState, selfConf float64, graderConf float64, seenAt time.Time) (float64, float64) {
	attempts := float64(st.Attempts)
	epiTarget := 1.0 / math.Sqrt(attempts+1.0)
	if st.LastSeenAt != nil && !st.LastSeenAt.IsZero() && !seenAt.IsZero() && seenAt.After(*st.LastSeenAt) {
		deltaDays := seenAt.Sub(*st.LastSeenAt).Hours() / 24.0
		if deltaDays > 14 {
			epiTarget = clamp01(epiTarget + (deltaDays / 90.0))
		}
	}
	if epiTarget < 0.05 {
		epiTarget = 0.05
	}
	epiPrev := clamp01(st.EpistemicUncertainty)
	epi := clamp01(epiPrev*0.75 + epiTarget*0.25)

	accuracy := 0.5
	if st.Attempts > 0 {
		accuracy = float64(st.Correct) / float64(st.Attempts)
	}
	aleaTarget := clamp01(4.0 * accuracy * (1.0 - accuracy))
	if selfConf > 0 {
		aleaTarget = clamp01(0.7*aleaTarget + 0.3*(1.0-selfConf))
	}
	if graderConf > 0 {
		aleaTarget = clamp01(0.8*aleaTarget + 0.2*(1.0-graderConf))
	}
	aleaPrev := clamp01(st.AleatoricUncertainty)
	alea := clamp01(aleaPrev*0.75 + aleaTarget*0.25)

	return epi, alea
}

func scheduleNextReview(st *types.UserConceptState, seenAt time.Time, isCorrect bool) {
	if st == nil {
		return
	}
	now := seenAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	epi := clamp01(st.EpistemicUncertainty)
	alea := clamp01(st.AleatoricUncertainty)
	strength := clamp01(st.Mastery) * clamp01(1.0-0.5*(epi+alea))

	base := 0.6 + (10.0 * strength * strength)
	if !isCorrect {
		base *= 0.55
	}
	if st.HalfLifeDays > 0 {
		base = 0.5*st.HalfLifeDays + 0.5*base
	}
	halfLife := clampRange(base, ktMinHalfLifeDays, ktMaxHalfLifeDays)
	st.HalfLifeDays = halfLife

	target := ktReviewTargetCorrect
	if !isCorrect {
		target = ktReviewTargetIncorrect
	}
	days := -math.Log(target) * halfLife
	if days < 0.10 {
		days = 0.10
	}
	if days > 365 {
		days = 365
	}
	next := now.Add(time.Duration(days*24) * time.Hour).UTC()
	if st.NextReviewAt == nil || st.NextReviewAt.IsZero() {
		st.NextReviewAt = &next
	} else if isCorrect && next.After(*st.NextReviewAt) {
		st.NextReviewAt = &next
	} else if !isCorrect && next.Before(*st.NextReviewAt) {
		st.NextReviewAt = &next
	}

	if halfLife > 0 {
		st.DecayRate = clamp01(math.Ln2 / halfLife)
	}
}

// ---- structural model helpers ----

type supportPointer struct {
	SourceType string  `json:"source_type"`
	SourceID   string  `json:"source_id"`
	OccurredAt string  `json:"occurred_at,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

type uncertaintySignal struct {
	Kind       string  `json:"kind"`
	Confidence float64 `json:"confidence"`
	LastSeenAt string  `json:"last_seen_at,omitempty"`
	Count      int     `json:"count,omitempty"`
}

func ensureConceptModel(prev *types.UserConceptModel, userID uuid.UUID, conceptID uuid.UUID) *types.UserConceptModel {
	if prev != nil {
		return prev
	}
	return &types.UserConceptModel{
		ID:                 uuid.New(),
		UserID:             userID,
		CanonicalConceptID: conceptID,
		ModelVersion:       1,
	}
}

func loadSupportPointers(raw []byte) []supportPointer {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	var out []supportPointer
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func addSupportPointer(list []supportPointer, ptr supportPointer, max int) ([]supportPointer, bool) {
	if ptr.SourceType == "" || ptr.SourceID == "" {
		return list, false
	}
	for _, it := range list {
		if it.SourceType == ptr.SourceType && it.SourceID == ptr.SourceID {
			return list, false
		}
	}
	list = append(list, ptr)
	if max > 0 && len(list) > max {
		list = list[len(list)-max:]
	}
	return list, true
}

func loadUncertainty(raw []byte) []uncertaintySignal {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	var out []uncertaintySignal
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func upsertUncertainty(list []uncertaintySignal, kind string, conf float64, seenAt time.Time) []uncertaintySignal {
	kind = strings.TrimSpace(strings.ToLower(kind))
	if kind == "" {
		return list
	}
	now := seenAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	for i := range list {
		if list[i].Kind == kind {
			if conf > list[i].Confidence {
				list[i].Confidence = conf
			}
			list[i].Count += 1
			list[i].LastSeenAt = now.Format(time.RFC3339Nano)
			return list
		}
	}
	list = append(list, uncertaintySignal{
		Kind:       kind,
		Confidence: conf,
		Count:      1,
		LastSeenAt: now.Format(time.RFC3339Nano),
	})
	return list
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func applyIncorrectAnswerToModel(model *types.UserConceptModel, seenAt time.Time, data map[string]any, eventType string) (bool, *types.UserMisconceptionInstance) {
	if model == nil {
		return false, nil
	}
	qid := strings.TrimSpace(fmt.Sprint(data["question_id"]))
	desc := "incorrect_answer"
	if qid != "" {
		desc = "incorrect_answer question_id=" + qid
	}
	conf := clamp01(floatFromAny(data["grader_confidence"], floatFromAny(data["confidence"], 0.6)))
	if conf == 0 {
		conf = 0.6
	}
	sourceID := strings.TrimSpace(fmt.Sprint(data["client_event_id"]))
	if sourceID == "" {
		sourceID = strings.TrimSpace(fmt.Sprint(data["event_id"]))
	}

	ptr := supportPointer{
		SourceType: "user_event",
		SourceID:   sourceID,
		OccurredAt: seenAt.UTC().Format(time.RFC3339Nano),
		Confidence: conf,
	}
	modelSupport := loadSupportPointers([]byte(model.Support))
	modelSupport, added := addSupportPointer(modelSupport, ptr, 20)
	if added {
		model.Support = datatypes.JSON(mustJSON(modelSupport))
		t := seenAt.UTC()
		model.LastStructuralAt = &t
	}

	pattern := "incorrect_answer"
	mis := &types.UserMisconceptionInstance{
		UserID:             model.UserID,
		CanonicalConceptID: model.CanonicalConceptID,
		PatternID:          &pattern,
		Description:        desc,
		Status:             "active",
		Confidence:         conf,
	}
	if !seenAt.IsZero() {
		t := seenAt.UTC()
		mis.FirstSeenAt = &t
		mis.LastSeenAt = &t
	}

	signature := inferMisconceptionSignature(eventType, data, "procedural_gap")
	misSupport := types.DecodeMisconceptionSupport(mis.Support)
	misSupport.SignatureType = signature
	misSupport = types.MergeMisconceptionSupportPointer(misSupport, types.MisconceptionSupportPointer{
		SourceType: ptr.SourceType,
		SourceID:   ptr.SourceID,
		OccurredAt: ptr.OccurredAt,
		Confidence: ptr.Confidence,
	}, 20)
	if ctx := misconceptionContextFromData(data); ctx != "" {
		misSupport = types.AddMisconceptionTriggerContext(misSupport, ctx, 12)
	}
	mis.Support = types.EncodeMisconceptionSupport(misSupport)
	return added, mis
}

func inferMisconceptionSignature(eventType string, data map[string]any, fallback string) string {
	if data != nil {
		if v := strings.TrimSpace(stringFromAny(data["signature_type"])); v != "" {
			return types.NormalizeMisconceptionSignature(v)
		}
	}
	eventType = strings.TrimSpace(strings.ToLower(eventType))
	polarity := strings.TrimSpace(strings.ToLower(stringFromAny(data["polarity"])))
	scope := strings.TrimSpace(strings.ToLower(stringFromAny(data["scope"])))

	if eventType == strings.ToLower(types.EventConceptClaimEvaluated) || eventType == "concept_claim_evaluated" {
		if polarity == "confusion" {
			return "frame_error"
		}
		if polarity == "confident_wrong" {
			switch scope {
			case "explanation", "assertion":
				return "frame_error"
			case "question", "attempt":
				return "procedural_gap"
			}
			return "frame_error"
		}
	}
	if eventType == strings.ToLower(types.EventHintUsed) || eventType == "hint_used" {
		return "procedural_gap"
	}
	if boolFromAny(data["transfer_context"], false) || boolFromAny(data["transfer_failure"], false) {
		return "transfer_failure"
	}
	if _, ok := data["transfer_success"]; ok {
		if !boolFromAny(data["transfer_success"], false) {
			return "transfer_failure"
		}
	}
	if fallback != "" {
		return types.NormalizeMisconceptionSignature(fallback)
	}
	return "unknown"
}

func misconceptionContextFromData(data map[string]any) string {
	if data == nil {
		return ""
	}
	if v := strings.TrimSpace(stringFromAny(data["question_id"])); v != "" {
		return "question:" + v
	}
	if v := strings.TrimSpace(stringFromAny(data["block_id"])); v != "" {
		return "block:" + v
	}
	if v := strings.TrimSpace(stringFromAny(data["message_id"])); v != "" {
		return "message:" + v
	}
	return ""
}

func applyHintToModel(model *types.UserConceptModel, seenAt time.Time, data map[string]any) bool {
	if model == nil {
		return false
	}
	sourceID := strings.TrimSpace(fmt.Sprint(data["client_event_id"]))
	if sourceID == "" {
		sourceID = strings.TrimSpace(fmt.Sprint(data["event_id"]))
	}
	ptr := supportPointer{
		SourceType: "user_event",
		SourceID:   sourceID,
		OccurredAt: seenAt.UTC().Format(time.RFC3339Nano),
		Confidence: 0.5,
	}
	support := loadSupportPointers([]byte(model.Support))
	support, added := addSupportPointer(support, ptr, 20)
	unc := loadUncertainty([]byte(model.Uncertainty))
	unc = upsertUncertainty(unc, "procedural_gap", 0.5, seenAt)
	if added {
		model.Support = datatypes.JSON(mustJSON(support))
		model.Uncertainty = datatypes.JSON(mustJSON(unc))
		t := seenAt.UTC()
		model.LastStructuralAt = &t
		return true
	}
	// Even if support already existed, update uncertainty if empty.
	model.Uncertainty = datatypes.JSON(mustJSON(unc))
	return true
}

func applyExposureToModel(model *types.UserConceptModel, seenAt time.Time, data map[string]any) bool {
	if model == nil {
		return false
	}
	dwellMS := intFromAny(data["dwell_ms"], 0)
	maxPct := floatFromAny(data["max_percent"], floatFromAny(data["percent"], 0))
	if maxPct > 1.0 {
		maxPct = maxPct / 100.0
	}
	if dwellMS < 2500 && maxPct < 0.45 {
		return false
	}
	conf := 0.2
	if dwellMS >= 8000 || maxPct >= 0.85 {
		conf = 0.35
	} else if dwellMS >= 4000 || maxPct >= 0.60 {
		conf = 0.25
	}
	sourceID := strings.TrimSpace(fmt.Sprint(data["client_event_id"]))
	if sourceID == "" {
		sourceID = strings.TrimSpace(fmt.Sprint(data["event_id"]))
	}
	ptr := supportPointer{
		SourceType: "exposure",
		SourceID:   sourceID,
		OccurredAt: seenAt.UTC().Format(time.RFC3339Nano),
		Confidence: conf,
	}
	support := loadSupportPointers([]byte(model.Support))
	support, added := addSupportPointer(support, ptr, 20)
	if added {
		model.Support = datatypes.JSON(mustJSON(support))
		t := seenAt.UTC()
		model.LastStructuralAt = &t
		return true
	}
	return false
}

func applyRetryToModel(model *types.UserConceptModel, seenAt time.Time, data map[string]any, attempt int) bool {
	if model == nil || attempt < 2 {
		return false
	}
	isCorrect := boolFromAny(data["is_correct"], false)
	conf := 0.5
	if !isCorrect {
		conf = 0.65
	}
	sourceID := strings.TrimSpace(fmt.Sprint(data["client_event_id"]))
	if sourceID == "" {
		sourceID = strings.TrimSpace(fmt.Sprint(data["event_id"]))
	}
	ptr := supportPointer{
		SourceType: "retry",
		SourceID:   sourceID,
		OccurredAt: seenAt.UTC().Format(time.RFC3339Nano),
		Confidence: conf,
	}
	support := loadSupportPointers([]byte(model.Support))
	support, added := addSupportPointer(support, ptr, 20)
	unc := loadUncertainty([]byte(model.Uncertainty))
	unc = upsertUncertainty(unc, "procedural_gap", conf, seenAt)
	if added {
		model.Support = datatypes.JSON(mustJSON(support))
		model.Uncertainty = datatypes.JSON(mustJSON(unc))
		t := seenAt.UTC()
		model.LastStructuralAt = &t
		return true
	}
	model.Uncertainty = datatypes.JSON(mustJSON(unc))
	return false
}

func propagationWeightForEdge(edgeType string) float64 {
	switch strings.ToLower(strings.TrimSpace(edgeType)) {
	case "prereq":
		return propagationPrereqWeight
	case "related":
		return propagationRelatedWeight
	case "analogy":
		return propagationAnalogyWeight
	default:
		return 0
	}
}

func canPropagateFrom(st *types.UserConceptState, minMastery float64, minConf float64, maxEpi float64, maxAlea float64) bool {
	if st == nil {
		return false
	}
	if clamp01(st.Mastery) < minMastery {
		return false
	}
	if clamp01(st.Confidence) < minConf {
		return false
	}
	if clamp01(st.EpistemicUncertainty) > maxEpi {
		return false
	}
	if clamp01(st.AleatoricUncertainty) > maxAlea {
		return false
	}
	return true
}

func applyPropagationDelta(from *types.UserConceptState, to *types.UserConceptState, weight float64, maxDelta float64) bool {
	if from == nil || to == nil {
		return false
	}
	weight = clampRange(weight, 0, 1)
	if weight == 0 {
		return false
	}
	fromM := clamp01(from.Mastery)
	toM := clamp01(to.Mastery)
	if fromM <= toM {
		return false
	}
	rawDelta := (fromM - toM) * weight
	if rawDelta <= 0 {
		return false
	}
	delta := rawDelta
	if maxDelta > 0 {
		delta = math.Min(delta, maxDelta)
	}
	toM = clamp01(toM + delta)
	if toM > fromM {
		toM = fromM
	}
	to.Mastery = toM
	to.Confidence = clamp01(math.Max(clamp01(to.Confidence), clamp01(from.Confidence*0.5)))
	to.EpistemicUncertainty = clamp01(to.EpistemicUncertainty * (1.0 - 0.2*delta))
	return true
}

func hoursToDuration(hours float64) time.Duration {
	if hours <= 0 {
		return 0
	}
	return time.Duration(hours * float64(time.Hour))
}

func shouldRequestBridgeValidation(last *time.Time, now time.Time, cooldown time.Duration) bool {
	if cooldown <= 0 {
		return true
	}
	if last == nil || last.IsZero() {
		return true
	}
	return now.Sub(*last) >= cooldown
}

func bridgeValidationBucket(now time.Time, cooldown time.Duration) int64 {
	if cooldown <= 0 {
		return now.Unix()
	}
	sec := int64(cooldown.Seconds())
	if sec <= 0 {
		return now.Unix()
	}
	return now.Unix() / sec
}

func ptrTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	tt := t.UTC()
	return &tt
}

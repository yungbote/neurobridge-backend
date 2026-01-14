package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/keys"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type CompletedUnitRefreshDeps struct {
	DB        *gorm.DB
	Log       *logger.Logger
	Completed repos.UserCompletedUnitRepo
	Progress  repos.UserProgressionEventRepo
	Concepts  repos.ConceptRepo
	Act       repos.ActivityRepo
	ActCon    repos.ActivityConceptRepo
	Chains    repos.ChainSignatureRepo
	Mastery   repos.UserConceptStateRepo
	Graph     *neo4jdb.Client
	Bootstrap services.LearningBuildBootstrapService
}

type CompletedUnitRefreshInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
}

type CompletedUnitRefreshOutput struct {
	Noop            bool `json:"noop"`
	UnitsUpserted   int  `json:"units_upserted"`
	UnitsCompleted  int  `json:"units_completed"`
	ChainsEvaluated int  `json:"chains_evaluated"`
}

func CompletedUnitRefresh(ctx context.Context, deps CompletedUnitRefreshDeps, in CompletedUnitRefreshInput) (CompletedUnitRefreshOutput, error) {
	out := CompletedUnitRefreshOutput{Noop: true}
	if deps.DB == nil || deps.Log == nil || deps.Bootstrap == nil ||
		deps.Completed == nil || deps.Progress == nil ||
		deps.Concepts == nil || deps.Act == nil || deps.ActCon == nil || deps.Chains == nil {
		return out, fmt.Errorf("completed_unit_refresh: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("completed_unit_refresh: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("completed_unit_refresh: missing material_set_id")
	}

	pathID, err := resolvePathID(ctx, deps.Bootstrap, in.OwnerUserID, in.MaterialSetID, in.PathID)
	if err != nil {
		return out, err
	}

	// Conservative default: require high confidence to mark completed.
	const threshold = 0.85
	const maxEvents = 5000

	type chainSig struct {
		ChainKey string
		KeySet   map[string]bool
		Size     int
	}

	bestChainKey := func(chains []chainSig, conceptKeys []string) string {
		if len(chains) == 0 || len(conceptKeys) == 0 {
			return ""
		}
		ckeys := dedupeStrings(conceptKeys)
		best := ""
		bestHit := -1
		bestSize := math.MaxInt
		for _, ch := range chains {
			hits := 0
			for _, k := range ckeys {
				if ch.KeySet[k] {
					hits++
				}
			}
			if hits == 0 {
				continue
			}
			// Prefer higher overlap; on ties prefer smaller chains; final tie lexicographic.
			if hits > bestHit || (hits == bestHit && ch.Size < bestSize) || (hits == bestHit && ch.Size == bestSize && ch.ChainKey < best) {
				bestHit = hits
				bestSize = ch.Size
				best = ch.ChainKey
			}
		}
		return best
	}

	err = deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}

		// Chain signatures (chains to evaluate).
		chainRows, err := deps.Chains.ListByScope(dbc, "path", &pathID)
		if err != nil {
			return err
		}
		if len(chainRows) == 0 {
			return nil
		}

		chains := make([]chainSig, 0, len(chainRows))
		chainConceptKeys := map[string][]string{}
		for _, ch := range chainRows {
			if ch == nil || strings.TrimSpace(ch.ChainKey) == "" {
				continue
			}
			var keysArr []string
			_ = json.Unmarshal(ch.ConceptKeys, &keysArr)
			keysArr = dedupeStrings(keysArr)
			set := map[string]bool{}
			for _, k := range keysArr {
				k = strings.TrimSpace(strings.ToLower(k))
				if k != "" {
					set[k] = true
				}
			}
			ck := strings.TrimSpace(ch.ChainKey)
			chains = append(chains, chainSig{ChainKey: ck, KeySet: set, Size: len(set)})
			chainConceptKeys[ck] = keysArr
		}
		sort.Slice(chains, func(i, j int) bool { return chains[i].ChainKey < chains[j].ChainKey })

		// Concepts for key->id mapping (for mastery lookup).
		concepts, err := deps.Concepts.GetByScope(dbc, "path", &pathID)
		if err != nil {
			return err
		}
		conceptIDByKey := map[string]uuid.UUID{}
		conceptKeyByID := map[uuid.UUID]string{}
		for _, c := range concepts {
			if c == nil || c.ID == uuid.Nil {
				continue
			}
			k := strings.TrimSpace(strings.ToLower(c.Key))
			// Mastery is tracked at the canonical (global) concept level to transfer knowledge across paths.
			id := c.ID
			if c.CanonicalConceptID != nil && *c.CanonicalConceptID != uuid.Nil {
				id = *c.CanonicalConceptID
			}
			if k != "" && id != uuid.Nil {
				conceptIDByKey[k] = id
			}
			conceptKeyByID[c.ID] = k
		}

		// Activities for this path, and their concept keys (for mapping to chain_key).
		activities, err := deps.Act.ListByOwner(dbc, "path", &pathID)
		if err != nil {
			return err
		}
		if len(activities) == 0 {
			return nil
		}
		activityIDs := make([]uuid.UUID, 0, len(activities))
		for _, a := range activities {
			if a == nil || a.ID == uuid.Nil {
				continue
			}
			activityIDs = append(activityIDs, a.ID)
		}

		acRows, err := deps.ActCon.GetByActivityIDs(dbc, activityIDs)
		if err != nil {
			return err
		}
		actToConceptKeys := map[uuid.UUID][]string{}
		for _, ac := range acRows {
			if ac == nil || ac.ActivityID == uuid.Nil || ac.ConceptID == uuid.Nil {
				continue
			}
			k := strings.TrimSpace(strings.ToLower(conceptKeyByID[ac.ConceptID]))
			if k != "" {
				actToConceptKeys[ac.ActivityID] = append(actToConceptKeys[ac.ActivityID], k)
			}
		}

		activityToChainKey := map[uuid.UUID]string{}
		for _, aid := range activityIDs {
			cks := dedupeStrings(actToConceptKeys[aid])
			ck := bestChainKey(chains, cks)
			if ck == "" && len(cks) > 0 {
				ck = keys.ChainKey(cks, nil)
			}
			activityToChainKey[aid] = ck
		}

		// Progression evidence (compact facts).
		events, err := deps.Progress.ListByUserAndPathID(dbc, in.OwnerUserID, pathID, maxEvents)
		if err != nil {
			return err
		}

		// Pre-aggregate by chain key.
		type progAgg struct {
			Completions int
			ScoreSum    float64
			ScoreN      int
			TotalDwell  int
			Attempts    int
		}
		progByChain := map[string]*progAgg{}
		for _, ev := range events {
			if ev == nil || ev.ActivityID == nil || *ev.ActivityID == uuid.Nil {
				continue
			}
			chainKey := strings.TrimSpace(activityToChainKey[*ev.ActivityID])
			if chainKey == "" {
				continue
			}
			if progByChain[chainKey] == nil {
				progByChain[chainKey] = &progAgg{}
			}
			pa := progByChain[chainKey]
			if ev.Completed {
				pa.Completions++
				pa.ScoreSum += ev.Score
				pa.ScoreN++
				pa.TotalDwell += ev.DwellMS
				pa.Attempts += ev.Attempts
			}
		}

		now := time.Now().UTC()

		for _, ch := range chains {
			out.ChainsEvaluated++

			chainKey := strings.TrimSpace(ch.ChainKey)
			if chainKey == "" {
				continue
			}

			pa := progByChain[chainKey]
			if pa == nil {
				pa = &progAgg{}
			}
			avgScore := 0.0
			if pa.ScoreN > 0 {
				avgScore = pa.ScoreSum / float64(pa.ScoreN)
			}

			// Mastery evidence (optional; use if available).
			avgMastery := 0.0
			avgConf := 0.0
			masteryN := 0
			conceptIDs := make([]uuid.UUID, 0, len(chainConceptKeys[chainKey]))
			for _, k := range chainConceptKeys[chainKey] {
				id := conceptIDByKey[strings.TrimSpace(strings.ToLower(k))]
				if id != uuid.Nil {
					conceptIDs = append(conceptIDs, id)
				}
			}
			conceptIDs = dedupeUUIDs(conceptIDs)

			if deps.Mastery != nil && len(conceptIDs) > 0 {
				states, err := deps.Mastery.ListByUserAndConceptIDs(dbc, in.OwnerUserID, conceptIDs)
				if err != nil {
					return err
				}
				for _, s := range states {
					if s == nil || s.UserID == uuid.Nil || s.ConceptID == uuid.Nil {
						continue
					}
					avgMastery += s.Mastery
					avgConf += s.Confidence
					masteryN++
				}
				if masteryN > 0 {
					avgMastery /= float64(masteryN)
					avgConf /= float64(masteryN)
				}
			}

			// Compute confidence as max(mastery, progression) with conservative gating.
			completionsFactor := 1 - math.Exp(-float64(pa.Completions)/3.0)
			progressionConfidence := clamp01(avgScore) * completionsFactor

			coverage := 0.0
			if len(conceptIDs) > 0 {
				coverage = float64(masteryN) / float64(len(conceptIDs))
			}
			coverageFactor := 0.0
			if coverage > 0 {
				coverageFactor = math.Min(1, coverage/0.7)
			}
			masteryConfidence := clamp01(avgMastery) * clamp01(avgConf) * coverageFactor

			completionConfidence := math.Max(masteryConfidence, progressionConfidence)

			// Preserve monotonicity.
			existing, err := deps.Completed.Get(dbc, in.OwnerUserID, chainKey)
			if err != nil {
				return err
			}

			finalConfidence := completionConfidence
			completedAt := (*time.Time)(nil)
			if completionConfidence >= threshold {
				t := now
				completedAt = &t
			}

			totalDwell := pa.TotalDwell
			attempts := pa.Attempts
			masteryAt := avgMastery

			if existing != nil {
				if existing.CompletedAt != nil {
					completedAt = existing.CompletedAt
				}
				if existing.CompletionConfidence > finalConfidence {
					finalConfidence = existing.CompletionConfidence
				}
				if existing.MasteryAt > masteryAt {
					masteryAt = existing.MasteryAt
				}
				if existing.AvgScore > avgScore {
					avgScore = existing.AvgScore
				}
				if existing.TotalDwellMS > totalDwell {
					totalDwell = existing.TotalDwellMS
				}
				if existing.Attempts > attempts {
					attempts = existing.Attempts
				}
			}

			row := &types.UserCompletedUnit{
				ID:                   uuid.New(),
				UserID:               in.OwnerUserID,
				ChainKey:             chainKey,
				CompletedAt:          completedAt,
				CompletionConfidence: clamp01(finalConfidence),
				MasteryAt:            clamp01(masteryAt),
				AvgScore:             clamp01(avgScore),
				TotalDwellMS:         totalDwell,
				Attempts:             attempts,
				Metadata: mustJSON(map[string]any{
					"completions":            pa.Completions,
					"avg_score":              avgScore,
					"progression_confidence": progressionConfidence,
					"mastery_confidence":     masteryConfidence,
					"mastery_coverage":       coverage,
					"mastery_n":              masteryN,
					"concept_count":          len(conceptIDs),
					"threshold":              threshold,
					"updated_at":             now.Format(time.RFC3339Nano),
				}),
				UpdatedAt: now,
			}

			if err := deps.Completed.Upsert(dbc, row); err != nil {
				return err
			}
			out.UnitsUpserted++
			if existing == nil || existing.CompletedAt == nil {
				if row.CompletedAt != nil {
					out.UnitsCompleted++
				}
			}
		}

		return nil
	})
	if err != nil {
		return out, err
	}

	if deps.Graph != nil && deps.Mastery != nil {
		if err := syncUserConceptStatesToNeo4j(ctx, deps, in.OwnerUserID, pathID); err != nil && deps.Log != nil {
			deps.Log.Warn("neo4j user learning graph sync failed (continuing)", "error", err, "user_id", in.OwnerUserID.String())
		}
	}

	out.Noop = out.UnitsUpserted == 0
	return out, nil
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

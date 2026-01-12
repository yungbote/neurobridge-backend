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
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type PriorsRefreshDeps struct {
	DB           *gorm.DB
	Log          *logger.Logger
	Activities   repos.ActivityRepo
	Variants     repos.ActivityVariantRepo
	VariantStats repos.ActivityVariantStatRepo
	Chains       repos.ChainSignatureRepo
	Concepts     repos.ConceptRepo
	ActConcepts  repos.ActivityConceptRepo
	ChainPriors  repos.ChainPriorRepo
	CohortPriors repos.CohortPriorRepo
	Bootstrap    services.LearningBuildBootstrapService
}

type PriorsRefreshInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
}

type PriorsRefreshOutput struct {
	Noop                 bool `json:"noop"`
	ChainPriorsUpserted  int  `json:"chain_priors_upserted"`
	CohortPriorsUpserted int  `json:"cohort_priors_upserted"`
	VariantsConsidered   int  `json:"variants_considered"`
}

func PriorsRefresh(ctx context.Context, deps PriorsRefreshDeps, in PriorsRefreshInput) (PriorsRefreshOutput, error) {
	out := PriorsRefreshOutput{Noop: true}
	if deps.DB == nil || deps.Log == nil || deps.Bootstrap == nil ||
		deps.Activities == nil || deps.Variants == nil || deps.VariantStats == nil ||
		deps.Chains == nil || deps.Concepts == nil || deps.ActConcepts == nil ||
		deps.ChainPriors == nil || deps.CohortPriors == nil {
		return out, fmt.Errorf("priors_refresh: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("priors_refresh: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("priors_refresh: missing material_set_id")
	}

	type chainSig struct {
		ChainKey string
		KeySet   map[string]bool
		Size     int
	}

	type agg struct {
		Starts      int
		Completions int
		ThumbsUp    int
		ThumbsDown  int

		ScoreSum    float64
		ScoreWeight float64

		DwellSum    float64
		DwellWeight float64

		LastObservedAt *time.Time
	}

	addStat := func(a *agg, st *types.ActivityVariantStat) {
		if a == nil || st == nil {
			return
		}
		a.Starts += st.Starts
		a.Completions += st.Completions
		a.ThumbsUp += st.ThumbsUp
		a.ThumbsDown += st.ThumbsDown

		// Weight averages by completions, then starts (fallback).
		w := float64(st.Completions)
		if w <= 0 {
			w = float64(st.Starts)
		}
		if w <= 0 {
			w = 1
		}

		a.ScoreSum += st.AvgScore * w
		a.ScoreWeight += w

		a.DwellSum += float64(st.AvgDwellMS) * w
		a.DwellWeight += w

		if st.LastObservedAt != nil {
			if a.LastObservedAt == nil || st.LastObservedAt.After(*a.LastObservedAt) {
				t := *st.LastObservedAt
				a.LastObservedAt = &t
			}
		}
	}

	modalityFromVariant := func(v *types.ActivityVariant) string {
		if v == nil {
			return "text"
		}
		candidates := []any{}
		if len(v.RenderSpec) > 0 && string(v.RenderSpec) != "null" {
			var obj map[string]any
			if json.Unmarshal(v.RenderSpec, &obj) == nil {
				candidates = append(candidates, obj)
			}
		}
		if len(v.ContentJSON) > 0 && string(v.ContentJSON) != "null" {
			var obj map[string]any
			if json.Unmarshal(v.ContentJSON, &obj) == nil {
				candidates = append(candidates, obj)
			}
		}
		for _, c := range candidates {
			obj, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if s := strings.TrimSpace(fmt.Sprint(obj["modality"])); s != "" {
				return s
			}
			if rep, ok := obj["representation"].(map[string]any); ok {
				if s := strings.TrimSpace(fmt.Sprint(rep["modality"])); s != "" {
					return s
				}
			}
		}
		return "text"
	}

	bestChainKey := func(chains []chainSig, activityConceptKeys []string) string {
		if len(chains) == 0 || len(activityConceptKeys) == 0 {
			return ""
		}
		ckeys := dedupeStrings(activityConceptKeys)
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

	err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		pathID, err := deps.Bootstrap.EnsurePath(dbc, in.OwnerUserID, in.MaterialSetID)
		if err != nil {
			return err
		}

		activities, err := deps.Activities.ListByOwner(dbc, "path", &pathID)
		if err != nil {
			return err
		}
		if len(activities) == 0 {
			return nil
		}

		activityByID := map[uuid.UUID]*types.Activity{}
		activityIDs := make([]uuid.UUID, 0, len(activities))
		for _, a := range activities {
			if a == nil || a.ID == uuid.Nil {
				continue
			}
			activityByID[a.ID] = a
			activityIDs = append(activityIDs, a.ID)
		}
		if len(activityIDs) == 0 {
			return nil
		}

		variants, err := deps.Variants.GetByActivityIDs(dbc, activityIDs)
		if err != nil {
			return err
		}
		if len(variants) == 0 {
			return nil
		}

		variantIDs := make([]uuid.UUID, 0, len(variants))
		for _, v := range variants {
			if v == nil || v.ID == uuid.Nil {
				continue
			}
			variantIDs = append(variantIDs, v.ID)
		}
		stats, err := deps.VariantStats.GetByVariantIDs(dbc, variantIDs)
		if err != nil {
			return err
		}
		statByVariantID := map[uuid.UUID]*types.ActivityVariantStat{}
		for _, st := range stats {
			if st == nil || st.ActivityVariantID == uuid.Nil {
				continue
			}
			statByVariantID[st.ActivityVariantID] = st
		}

		// Activity -> concept keys
		acRows, err := deps.ActConcepts.GetByActivityIDs(dbc, activityIDs)
		if err != nil {
			return err
		}
		actToConceptIDs := map[uuid.UUID][]uuid.UUID{}
		allConceptIDs := make([]uuid.UUID, 0, len(acRows))
		for _, ac := range acRows {
			if ac == nil || ac.ActivityID == uuid.Nil || ac.ConceptID == uuid.Nil {
				continue
			}
			actToConceptIDs[ac.ActivityID] = append(actToConceptIDs[ac.ActivityID], ac.ConceptID)
			allConceptIDs = append(allConceptIDs, ac.ConceptID)
		}
		allConceptIDs = dedupeUUIDs(allConceptIDs)

		conceptKeyByID := map[uuid.UUID]string{}
		if len(allConceptIDs) > 0 {
			concepts, err := deps.Concepts.GetByIDs(dbc, allConceptIDs)
			if err != nil {
				return err
			}
			for _, c := range concepts {
				if c == nil || c.ID == uuid.Nil {
					continue
				}
				conceptKeyByID[c.ID] = strings.TrimSpace(c.Key)
			}
		}

		// Load chain signatures (if missing, we can still compute chain_key set-based as a fallback).
		chainRows, err := deps.Chains.ListByScope(dbc, "path", &pathID)
		if err != nil {
			return err
		}
		chains := make([]chainSig, 0, len(chainRows))
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
			chains = append(chains, chainSig{ChainKey: strings.TrimSpace(ch.ChainKey), KeySet: set, Size: len(set)})
		}
		sort.Slice(chains, func(i, j int) bool { return chains[i].ChainKey < chains[j].ChainKey })

		activityToChainKey := map[uuid.UUID]string{}
		for _, aid := range activityIDs {
			cids := actToConceptIDs[aid]
			cks := make([]string, 0, len(cids))
			for _, cid := range dedupeUUIDs(cids) {
				k := strings.TrimSpace(strings.ToLower(conceptKeyByID[cid]))
				if k != "" {
					cks = append(cks, k)
				}
			}
			cks = dedupeStrings(cks)

			ck := bestChainKey(chains, cks)
			if ck == "" && len(cks) > 0 {
				ck = keys.ChainKey(cks, nil)
			}
			activityToChainKey[aid] = ck
		}

		type chainKey struct {
			ChainKey          string
			ActivityKind      string
			Modality          string
			Variant           string
			RepresentationKey string
		}
		type cohortKey struct {
			ActivityKind string
			Modality     string
			Variant      string
		}

		chainAgg := map[chainKey]*agg{}
		cohortAgg := map[cohortKey]*agg{}

		for _, v := range variants {
			if v == nil || v.ID == uuid.Nil || v.ActivityID == uuid.Nil {
				continue
			}
			act := activityByID[v.ActivityID]
			if act == nil {
				continue
			}
			out.VariantsConsidered++

			kind := strings.TrimSpace(act.Kind)
			if kind == "" {
				kind = "reading"
			}
			varName := strings.TrimSpace(v.Variant)
			if varName == "" {
				varName = "default"
			}
			modality := strings.TrimSpace(modalityFromVariant(v))
			if modality == "" {
				modality = "text"
			}
			reprKey := keys.RepresentationKey(map[string]any{
				"activity_kind": kind,
				"modality":      modality,
				"variant":       varName,
			})

			st := statByVariantID[v.ID]

			// Global cohort priors (no concept_id / cluster_id in v1).
			ck := cohortKey{ActivityKind: kind, Modality: modality, Variant: varName}
			if cohortAgg[ck] == nil {
				cohortAgg[ck] = &agg{}
			}
			addStat(cohortAgg[ck], st)

			// Chain priors only if we can resolve a chain key.
			chainK := strings.TrimSpace(activityToChainKey[v.ActivityID])
			if chainK == "" {
				continue
			}
			k := chainKey{
				ChainKey:          chainK,
				ActivityKind:      kind,
				Modality:          modality,
				Variant:           varName,
				RepresentationKey: reprKey,
			}
			if chainAgg[k] == nil {
				chainAgg[k] = &agg{}
			}
			addStat(chainAgg[k], st)
		}

		now := time.Now().UTC()

		for k, a := range cohortAgg {
			if a == nil {
				continue
			}
			if a.ScoreWeight <= 0 && a.DwellWeight <= 0 && a.Starts == 0 && a.Completions == 0 && a.ThumbsUp == 0 && a.ThumbsDown == 0 {
				continue
			}
			avgScore := 0.0
			if a.ScoreWeight > 0 {
				avgScore = a.ScoreSum / a.ScoreWeight
			}
			avgDwell := 0.0
			if a.DwellWeight > 0 {
				avgDwell = a.DwellSum / a.DwellWeight
			}
			completionRate := 0.0
			if a.Starts > 0 {
				completionRate = float64(a.Completions) / float64(a.Starts)
			} else if a.Completions > 0 {
				completionRate = 1
			}

			row := &types.CohortPrior{
				ID:               uuid.New(),
				ConceptID:        nil,
				ConceptClusterID: nil,
				ActivityKind:     k.ActivityKind,
				Modality:         k.Modality,
				Variant:          k.Variant,
				EMA:              avgScore,
				N:                a.Completions,
				A:                float64(a.ThumbsUp + 1),
				B:                float64(a.ThumbsDown + 1),
				CompletionRate:   completionRate,
				MedianDwellMS:    int(math.Round(avgDwell)),
				LastObservedAt:   a.LastObservedAt,
				UpdatedAt:        now,
			}
			if err := deps.CohortPriors.Upsert(dbc, row); err != nil {
				return err
			}
			out.CohortPriorsUpserted++
		}

		for k, a := range chainAgg {
			if a == nil {
				continue
			}
			if strings.TrimSpace(k.ChainKey) == "" {
				continue
			}
			if a.ScoreWeight <= 0 && a.DwellWeight <= 0 && a.Starts == 0 && a.Completions == 0 && a.ThumbsUp == 0 && a.ThumbsDown == 0 {
				continue
			}
			avgScore := 0.0
			if a.ScoreWeight > 0 {
				avgScore = a.ScoreSum / a.ScoreWeight
			}
			avgDwell := 0.0
			if a.DwellWeight > 0 {
				avgDwell = a.DwellSum / a.DwellWeight
			}
			completionRate := 0.0
			if a.Starts > 0 {
				completionRate = float64(a.Completions) / float64(a.Starts)
			} else if a.Completions > 0 {
				completionRate = 1
			}

			row := &types.ChainPrior{
				ID:                uuid.New(),
				ChainKey:          k.ChainKey,
				CohortKey:         "",
				ActivityKind:      k.ActivityKind,
				Modality:          k.Modality,
				Variant:           k.Variant,
				RepresentationKey: k.RepresentationKey,
				EMA:               avgScore,
				N:                 a.Completions,
				A:                 float64(a.ThumbsUp + 1),
				B:                 float64(a.ThumbsDown + 1),
				CompletionRate:    completionRate,
				MedianDwellMS:     int(math.Round(avgDwell)),
				LastObservedAt:    a.LastObservedAt,
				UpdatedAt:         now,
			}
			if err := deps.ChainPriors.Upsert(dbc, row); err != nil {
				return err
			}
			out.ChainPriorsUpserted++
		}

		return nil
	})
	if err != nil {
		return out, err
	}

	out.Noop = out.ChainPriorsUpserted == 0 && out.CohortPriorsUpserted == 0
	return out, nil
}

func dedupeUUIDs(in []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(in))
	for _, id := range in {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

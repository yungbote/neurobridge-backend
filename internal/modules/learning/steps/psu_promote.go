package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
)

type PSUPromotionDeps struct {
	DB           *gorm.DB
	Log          *logger.Logger
	Events       repos.UserEventRepo
	PSUs         repos.PathStructuralUnitRepo
	Concepts     repos.ConceptRepo
	Edges        repos.ConceptEdgeRepo
	ConceptState repos.UserConceptStateRepo
	ConceptModel repos.UserConceptModelRepo
	MisconRepo   repos.UserMisconceptionInstanceRepo
	AI           openai.Client
}

type PSUPromotionInput struct {
	UserID uuid.UUID `json:"user_id"`
}

type PSUPromotionOutput struct {
	UserID     uuid.UUID `json:"user_id"`
	Candidates int       `json:"candidates"`
	Promoted   int       `json:"promoted"`
	Demoted    int       `json:"demoted"`
	Considered int       `json:"considered"`
}

type psuSignatureGroup struct {
	Signature  string
	ConceptIDs []uuid.UUID
	PsuIDs     []uuid.UUID
	PathIDs    map[uuid.UUID]bool
}

type compoundLabel struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

func PSUPromotion(ctx context.Context, deps PSUPromotionDeps, in PSUPromotionInput) (PSUPromotionOutput, error) {
	out := PSUPromotionOutput{UserID: in.UserID}
	if deps.DB == nil || deps.Events == nil || deps.PSUs == nil || deps.Concepts == nil || deps.ConceptState == nil || deps.ConceptModel == nil || deps.MisconRepo == nil {
		return out, fmt.Errorf("psu_promote: missing deps")
	}
	if in.UserID == uuid.Nil {
		return out, fmt.Errorf("psu_promote: missing user_id")
	}
	if !envBool("PSU_PROMOTION_ENABLED", false) {
		return out, nil
	}

	recencyDays := envIntAllowZero("PSU_PROMOTION_RECENCY_DAYS", 180)
	if recencyDays < 30 {
		recencyDays = 30
	}
	minPaths := envIntAllowZero("PSU_PROMOTION_MIN_PATHS", 2)
	if minPaths < 1 {
		minPaths = 1
	}
	minConcepts := envIntAllowZero("PSU_PROMOTION_MIN_CONCEPTS", 2)
	if minConcepts < 2 {
		minConcepts = 2
	}
	promoteMastery := envFloatAllowZero("PSU_PROMOTION_PROMOTE_MASTERY", 0.80)
	promoteConfidence := envFloatAllowZero("PSU_PROMOTION_PROMOTE_CONFIDENCE", 0.60)
	demoteMastery := envFloatAllowZero("PSU_PROMOTION_DEMOTE_MASTERY", 0.65)
	demoteConfidence := envFloatAllowZero("PSU_PROMOTION_DEMOTE_CONFIDENCE", 0.50)
	maxMembers := envIntAllowZero("PSU_PROMOTION_MAX_MEMBERS", 8)
	if maxMembers < 2 {
		maxMembers = 2
	}
	maxCandidates := envIntAllowZero("PSU_PROMOTION_MAX_CANDIDATES", 40)
	if maxCandidates < 5 {
		maxCandidates = 5
	}

	templateSignatures := parseSignatureSet(strings.TrimSpace(os.Getenv("PSU_PROMOTION_TEMPLATE_SIGNATURES")))

	since := time.Now().UTC().Add(-time.Duration(recencyDays) * 24 * time.Hour)
	dbc := dbctx.Context{Ctx: ctx}
	pathIDs, err := deps.Events.ListDistinctPathIDsByUser(dbc, in.UserID, &since, 1000)
	if err != nil {
		return out, err
	}
	if len(pathIDs) == 0 {
		return out, nil
	}

	psus, err := deps.PSUs.ListByPathIDs(dbc, pathIDs)
	if err != nil {
		return out, err
	}
	if len(psus) == 0 {
		return out, nil
	}

	groups := map[string]*psuSignatureGroup{}
	for _, psu := range psus {
		if psu == nil || psu.PathID == uuid.Nil {
			continue
		}
		ids := conceptIDsFromPSU(psu)
		ids = dedupeUUIDs(ids)
		if len(ids) < minConcepts {
			continue
		}
		sig := signatureForConceptIDs(ids)
		if sig == "" {
			continue
		}
		group := groups[sig]
		if group == nil {
			group = &psuSignatureGroup{
				Signature:  sig,
				ConceptIDs: ids,
				PathIDs:    map[uuid.UUID]bool{},
			}
			groups[sig] = group
		}
		group.PsuIDs = append(group.PsuIDs, psu.ID)
		group.PathIDs[psu.PathID] = true
	}

	if len(groups) == 0 {
		return out, nil
	}

	candidates := make([]*psuSignatureGroup, 0, len(groups))
	for _, g := range groups {
		candidates = append(candidates, g)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return len(candidates[i].PathIDs) > len(candidates[j].PathIDs)
	})
	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}

	out.Candidates = len(candidates)
	for _, g := range candidates {
		pathCount := len(g.PathIDs)
		templateAllowed := templateSignatures[g.Signature]
		if pathCount < minPaths && !templateAllowed {
			continue
		}
		out.Considered++

		stateByConcept := map[uuid.UUID]*types.UserConceptState{}
		if rows, err := deps.ConceptState.ListByUserAndConceptIDs(dbc, in.UserID, g.ConceptIDs); err == nil {
			for _, st := range rows {
				if st != nil && st.ConceptID != uuid.Nil {
					stateByConcept[st.ConceptID] = st
				}
			}
		}
		misconByConcept := map[uuid.UUID][]*types.UserMisconceptionInstance{}
		if rows, err := deps.MisconRepo.ListActiveByUserAndConceptIDs(dbc, in.UserID, g.ConceptIDs); err == nil {
			for _, r := range rows {
				if r != nil && r.CanonicalConceptID != uuid.Nil {
					misconByConcept[r.CanonicalConceptID] = append(misconByConcept[r.CanonicalConceptID], r)
				}
			}
		}

		minMastery := 1.0
		minConf := 1.0
		hasMiscon := false
		for _, cid := range g.ConceptIDs {
			if cid == uuid.Nil {
				continue
			}
			st := stateByConcept[cid]
			if st == nil {
				minMastery = 0
				minConf = 0
				continue
			}
			if st.Mastery < minMastery {
				minMastery = st.Mastery
			}
			if st.Confidence < minConf {
				minConf = st.Confidence
			}
			if len(misconByConcept[cid]) > 0 {
				hasMiscon = true
			}
		}

		compoundKey := "compound_" + g.Signature
		existing, _ := deps.Concepts.GetByScopeAndKeys(dbc, "global", nil, []string{compoundKey})
		var compound *types.Concept
		if len(existing) > 0 && existing[0] != nil {
			compound = existing[0]
		}

		active := compound != nil
		var compoundState *types.UserConceptState
		if compound != nil {
			if st, err := deps.ConceptState.Get(dbc, in.UserID, compound.ID); err == nil && st != nil && st.ConceptID != uuid.Nil {
				compoundState = st
				active = st.Mastery > 0 || st.Confidence > 0
			}
		}

		action := promotionDecision(active, minMastery, minConf, hasMiscon, promoteMastery, promoteConfidence, demoteMastery, demoteConfidence)
		if action == "skip" {
			continue
		}

		if action == "promote" && compound == nil {
			label := compoundLabelFromConcepts(ctx, deps, g.ConceptIDs, maxMembers)
			now := time.Now().UTC()
			row := &types.Concept{
				ID:        uuid.New(),
				Scope:     "global",
				ScopeID:   nil,
				Depth:     0,
				SortIndex: 0,
				Key:       compoundKey,
				Name:      label.Name,
				Summary:   label.Summary,
				KeyPoints: datatypes.JSON([]byte(`[]`)),
				VectorID:  "",
				Metadata:  datatypes.JSON(mustJSON(compoundMetadata(g, label))),
				CreatedAt: now,
				UpdatedAt: now,
			}
			if row.VectorID == "" {
				row.VectorID = "concept:" + row.ID.String()
			}
			if err := deps.Concepts.UpsertByScopeAndKey(dbc, row); err == nil {
				compound = row
			}
		}
		if compound == nil {
			continue
		}

		if action == "promote" {
			if compoundState == nil {
				compoundState = &types.UserConceptState{
					ID:         uuid.New(),
					UserID:     in.UserID,
					ConceptID:  compound.ID,
					Mastery:    0,
					Confidence: 0,
					DecayRate:  0.015,
				}
			}
			if minMastery > compoundState.Mastery {
				compoundState.Mastery = clamp01(minMastery)
			}
			if minConf > compoundState.Confidence {
				compoundState.Confidence = clamp01(minConf)
			}
			compoundState.LastSeenAt = ptrTime(time.Now().UTC())
			_ = deps.ConceptState.Upsert(dbc, compoundState)

			updatePromotionSupport(dbc, deps, compound.ID, in.UserID, g, minConf)
			upsertCompoundEdges(dbc, deps.Edges, compound.ID, g, minConf)
			out.Promoted++
		} else if action == "demote" {
			if compoundState != nil {
				if compoundState.Mastery > demoteMastery {
					compoundState.Mastery = demoteMastery
				}
				if compoundState.Confidence > demoteConfidence {
					compoundState.Confidence = demoteConfidence
				}
				compoundState.LastSeenAt = ptrTime(time.Now().UTC())
				_ = deps.ConceptState.Upsert(dbc, compoundState)
			}
			updatePromotionSupport(dbc, deps, compound.ID, in.UserID, g, minConf)
			upsertCompoundEdges(dbc, deps.Edges, compound.ID, g, minConf)
			out.Demoted++
		} else {
			upsertCompoundEdges(dbc, deps.Edges, compound.ID, g, minConf)
			out.Considered++
		}
	}

	if deps.Log != nil {
		deps.Log.Info("psu_promote: completed", "user_id", in.UserID.String(), "candidates", out.Candidates, "considered", out.Considered, "promoted", out.Promoted, "demoted", out.Demoted)
	}
	return out, nil
}

func conceptIDsFromPSU(psu *types.PathStructuralUnit) []uuid.UUID {
	if psu == nil || len(psu.DerivedCanonicalConceptIDs) == 0 || string(psu.DerivedCanonicalConceptIDs) == "null" {
		return nil
	}
	var raw []string
	if err := json.Unmarshal(psu.DerivedCanonicalConceptIDs, &raw); err != nil {
		return nil
	}
	return uuidSliceFromStrings(raw)
}

func signatureForConceptIDs(ids []uuid.UUID) string {
	if len(ids) == 0 {
		return ""
	}
	clean := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != uuid.Nil {
			clean = append(clean, id.String())
		}
	}
	if len(clean) == 0 {
		return ""
	}
	sort.Strings(clean)
	return hashString(strings.Join(clean, "|"))
}

func parseSignatureSet(raw string) map[string]bool {
	out := map[string]bool{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	for _, part := range strings.Split(raw, ",") {
		key := strings.TrimSpace(part)
		if key != "" {
			out[key] = true
		}
	}
	return out
}

func promotionDecision(active bool, minMastery float64, minConf float64, hasMiscon bool, promoteMastery float64, promoteConf float64, demoteMastery float64, demoteConf float64) string {
	if !active {
		if hasMiscon {
			return "skip"
		}
		if minMastery >= promoteMastery && minConf >= promoteConf {
			return "promote"
		}
		return "skip"
	}
	if hasMiscon {
		return "demote"
	}
	if minMastery < demoteMastery || minConf < demoteConf {
		return "demote"
	}
	return "keep"
}

func compoundLabelFromConcepts(ctx context.Context, deps PSUPromotionDeps, conceptIDs []uuid.UUID, maxMembers int) compoundLabel {
	keys := []string{}
	if deps.Concepts != nil && len(conceptIDs) > 0 {
		if rows, err := deps.Concepts.GetByIDs(dbctx.Context{Ctx: ctx}, conceptIDs); err == nil {
			for _, c := range rows {
				if c == nil {
					continue
				}
				if strings.TrimSpace(c.Name) != "" {
					keys = append(keys, c.Name)
				} else if strings.TrimSpace(c.Key) != "" {
					keys = append(keys, c.Key)
				}
			}
		}
	}
	keys = dedupeStrings(keys)
	sort.Strings(keys)
	if maxMembers > 0 && len(keys) > maxMembers {
		keys = keys[:maxMembers]
	}
	if envBool("PSU_PROMOTION_USE_LLM", false) && deps.AI != nil {
		if label, ok := proposeCompoundLabel(ctx, deps.AI, keys); ok {
			return label
		}
	}
	name := "Compound concept"
	if len(keys) > 0 {
		name = strings.Join(keys, " + ")
	}
	return compoundLabel{
		Name:    name,
		Summary: "Compound concept derived from: " + strings.Join(keys, ", "),
	}
}

func proposeCompoundLabel(ctx context.Context, ai openai.Client, keys []string) (compoundLabel, bool) {
	if ai == nil || len(keys) == 0 {
		return compoundLabel{}, false
	}
	system := "You name an umbrella concept that unifies the provided member concepts. Return JSON only."
	user := "MEMBER_CONCEPTS:\n" + strings.Join(keys, ", ")
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":    map[string]any{"type": "string"},
			"summary": map[string]any{"type": "string"},
		},
		"required":             []string{"name", "summary"},
		"additionalProperties": false,
	}
	obj, err := ai.GenerateJSON(ctx, system, user, "psu_promotion_label", schema)
	if err != nil {
		return compoundLabel{}, false
	}
	name := strings.TrimSpace(stringFromAny(obj["name"]))
	summary := strings.TrimSpace(stringFromAny(obj["summary"]))
	if name == "" {
		return compoundLabel{}, false
	}
	return compoundLabel{Name: name, Summary: summary}, true
}

func compoundMetadata(group *psuSignatureGroup, label compoundLabel) map[string]any {
	paths := []string{}
	for pid := range group.PathIDs {
		paths = append(paths, pid.String())
	}
	sort.Strings(paths)
	ids := []string{}
	for _, id := range group.ConceptIDs {
		if id != uuid.Nil {
			ids = append(ids, id.String())
		}
	}
	sort.Strings(ids)
	return map[string]any{
		"kind":             "compound",
		"signature":        group.Signature,
		"source":           "psu_promotion",
		"member_concepts":  ids,
		"supporting_paths": paths,
		"label":            label.Name,
	}
}

func upsertCompoundEdges(dbc dbctx.Context, repo repos.ConceptEdgeRepo, compoundID uuid.UUID, group *psuSignatureGroup, strength float64) {
	if repo == nil || compoundID == uuid.Nil || group == nil {
		return
	}
	strength = clamp01(strength)
	paths := []string{}
	for pid := range group.PathIDs {
		if pid != uuid.Nil {
			paths = append(paths, pid.String())
		}
	}
	sort.Strings(paths)
	psuIDs := []string{}
	for _, id := range group.PsuIDs {
		if id != uuid.Nil {
			psuIDs = append(psuIDs, id.String())
		}
	}
	sort.Strings(psuIDs)
	for _, memberID := range group.ConceptIDs {
		if memberID == uuid.Nil || memberID == compoundID {
			continue
		}
		ev := map[string]any{
			"source":           "psu_promotion",
			"signature":        group.Signature,
			"member_id":        memberID.String(),
			"supporting_paths": paths,
			"psu_ids":          psuIDs,
		}
		row := &types.ConceptEdge{
			FromConceptID: compoundID,
			ToConceptID:   memberID,
			EdgeType:      "composes",
			Strength:      strength,
			Evidence:      datatypes.JSON(mustJSON(ev)),
		}
		_ = repo.Upsert(dbc, row)
	}
}

func updatePromotionSupport(dbc dbctx.Context, deps PSUPromotionDeps, conceptID uuid.UUID, userID uuid.UUID, group *psuSignatureGroup, confidence float64) {
	if deps.ConceptModel == nil || conceptID == uuid.Nil || userID == uuid.Nil {
		return
	}
	model, _ := deps.ConceptModel.Get(dbc, userID, conceptID)
	if model == nil {
		model = &types.UserConceptModel{
			ID:                 uuid.New(),
			UserID:             userID,
			CanonicalConceptID: conceptID,
			ModelVersion:       1,
		}
	}
	ptr := supportPointer{
		SourceType: "psu_promotion",
		SourceID:   group.Signature,
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
		Confidence: confidence,
	}
	support := loadSupportPointers([]byte(model.Support))
	support, added := addSupportPointer(support, ptr, 20)
	if added {
		model.Support = datatypes.JSON(mustJSON(support))
		t := time.Now().UTC()
		model.LastStructuralAt = &t
		_ = deps.ConceptModel.Upsert(dbc, model)
	}
}

func ptrTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	tt := t.UTC()
	return &tt
}

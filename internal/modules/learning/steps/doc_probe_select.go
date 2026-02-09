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
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
	docgen "github.com/yungbote/neurobridge-backend/internal/modules/learning/docgen"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type DocProbeSelectDeps struct {
	DB           *gorm.DB
	Log          *logger.Logger
	Path         repos.PathRepo
	PathRuns     repos.PathRunRepo
	PathNodes    repos.PathNodeRepo
	NodeDocs     repos.LearningNodeDocRepo
	DocVariants  repos.LearningNodeDocVariantRepo
	Concepts     repos.ConceptRepo
	ConceptState repos.UserConceptStateRepo
	MisconRepo   repos.UserMisconceptionInstanceRepo
	Testlets     repos.UserTestletStateRepo
	DocProbes    repos.DocProbeRepo
	Bootstrap    services.LearningBuildBootstrapService
}

type DocProbeSelectInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	PathID        uuid.UUID
	AnchorNodeID  uuid.UUID
	Lookahead     int
	NodeIDs       []uuid.UUID
}

type DocProbeSelectOutput struct {
	PathID           uuid.UUID `json:"path_id"`
	Lookahead        int       `json:"lookahead"`
	NodesConsidered  int       `json:"nodes_considered"`
	DocsConsidered   int       `json:"docs_considered"`
	BlocksConsidered int       `json:"blocks_considered"`
	ProbesSelected   int       `json:"probes_selected"`
	DocsUpdated      int       `json:"docs_updated"`
	RateLimited      bool      `json:"rate_limited"`
}

type docProbeCandidate struct {
	NodeID             uuid.UUID
	BlockID            string
	BlockType          string
	BlockIndex         int
	ConceptKeys        []string
	ConceptIDs         []uuid.UUID
	TargetedPrereq     bool
	TestletID          string
	TestletType        string
	TestletUncertainty float64
	InfoGain           float64
	Score              float64
	ScoreComponents    map[string]float64
	TriggerAfter       []string
}

func DocProbeSelect(ctx context.Context, deps DocProbeSelectDeps, in DocProbeSelectInput) (DocProbeSelectOutput, error) {
	out := DocProbeSelectOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Path == nil || deps.PathNodes == nil || deps.NodeDocs == nil || deps.DocProbes == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("doc_probe_select: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("doc_probe_select: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("doc_probe_select: missing material_set_id")
	}

	pathID, err := resolvePathID(ctx, deps.Bootstrap, in.OwnerUserID, in.MaterialSetID, in.PathID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	pathRow, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, pathID)
	if err != nil {
		return out, err
	}
	pathKind := ""
	if pathRow != nil {
		pathKind = strings.TrimSpace(pathRow.Kind)
	}

	lookahead := in.Lookahead
	if lookahead <= 0 {
		lookahead = docgen.DocLookaheadForPathKind(pathKind)
	}
	out.Lookahead = lookahead

	nodeIDs := dedupeUUIDsProbe(in.NodeIDs)
	nodeMetaByID := map[uuid.UUID]map[string]any{}
	if len(nodeIDs) == 0 {
		if lookahead <= 0 {
			return out, nil
		}
		anchorID := in.AnchorNodeID
		if anchorID == uuid.Nil && deps.PathRuns != nil {
			if pr, err := deps.PathRuns.GetByUserAndPathID(dbctx.Context{Ctx: ctx}, in.OwnerUserID, pathID); err == nil && pr != nil {
				if pr.ActiveNodeID != nil && *pr.ActiveNodeID != uuid.Nil {
					anchorID = *pr.ActiveNodeID
				}
			}
		}

		nodes, err := deps.PathNodes.GetByPathIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{pathID})
		if err != nil {
			return out, err
		}
		if len(nodes) == 0 {
			return out, nil
		}
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].Index < nodes[j].Index })

		anchorIdx := -1
		if anchorID != uuid.Nil {
			for i, n := range nodes {
				if n != nil && n.ID == anchorID {
					anchorIdx = i
					break
				}
			}
		}
		start := anchorIdx + 1
		if start < 0 {
			start = 0
		}
		if start >= len(nodes) {
			return out, nil
		}
		end := start + lookahead
		if end > len(nodes) {
			end = len(nodes)
		}
		for _, n := range nodes[start:end] {
			if n != nil && n.ID != uuid.Nil {
				nodeIDs = append(nodeIDs, n.ID)
				if _, ok := nodeMetaByID[n.ID]; !ok {
					nodeMetaByID[n.ID] = decodeJSONMap(n.Metadata)
				}
			}
		}
	}

	if len(nodeIDs) == 0 {
		return out, nil
	}
	out.NodesConsidered = len(nodeIDs)

	if deps.PathNodes != nil && len(nodeMetaByID) == 0 {
		if rows, err := deps.PathNodes.GetByIDs(dbctx.Context{Ctx: ctx}, nodeIDs); err == nil {
			for _, n := range rows {
				if n == nil || n.ID == uuid.Nil {
					continue
				}
				nodeMetaByID[n.ID] = decodeJSONMap(n.Metadata)
			}
		}
	}

	prereqKeysByNode := map[uuid.UUID][]string{}
	if len(nodeMetaByID) > 0 {
		for nodeID, meta := range nodeMetaByID {
			if len(meta) == 0 {
				continue
			}
			keys := normalizeKeys(dedupeStrings(stringSliceFromAny(meta["prereq_concept_keys"])))
			if len(keys) > 0 {
				prereqKeysByNode[nodeID] = keys
			}
		}
	}

	docs, err := deps.NodeDocs.GetByPathNodeIDs(dbctx.Context{Ctx: ctx}, nodeIDs)
	if err != nil {
		return out, err
	}
	docByNode := map[uuid.UUID]*types.LearningNodeDoc{}
	for _, d := range docs {
		if d != nil && d.PathNodeID != uuid.Nil {
			docByNode[d.PathNodeID] = d
		}
	}

	conceptByKey := map[string]*types.Concept{}
	canonicalIDByKey := map[string]uuid.UUID{}
	if deps.Concepts != nil {
		if rows, err := deps.Concepts.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID); err == nil {
			for _, c := range rows {
				if c == nil || c.ID == uuid.Nil {
					continue
				}
				key := normalizeConceptKeyProbe(c.Key)
				if key == "" {
					continue
				}
				conceptByKey[key] = c
				cid := c.ID
				if c.CanonicalConceptID != nil && *c.CanonicalConceptID != uuid.Nil {
					cid = *c.CanonicalConceptID
				}
				if cid != uuid.Nil {
					canonicalIDByKey[key] = cid
				}
			}
		}
	}

	docContentByNode := map[uuid.UUID]*content.NodeDocV1{}
	candidates := []docProbeCandidate{}
	conceptIDSet := map[uuid.UUID]bool{}
	testletIDSet := map[string]bool{}

	for _, nodeID := range nodeIDs {
		docRow := docByNode[nodeID]
		if docRow == nil || len(docRow.DocJSON) == 0 || string(docRow.DocJSON) == "null" {
			continue
		}
		doc := content.NodeDocV1{}
		if err := json.Unmarshal(docRow.DocJSON, &doc); err != nil {
			continue
		}
		if len(doc.Blocks) == 0 {
			continue
		}
		out.DocsConsidered++
		docContentByNode[nodeID] = &doc

		for i, b := range doc.Blocks {
			if b == nil {
				continue
			}
			typ := strings.ToLower(strings.TrimSpace(stringFromAny(b["type"])))
			if typ != "quick_check" && typ != "flashcard" {
				continue
			}
			id := strings.TrimSpace(stringFromAny(b["id"]))
			if id == "" {
				continue
			}
			out.BlocksConsidered++

			keys := dedupeStrings(stringSliceFromAny(b["concept_keys"]))
			if len(keys) == 0 {
				keys = dedupeStrings(doc.ConceptKeys)
			}
			normKeys := normalizeKeys(keys)
			ids := map[uuid.UUID]bool{}
			for _, k := range normKeys {
				if cid := canonicalIDByKey[k]; cid != uuid.Nil {
					ids[cid] = true
					continue
				}
				if c := conceptByKey[k]; c != nil && c.ID != uuid.Nil {
					ids[c.ID] = true
				}
			}
			conceptIDs := make([]uuid.UUID, 0, len(ids))
			for cid := range ids {
				conceptIDs = append(conceptIDs, cid)
				conceptIDSet[cid] = true
			}
			sort.Slice(conceptIDs, func(i, j int) bool { return conceptIDs[i].String() < conceptIDs[j].String() })

			triggerAfter := stringSliceFromAny(b["trigger_after_block_ids"])
			if len(triggerAfter) == 0 {
				triggerAfter = inferTriggerIDs(doc, i)
			}

			testletType := inferTestletType(typ)
			testletID := inferTestletID(b, typ, normKeys)
			if testletID != "" {
				testletIDSet[testletID] = true
			}

			candidates = append(candidates, docProbeCandidate{
				NodeID:       nodeID,
				BlockID:      id,
				BlockType:    typ,
				BlockIndex:   i,
				ConceptKeys:  normKeys,
				ConceptIDs:   conceptIDs,
				TestletID:    testletID,
				TestletType:  testletType,
				TriggerAfter: triggerAfter,
			})
		}
	}

	if len(candidates) == 0 {
		return out, nil
	}

	if len(prereqKeysByNode) > 0 {
		for _, keys := range prereqKeysByNode {
			for _, k := range keys {
				if cid := canonicalIDByKey[k]; cid != uuid.Nil {
					conceptIDSet[cid] = true
				}
			}
		}
	}

	conceptState := map[uuid.UUID]*types.UserConceptState{}
	if deps.ConceptState != nil && len(conceptIDSet) > 0 {
		ids := make([]uuid.UUID, 0, len(conceptIDSet))
		for id := range conceptIDSet {
			ids = append(ids, id)
		}
		if rows, err := deps.ConceptState.ListByUserAndConceptIDs(dbctx.Context{Ctx: ctx}, in.OwnerUserID, ids); err == nil {
			for _, row := range rows {
				if row != nil && row.ConceptID != uuid.Nil {
					conceptState[row.ConceptID] = row
				}
			}
		}
	}

	misconByConcept := map[uuid.UUID]bool{}
	if deps.MisconRepo != nil && len(conceptIDSet) > 0 {
		ids := make([]uuid.UUID, 0, len(conceptIDSet))
		for id := range conceptIDSet {
			ids = append(ids, id)
		}
		if rows, err := deps.MisconRepo.ListActiveByUserAndConceptIDs(dbctx.Context{Ctx: ctx}, in.OwnerUserID, ids); err == nil {
			for _, row := range rows {
				if row != nil && row.CanonicalConceptID != uuid.Nil {
					misconByConcept[row.CanonicalConceptID] = true
				}
			}
		}
	}

	targetPrereqByNode := map[uuid.UUID]map[string]bool{}
	if len(prereqKeysByNode) > 0 {
		minReady := docgen.DocPrereqReadyMin()
		for nodeID, keys := range prereqKeysByNode {
			for _, k := range keys {
				cid := canonicalIDByKey[k]
				hasMiscon := false
				if cid != uuid.Nil && misconByConcept[cid] {
					hasMiscon = true
				}
				st := conceptState[cid]
				unresolved := false
				if st == nil || cid == uuid.Nil {
					unresolved = true
				} else {
					mastery := clamp01Probe(st.Mastery)
					unc := math.Max(clamp01Probe(st.EpistemicUncertainty), clamp01Probe(st.AleatoricUncertainty))
					if mastery < minReady || unc > 0.6 || hasMiscon {
						unresolved = true
					}
				}
				if unresolved {
					if targetPrereqByNode[nodeID] == nil {
						targetPrereqByNode[nodeID] = map[string]bool{}
					}
					targetPrereqByNode[nodeID][k] = true
				}
			}
		}
	}

	testletStateByID := map[string]*types.UserTestletState{}
	if deps.Testlets != nil && len(testletIDSet) > 0 {
		ids := make([]string, 0, len(testletIDSet))
		for id := range testletIDSet {
			ids = append(ids, id)
		}
		if rows, err := deps.Testlets.ListByUserAndTestletIDs(dbctx.Context{Ctx: ctx}, in.OwnerUserID, ids); err == nil {
			for _, row := range rows {
				if row != nil && row.TestletID != "" {
					testletStateByID[row.TestletID] = row
				}
			}
		}
	}

	existingByNode := map[uuid.UUID]map[string]*types.DocProbe{}
	existingActiveCount := map[uuid.UUID]int{}
	for _, nodeID := range nodeIDs {
		rows, err := deps.DocProbes.ListByUserAndNode(dbctx.Context{Ctx: ctx}, in.OwnerUserID, nodeID)
		if err != nil || len(rows) == 0 {
			continue
		}
		blockMap := map[string]*types.DocProbe{}
		for _, row := range rows {
			if row == nil || row.BlockID == "" {
				continue
			}
			blockMap[row.BlockID] = row
			switch strings.ToLower(strings.TrimSpace(row.Status)) {
			case "planned", "shown":
				existingActiveCount[nodeID]++
			}
		}
		if len(blockMap) > 0 {
			existingByNode[nodeID] = blockMap
		}
	}

	minInfoGain := docgen.DocProbeMinInfoGain()
	testletWeight := docgen.DocProbeTestletWeight()
	misconBoost := docgen.DocProbeMisconceptionBoost()
	prereqBoost := docgen.DocProbePrereqBoost()

	for i := range candidates {
		cand := &candidates[i]
		if targetKeys := targetPrereqByNode[cand.NodeID]; len(targetKeys) > 0 && hasOverlapKeys(cand.ConceptKeys, targetKeys) {
			cand.TargetedPrereq = true
		}
		infoGain := computeInfoGainProbe(cand.ConceptIDs, conceptState)
		cand.InfoGain = infoGain
		if infoGain < minInfoGain && !cand.TargetedPrereq {
			continue
		}
		score := infoGain
		scoreParts := map[string]float64{"info_gain": infoGain}

		if cand.TestletID != "" {
			unc := testletUncertainty(testletStateByID[cand.TestletID])
			cand.TestletUncertainty = unc
			if unc > 0 && testletWeight > 0 {
				boost := unc * testletWeight
				score += boost
				scoreParts["testlet_uncertainty"] = boost
			}
		}

		if misconBoost > 0 && len(cand.ConceptIDs) > 0 {
			for _, cid := range cand.ConceptIDs {
				if misconByConcept[cid] {
					score += misconBoost
					scoreParts["misconception_boost"] = misconBoost
					break
				}
			}
		}
		if cand.TargetedPrereq && prereqBoost > 0 {
			score += prereqBoost
			scoreParts["prereq_target_boost"] = prereqBoost
		}

		cand.Score = score
		cand.ScoreComponents = scoreParts
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].BlockID < candidates[j].BlockID
		}
		return candidates[i].Score > candidates[j].Score
	})

	maxPerNode := docgen.DocProbeMaxPerNode()
	maxTotal := docgen.DocProbeMaxPerLookahead()
	rateLimit := int(math.Ceil(docgen.DocProbeRatePerHour()))
	if rateLimit < 0 {
		rateLimit = 0
	}
	if rateLimit == 0 {
		out.RateLimited = true
		return out, nil
	}

	rateRemaining := rateLimit
	if rateLimit > 0 {
		since := time.Now().UTC().Add(-1 * time.Hour)
		if count, err := deps.DocProbes.CountByUserSince(dbctx.Context{Ctx: ctx}, in.OwnerUserID, since); err == nil {
			rateRemaining = rateLimit - int(count)
		}
		if rateRemaining <= 0 {
			out.RateLimited = true
			return out, nil
		}
	}

	perNodeSelected := map[uuid.UUID]int{}
	for nodeID, count := range existingActiveCount {
		perNodeSelected[nodeID] = count
	}
	selected := []docProbeCandidate{}
	for _, cand := range candidates {
		if maxTotal > 0 && len(selected) >= maxTotal {
			break
		}
		if rateLimit > 0 && rateRemaining <= 0 {
			out.RateLimited = true
			break
		}
		if cand.InfoGain < minInfoGain && !cand.TargetedPrereq {
			continue
		}
		if maxPerNode > 0 && perNodeSelected[cand.NodeID] >= maxPerNode {
			continue
		}
		if existing := existingByNode[cand.NodeID]; existing != nil {
			if row := existing[cand.BlockID]; row != nil {
				continue
			}
		}
		selected = append(selected, cand)
		perNodeSelected[cand.NodeID]++
		if rateLimit > 0 {
			rateRemaining--
		}
	}

	if len(selected) == 0 {
		return out, nil
	}
	out.ProbesSelected = len(selected)

	now := time.Now().UTC()
	policyVersion := docgen.DocPolicyVersion()

	err = deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		updatedNodes := map[uuid.UUID]bool{}

		for _, cand := range selected {
			row := &types.DocProbe{
				UserID:        in.OwnerUserID,
				PathID:        pathID,
				PathNodeID:    cand.NodeID,
				BlockID:       cand.BlockID,
				BlockType:     cand.BlockType,
				ProbeKind:     cand.BlockType,
				InfoGain:      cand.InfoGain,
				Score:         cand.Score,
				PolicyVersion: policyVersion,
				SchemaVersion: 1,
				Status:        "planned",
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			if len(cand.ConceptKeys) > 0 {
				row.ConceptKeys = datatypes.JSON(mustJSON(cand.ConceptKeys))
			}
			if len(cand.ConceptIDs) > 0 {
				ids := make([]string, 0, len(cand.ConceptIDs))
				for _, id := range cand.ConceptIDs {
					if id != uuid.Nil {
						ids = append(ids, id.String())
					}
				}
				row.ConceptIDs = datatypes.JSON(mustJSON(ids))
			}
			if len(cand.TriggerAfter) > 0 {
				row.TriggerAfterBlockIDs = datatypes.JSON(mustJSON(cand.TriggerAfter))
			}
			meta := map[string]any{
				"score_components":    cand.ScoreComponents,
				"testlet_id":          cand.TestletID,
				"testlet_type":        cand.TestletType,
				"testlet_uncertainty": cand.TestletUncertainty,
				"targeted_prereq":     cand.TargetedPrereq,
				"selected_at":         now.Format(time.RFC3339),
			}
			row.Metadata = datatypes.JSON(mustJSON(meta))

			if err := deps.DocProbes.Upsert(dbc, row); err != nil {
				return err
			}
			updatedNodes[cand.NodeID] = true

			if doc := docContentByNode[cand.NodeID]; doc != nil && cand.BlockIndex >= 0 && cand.BlockIndex < len(doc.Blocks) {
				block := doc.Blocks[cand.BlockIndex]
				if block != nil {
					block["probe"] = true
					block["probe_score"] = cand.Score
					block["probe_info_gain"] = cand.InfoGain
					if len(cand.ConceptKeys) > 0 {
						block["probe_concept_keys"] = cand.ConceptKeys
					}
					if len(cand.ConceptIDs) > 0 {
						ids := make([]string, 0, len(cand.ConceptIDs))
						for _, id := range cand.ConceptIDs {
							if id != uuid.Nil {
								ids = append(ids, id.String())
							}
						}
						block["probe_concept_ids"] = ids
					}
					if len(cand.TriggerAfter) > 0 && len(stringSliceFromAny(block["trigger_after_block_ids"])) == 0 {
						block["trigger_after_block_ids"] = cand.TriggerAfter
					}
				}
			}
		}

		for nodeID := range updatedNodes {
			doc := docContentByNode[nodeID]
			if doc == nil {
				continue
			}
			docRow := docByNode[nodeID]
			if docRow == nil {
				continue
			}
			raw, err := content.CanonicalizeJSON(doc)
			if err != nil {
				return err
			}
			docRow.DocJSON = datatypes.JSON(raw)
			docRow.ContentHash = content.HashBytes(raw)
			docRow.UpdatedAt = now
			if err := deps.NodeDocs.Upsert(dbc, docRow); err != nil {
				return err
			}
			out.DocsUpdated++

			if deps.DocVariants != nil {
				if variant, err := deps.DocVariants.GetLatestByUserAndNode(dbc, in.OwnerUserID, nodeID); err == nil && variant != nil && len(variant.DocJSON) > 0 && string(variant.DocJSON) != "null" {
					vdoc := content.NodeDocV1{}
					if err := json.Unmarshal(variant.DocJSON, &vdoc); err == nil {
						variantIndex := map[string]int{}
						for i, b := range vdoc.Blocks {
							id := strings.TrimSpace(stringFromAny(b["id"]))
							if id != "" {
								variantIndex[id] = i
							}
						}
						changed := false
						for _, cand := range selected {
							if cand.NodeID != nodeID {
								continue
							}
							idx := -1
							if v, ok := variantIndex[cand.BlockID]; ok {
								idx = v
							}
							if idx >= 0 && idx < len(vdoc.Blocks) {
								block := vdoc.Blocks[idx]
								if block == nil {
									continue
								}
								block["probe"] = true
								block["probe_score"] = cand.Score
								block["probe_info_gain"] = cand.InfoGain
								if len(cand.ConceptKeys) > 0 {
									block["probe_concept_keys"] = cand.ConceptKeys
								}
								if len(cand.ConceptIDs) > 0 {
									ids := make([]string, 0, len(cand.ConceptIDs))
									for _, id := range cand.ConceptIDs {
										if id != uuid.Nil {
											ids = append(ids, id.String())
										}
									}
									block["probe_concept_ids"] = ids
								}
								if len(cand.TriggerAfter) > 0 && len(stringSliceFromAny(block["trigger_after_block_ids"])) == 0 {
									block["trigger_after_block_ids"] = cand.TriggerAfter
								}
								changed = true
							}
						}
						if changed {
							raw, err := content.CanonicalizeJSON(vdoc)
							if err == nil {
								variant.DocJSON = datatypes.JSON(raw)
								variant.ContentHash = content.HashBytes(raw)
								variant.UpdatedAt = now
								_ = deps.DocVariants.Upsert(dbc, variant)
							}
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return out, err
	}

	return out, nil
}

func normalizeConceptKeyProbe(k string) string {
	return strings.TrimSpace(strings.ToLower(k))
}

func normalizeKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		k = normalizeConceptKeyProbe(k)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func dedupeUUIDsProbe(in []uuid.UUID) []uuid.UUID {
	if len(in) == 0 {
		return nil
	}
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(in))
	for _, id := range in {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func hasOverlapKeys(keys []string, set map[string]bool) bool {
	if len(keys) == 0 || len(set) == 0 {
		return false
	}
	for _, k := range keys {
		if set[k] {
			return true
		}
	}
	return false
}

func computeInfoGainProbe(conceptIDs []uuid.UUID, states map[uuid.UUID]*types.UserConceptState) float64 {
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
		mastery := clamp01Probe(st.Mastery)
		unc := math.Max(clamp01Probe(st.EpistemicUncertainty), clamp01Probe(st.AleatoricUncertainty))
		conf := clamp01Probe(st.Confidence)
		gain += (1.0 - mastery) * (0.5 + 0.5*math.Max(unc, 1.0-conf))
	}
	return gain / float64(len(conceptIDs))
}

func clamp01Probe(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func inferTestletType(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		return "quick_check"
	}
	return kind
}

func inferTestletID(block map[string]any, kind string, conceptKeys []string) string {
	if block == nil {
		return ""
	}
	if v := strings.TrimSpace(stringFromAny(block["testlet_id"])); v != "" {
		return v
	}
	if v := strings.TrimSpace(stringFromAny(block["testlet_key"])); v != "" {
		return v
	}
	keys := []string{}
	for _, k := range conceptKeys {
		k = strings.TrimSpace(strings.ToLower(k))
		if k == "" {
			continue
		}
		keys = append(keys, k)
	}
	if len(keys) > 0 {
		sort.Strings(keys)
		return inferTestletType(kind) + ":" + strings.Join(keys, "|")
	}
	if v := strings.TrimSpace(stringFromAny(block["id"])); v != "" {
		return inferTestletType(kind) + ":" + v
	}
	return inferTestletType(kind)
}

func testletUncertainty(st *types.UserTestletState) float64 {
	if st == nil {
		return 0.5
	}
	a := st.BetaA
	b := st.BetaB
	if a <= 0 {
		a = 1
	}
	if b <= 0 {
		b = 1
	}
	variance := betaVariance(a, b)
	if variance <= 0 {
		return 0
	}
	return clamp01Probe(variance / 0.25)
}

func betaVariance(a float64, b float64) float64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	sum := a + b
	return (a * b) / (sum * sum * (sum + 1))
}

func inferTriggerIDs(doc content.NodeDocV1, idx int) []string {
	if idx <= 0 {
		return nil
	}
	citeIDs := map[string]bool{}
	for _, id := range extractChunkIDs(doc.Blocks[idx]["citations"]) {
		if id != "" {
			citeIDs[id] = true
		}
	}
	candidates := []string{}
	for i := idx - 1; i >= 0; i-- {
		b := doc.Blocks[i]
		if b == nil {
			continue
		}
		t := strings.ToLower(strings.TrimSpace(stringFromAny(b["type"])))
		if !isTeachingBlock(t) {
			continue
		}
		id := strings.TrimSpace(stringFromAny(b["id"]))
		if id == "" {
			continue
		}
		if len(citeIDs) > 0 {
			overlap := false
			for _, c := range extractChunkIDs(b["citations"]) {
				if citeIDs[c] {
					overlap = true
					break
				}
			}
			if !overlap {
				continue
			}
		}
		candidates = append(candidates, id)
		if len(candidates) >= 3 {
			break
		}
	}
	if len(candidates) == 0 {
		for i := idx - 1; i >= 0; i-- {
			b := doc.Blocks[i]
			if b == nil {
				continue
			}
			t := strings.ToLower(strings.TrimSpace(stringFromAny(b["type"])))
			if !isTeachingBlock(t) {
				continue
			}
			id := strings.TrimSpace(stringFromAny(b["id"]))
			if id != "" {
				candidates = append(candidates, id)
				break
			}
		}
	}
	return candidates
}

func extractChunkIDs(raw any) []string {
	out := []string{}
	for _, item := range stringSliceFromAny(raw) {
		if item == "" {
			continue
		}
		out = appendIfMissing(out, item)
	}
	switch arr := raw.(type) {
	case []any:
		for _, v := range arr {
			m, ok := v.(map[string]any)
			if !ok {
				continue
			}
			id := strings.TrimSpace(stringFromAny(m["chunk_id"]))
			if id != "" {
				out = appendIfMissing(out, id)
			}
		}
	}
	return out
}

func isTeachingBlock(t string) bool {
	t = strings.ToLower(strings.TrimSpace(t))
	switch t {
	case "", "quick_check", "flashcard", "heading", "divider", "objectives", "prerequisites", "key_takeaways", "glossary":
		return false
	default:
		return true
	}
}

func appendIfMissing(list []string, v string) []string {
	if v == "" {
		return list
	}
	for _, item := range list {
		if item == v {
			return list
		}
	}
	return append(list, v)
}

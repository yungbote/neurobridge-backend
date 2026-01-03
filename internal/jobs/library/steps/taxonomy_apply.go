package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
)

func applyRouteResult(
	ctx context.Context,
	deps LibraryTaxonomyRouteDeps,
	userID uuid.UUID,
	facet string,
	pathID uuid.UUID,
	pathEmb []float32,
	root *types.LibraryTaxonomyNode,
	inbox *types.LibraryTaxonomyNode,
	modelOut routeModelOut,
	maxMemberships int,
	maxNewNodes int,
) (nodesCreated int, edgesUpserted int, membersUpserted int, usedInbox bool, err error) {
	facet = normalizeFacet(facet)
	if facet == "" {
		return 0, 0, 0, false, nil
	}
	if userID == uuid.Nil || pathID == uuid.Nil || root == nil || inbox == nil {
		return 0, 0, 0, false, fmt.Errorf("applyRouteResult: missing inputs")
	}

	now := time.Now().UTC()

	// Defensive enforcement: if routing disallows new nodes, ignore them even if the model returns them.
	if maxNewNodes <= 0 {
		modelOut.NewNodes = nil
	} else if len(modelOut.NewNodes) > maxNewNodes {
		modelOut.NewNodes = modelOut.NewNodes[:maxNewNodes]
	}

	// Load current nodes so we can validate ids and do incremental centroid updates.
	nodes, err := deps.TaxNodes.GetByUserFacet(dbctx.Context{Ctx: ctx}, userID, facet)
	if err != nil {
		return 0, 0, 0, false, err
	}
	nodeByID := map[uuid.UUID]*types.LibraryTaxonomyNode{}
	for _, n := range nodes {
		if n == nil || n.ID == uuid.Nil {
			continue
		}
		nodeByID[n.ID] = n
	}

	anchorIDs := map[uuid.UUID]bool{}
	for _, n := range nodes {
		if n == nil || n.ID == uuid.Nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(n.Kind), "anchor") {
			anchorIDs[n.ID] = true
		}
	}

	// Create any new nodes requested by the model.
	newNodeIDByClient := map[string]uuid.UUID{}
	newNodes := make([]*types.LibraryTaxonomyNode, 0, len(modelOut.NewNodes))
	newEdges := make([]*types.LibraryTaxonomyEdge, 0, len(modelOut.NewNodes)*2)
	for _, nn := range modelOut.NewNodes {
		name := strings.TrimSpace(nn.Name)
		desc := strings.TrimSpace(nn.Description)
		clientID := strings.TrimSpace(nn.ClientID)
		if name == "" || clientID == "" {
			continue
		}
		id := uuid.New()
		newNodeIDByClient[clientID] = id

		kind := strings.ToLower(strings.TrimSpace(nn.Kind))
		if kind == "" || kind == "root" || kind == "inbox" {
			kind = "category"
		}

		stats := map[string]any{"member_count": 1}
		node := &types.LibraryTaxonomyNode{
			ID:          id,
			UserID:      userID,
			Facet:       facet,
			Key:         "cat_" + id.String(),
			Kind:        kind,
			Name:        name,
			Description: desc,
			Embedding:   datatypes.JSON(toJSON(pathEmb)),
			Stats:       datatypes.JSON(toJSON(stats)),
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		newNodes = append(newNodes, node)

		parentIDs := make([]uuid.UUID, 0, len(nn.ParentNodeIDs))
		for _, pid := range nn.ParentNodeIDs {
			pid = strings.TrimSpace(pid)
			if pid == "" {
				continue
			}
			uid, uErr := uuid.Parse(pid)
			if uErr != nil || uid == uuid.Nil {
				continue
			}
			if uid == inbox.ID {
				continue
			}
			if _, ok := nodeByID[uid]; ok || uid == root.ID {
				parentIDs = append(parentIDs, uid)
			}
		}
		if len(parentIDs) == 0 {
			parentIDs = append(parentIDs, root.ID)
		}

		parentIDs = dedupeUUIDs(parentIDs)
		for _, pid := range parentIDs {
			newEdges = append(newEdges, &types.LibraryTaxonomyEdge{
				ID:         uuid.New(),
				UserID:     userID,
				Facet:      facet,
				Kind:       "subsumes",
				FromNodeID: pid,
				ToNodeID:   id,
				Weight:     1,
				Metadata:   datatypes.JSON([]byte(`{}`)),
				Version:    1,
				CreatedAt:  now,
				UpdatedAt:  now,
			})
		}

		relatedIDs := make([]uuid.UUID, 0, len(nn.RelatedNodeIDs))
		for _, rid := range nn.RelatedNodeIDs {
			rid = strings.TrimSpace(rid)
			if rid == "" {
				continue
			}
			uid, uErr := uuid.Parse(rid)
			if uErr != nil || uid == uuid.Nil {
				continue
			}
			if uid == root.ID || uid == inbox.ID {
				continue
			}
			if _, ok := nodeByID[uid]; ok {
				relatedIDs = append(relatedIDs, uid)
			}
		}
		relatedIDs = dedupeUUIDs(relatedIDs)
		for _, rid := range relatedIDs {
			w := clamp01(nn.MembershipWeight)
			if w <= 0 {
				w = 0.7
			}
			newEdges = append(newEdges, &types.LibraryTaxonomyEdge{
				ID:         uuid.New(),
				UserID:     userID,
				Facet:      facet,
				Kind:       "related",
				FromNodeID: id,
				ToNodeID:   rid,
				Weight:     w,
				Metadata:   datatypes.JSON(toJSON(map[string]any{"reason": strings.TrimSpace(nn.Reason)})),
				Version:    1,
				CreatedAt:  now,
				UpdatedAt:  now,
			})
			newEdges = append(newEdges, &types.LibraryTaxonomyEdge{
				ID:         uuid.New(),
				UserID:     userID,
				Facet:      facet,
				Kind:       "related",
				FromNodeID: rid,
				ToNodeID:   id,
				Weight:     w,
				Metadata:   datatypes.JSON(toJSON(map[string]any{"reason": strings.TrimSpace(nn.Reason)})),
				Version:    1,
				CreatedAt:  now,
				UpdatedAt:  now,
			})
		}
	}

	if len(newNodes) > 0 {
		if err := deps.TaxNodes.UpsertMany(dbctx.Context{Ctx: ctx}, newNodes); err != nil {
			return 0, 0, 0, false, err
		}
		nodesCreated = len(newNodes)
		for _, n := range newNodes {
			nodeByID[n.ID] = n
		}
	}
	if len(newEdges) > 0 {
		if err := deps.TaxEdges.UpsertMany(dbctx.Context{Ctx: ctx}, newEdges); err != nil {
			return nodesCreated, 0, 0, false, err
		}
		edgesUpserted = len(newEdges)
	}

	// Collect final membership targets.
	type target struct {
		nodeID uuid.UUID
		weight float64
		reason string
	}
	targetByNode := map[uuid.UUID]target{}
	for _, m := range modelOut.Memberships {
		idStr := strings.TrimSpace(m.NodeID)
		uid, uErr := uuid.Parse(idStr)
		if uErr != nil || uid == uuid.Nil || uid == root.ID {
			continue
		}
		if uid == inbox.ID || nodeByID[uid] != nil {
			cur, ok := targetByNode[uid]
			w := clamp01(m.Weight)
			if uid != inbox.ID && anchorIDs[uid] && w < 0.97 {
				w = 0.97
			}
			if !ok || w > cur.weight {
				targetByNode[uid] = target{nodeID: uid, weight: w, reason: strings.TrimSpace(m.Reason)}
			}
		}
	}
	for _, nn := range modelOut.NewNodes {
		id := newNodeIDByClient[strings.TrimSpace(nn.ClientID)]
		if id == uuid.Nil {
			continue
		}
		w := clamp01(nn.MembershipWeight)
		if w <= 0 {
			w = 0.8
		}
		targetByNode[id] = target{nodeID: id, weight: w, reason: strings.TrimSpace(nn.Reason)}
	}

	targets := make([]target, 0, len(targetByNode))
	for _, t := range targetByNode {
		// Never allow direct membership to the root.
		if t.nodeID == root.ID {
			continue
		}
		// Don't allow empty weights to silently "assign".
		if t.weight <= 0 {
			continue
		}
		targets = append(targets, t)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].weight > targets[j].weight })

	// Remove inbox if we have any non-inbox assignment.
	hasNonInbox := false
	for _, t := range targets {
		if t.nodeID != inbox.ID {
			hasNonInbox = true
			break
		}
	}
	if hasNonInbox {
		tmp := targets[:0]
		for _, t := range targets {
			if t.nodeID == inbox.ID {
				continue
			}
			tmp = append(tmp, t)
		}
		targets = tmp
		// If we assigned anything other than the inbox, remove any pre-existing inbox membership for this path.
		deleteMembershipsForNodeAndPaths(ctx, deps, userID, facet, inbox.ID, []uuid.UUID{pathID})
	}

	// Ensure the topic facet always has at least one seeded anchor membership when anchors exist.
	// If the model omitted anchors but chose a descendant category, infer anchors from subsumes edges.
	if facet == "topic" && len(anchorIDs) > 0 {
		hasAnchor := false
		for _, t := range targets {
			if anchorIDs[t.nodeID] {
				hasAnchor = true
				break
			}
		}
		if !hasAnchor {
			edges, eErr := deps.TaxEdges.GetByUserFacetKind(dbctx.Context{Ctx: ctx}, userID, facet, "subsumes")
			if eErr == nil && len(edges) > 0 {
				parentsByChild := map[uuid.UUID][]uuid.UUID{}
				for _, e := range edges {
					if e == nil || e.FromNodeID == uuid.Nil || e.ToNodeID == uuid.Nil {
						continue
					}
					parentsByChild[e.ToNodeID] = append(parentsByChild[e.ToNodeID], e.FromNodeID)
				}
				bestByAnchor := map[uuid.UUID]float64{}
				for _, t := range targets {
					if t.nodeID == uuid.Nil || t.nodeID == inbox.ID {
						continue
					}
					q := []uuid.UUID{t.nodeID}
					seen := map[uuid.UUID]bool{t.nodeID: true}
					for len(q) > 0 {
						cur := q[0]
						q = q[1:]
						for _, pid := range parentsByChild[cur] {
							if pid == uuid.Nil || seen[pid] {
								continue
							}
							seen[pid] = true
							if anchorIDs[pid] {
								w := t.weight
								if w < 0.97 {
									w = 0.97
								}
								if w > bestByAnchor[pid] {
									bestByAnchor[pid] = w
								}
								continue
							}
							q = append(q, pid)
						}
					}
				}

				for aid, w := range bestByAnchor {
					targets = append(targets, target{nodeID: aid, weight: w, reason: "implied_by_subsumes"})
				}

				// Dedupe by node_id (keep highest weight) and re-sort.
				if len(targets) > 0 {
					byID := map[uuid.UUID]target{}
					for _, t := range targets {
						cur, ok := byID[t.nodeID]
						if !ok || t.weight > cur.weight {
							byID[t.nodeID] = t
						}
					}
					targets = targets[:0]
					for _, t := range byID {
						targets = append(targets, t)
					}
					sort.Slice(targets, func(i, j int) bool { return targets[i].weight > targets[j].weight })
				}
			}
		}
	}

	if maxMemberships <= 0 {
		maxMemberships = 4
	}
	if len(targets) > maxMemberships {
		targets = targets[:maxMemberships]
	}
	if len(targets) == 0 {
		targets = append(targets, target{nodeID: inbox.ID, weight: 1, reason: "No strong existing category match."})
	}
	for _, t := range targets {
		if t.nodeID == inbox.ID {
			usedInbox = true
			break
		}
	}

	// Upsert memberships for this path in this facet.
	memRows := make([]*types.LibraryTaxonomyMembership, 0, len(targets))
	for _, t := range targets {
		meta := map[string]any{}
		if strings.TrimSpace(t.reason) != "" {
			meta["reason"] = strings.TrimSpace(t.reason)
		}
		memRows = append(memRows, &types.LibraryTaxonomyMembership{
			ID:         uuid.New(),
			UserID:     userID,
			Facet:      facet,
			PathID:     pathID,
			NodeID:     t.nodeID,
			Weight:     clamp01(t.weight),
			AssignedBy: "route",
			Metadata:   datatypes.JSON(toJSON(meta)),
			Version:    1,
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}
	if err := deps.Membership.UpsertMany(dbctx.Context{Ctx: ctx}, memRows); err != nil {
		return nodesCreated, edgesUpserted, 0, usedInbox, err
	}
	membersUpserted = len(memRows)

	// Best-effort centroid update for touched nodes (skip root).
	touched := make([]*types.LibraryTaxonomyNode, 0, len(targets))
	for _, t := range targets {
		n := nodeByID[t.nodeID]
		if n == nil || n.ID == uuid.Nil {
			// New nodes might not be in nodeByID if they weren't returned by GetByUserFacet above.
			continue
		}
		if strings.EqualFold(strings.TrimSpace(n.Kind), "root") {
			continue
		}
		emb, ok := parseFloat32ArrayJSON(n.Embedding)
		if !ok || len(emb) == 0 {
			n.Embedding = datatypes.JSON(toJSON(pathEmb))
		} else if len(pathEmb) > 0 && len(pathEmb) == len(emb) {
			count := intFromStats(n.Stats, "member_count", 0)
			if count < 0 {
				count = 0
			}
			n.Embedding = datatypes.JSON(toJSON(incrementalMean(emb, pathEmb, count)))
			n.Stats = datatypes.JSON(toJSON(setIntStat(n.Stats, "member_count", count+1)))
		}
		n.UpdatedAt = now
		touched = append(touched, n)
	}
	if len(touched) > 0 {
		_ = deps.TaxNodes.UpsertMany(dbctx.Context{Ctx: ctx}, touched)
	}

	return nodesCreated, edgesUpserted, membersUpserted, usedInbox, nil
}

func dedupeUUIDs(ids []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func intFromStats(raw datatypes.JSON, key string, def int) int {
	if len(raw) == 0 || key == "" {
		return def
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return def
	}
	v, ok := obj[key]
	if !ok {
		return def
	}
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	default:
		return def
	}
}

func setIntStat(raw datatypes.JSON, key string, value int) map[string]any {
	obj := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &obj)
	}
	obj[key] = value
	return obj
}

func incrementalMean(old, add []float32, oldCount int) []float32 {
	if len(old) == 0 {
		return add
	}
	if len(add) == 0 || len(add) != len(old) {
		return old
	}
	if oldCount < 0 {
		oldCount = 0
	}
	out := make([]float32, len(old))
	den := float32(oldCount + 1)
	for i := range old {
		out[i] = (old[i]*float32(oldCount) + add[i]) / den
	}
	return out
}

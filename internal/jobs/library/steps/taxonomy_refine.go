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
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/learning/prompts"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/envutil"
)

type LibraryTaxonomyRefineInput struct {
	UserID uuid.UUID `json:"user_id"`
	Force  bool      `json:"force,omitempty"`
}

type LibraryTaxonomyRefineOutput struct {
	UserID uuid.UUID `json:"user_id"`

	FacetsProcessed int `json:"facets_processed"`
	PathsConsidered int `json:"paths_considered"`
	PathsReassigned int `json:"paths_reassigned"`

	NodesCreated    int `json:"nodes_created"`
	EdgesUpserted   int `json:"edges_upserted"`
	MembersUpserted int `json:"members_upserted"`

	Skipped bool `json:"skipped"`
}

type refineProposedNode struct {
	ClientID       string `json:"client_id"`
	ParentNodeID   string `json:"parent_node_id"`
	ParentNodeKey  string `json:"parent_node_key"`
	ParentNodeName string `json:"parent_node_name"`
	PathCount      int    `json:"path_count"`
	SamplePaths    []struct {
		PathID      string `json:"path_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
	} `json:"sample_paths"`
}

type refineDecision struct {
	ClientID     string `json:"client_id"`
	ShouldCreate bool   `json:"should_create"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Reason       string `json:"reason"`
}

type refineModelOut struct {
	Version   int              `json:"version"`
	Facet     string           `json:"facet"`
	Decisions []refineDecision `json:"decisions"`
}

type pathVec struct {
	Path *types.Path
	Vec  []float32
}

type kCluster struct {
	Centroid []float32
	Members  []pathVec
}

func LibraryTaxonomyRefine(ctx context.Context, deps LibraryTaxonomyRouteDeps, in LibraryTaxonomyRefineInput) (LibraryTaxonomyRefineOutput, error) {
	out := LibraryTaxonomyRefineOutput{UserID: in.UserID}
	if deps.DB == nil || deps.Log == nil || deps.AI == nil || deps.Path == nil || deps.Clusters == nil || deps.TaxNodes == nil || deps.TaxEdges == nil || deps.Membership == nil || deps.State == nil || deps.Snapshots == nil || deps.PathVectors == nil {
		return out, fmt.Errorf("library_taxonomy_refine: missing deps")
	}
	if in.UserID == uuid.Nil {
		return out, fmt.Errorf("library_taxonomy_refine: missing user_id")
	}

	// Coalesce: acquire a per-user refine lock (best-effort, no hard fail if state table missing).
	lockMinutes := envutil.Int("LIBRARY_TAXONOMY_REFINE_LOCK_MINUTES", 30)
	if lockMinutes < 5 {
		lockMinutes = 30
	}
	now := time.Now().UTC()
	lockUntil := now.Add(time.Duration(lockMinutes) * time.Minute)

	err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var st types.LibraryTaxonomyState
		txq := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", in.UserID).Limit(1)
		_ = txq.Find(&st).Error
		if st.ID != uuid.Nil && st.RefineLockUntil != nil && st.RefineLockUntil.After(now) && !in.Force {
			out.Skipped = true
			return nil
		}
		if st.ID == uuid.Nil {
			st = types.LibraryTaxonomyState{
				ID:        uuid.New(),
				UserID:    in.UserID,
				Version:   1,
				Dirty:     true,
				CreatedAt: now,
				UpdatedAt: now,
			}
		}
		st.RefineLockUntil = &lockUntil
		st.UpdatedAt = now
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"refine_lock_until": lockUntil,
				"updated_at":        now,
				"deleted_at":        nil,
			}),
		}).Create(&st).Error
	})
	if err != nil {
		return out, err
	}
	if out.Skipped {
		return out, nil
	}

	paths, err := deps.Path.ListByUser(dbctx.Context{Ctx: ctx}, &in.UserID)
	if err != nil {
		return out, err
	}
	readyPaths := make([]*types.Path, 0, len(paths))
	for _, p := range paths {
		if p == nil || p.ID == uuid.Nil {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(p.Status), "ready") {
			continue
		}
		readyPaths = append(readyPaths, p)
	}
	out.PathsConsidered = len(readyPaths)
	if len(readyPaths) == 0 {
		_ = deps.State.UpdateFields(dbctx.Context{Ctx: ctx}, in.UserID, map[string]interface{}{
			"dirty":                  false,
			"new_paths_since_refine": 0,
			"pending_unsorted_paths": 0,
			"last_refined_at":        &now,
			"refine_lock_until":      nil,
		})
		_ = BuildAndPersistLibraryTaxonomySnapshot(ctx, deps, in.UserID)
		return out, nil
	}

	assignThreshold := float64(0.82)
	if v := envutil.Int("LIBRARY_TAXONOMY_ASSIGN_SIMILARITY_THRESHOLD_PCT", 82); v >= 50 && v <= 98 {
		assignThreshold = float64(v) / 100.0
	}
	maxNewNodes := envutil.Int("LIBRARY_TAXONOMY_REFINE_MAX_NEW_NODES_PER_FACET", 3)
	if maxNewNodes < 0 {
		maxNewNodes = 0
	}
	if maxNewNodes > 6 {
		maxNewNodes = 6
	}

	relatedThreshold := float64(0.88)
	if v := envutil.Int("LIBRARY_TAXONOMY_RELATED_SIMILARITY_THRESHOLD_PCT", 88); v >= 60 && v <= 99 {
		relatedThreshold = float64(v) / 100.0
	}
	maxRelatedPerNode := envutil.Int("LIBRARY_TAXONOMY_MAX_RELATED_PER_NODE", 4)
	if maxRelatedPerNode < 1 {
		maxRelatedPerNode = 4
	}
	if maxRelatedPerNode > 8 {
		maxRelatedPerNode = 8
	}

	minCategoryPaths := envutil.Int("LIBRARY_TAXONOMY_MIN_CATEGORY_PATHS", 3)
	if minCategoryPaths < 2 {
		minCategoryPaths = 3
	}
	if minCategoryPaths > 10 {
		minCategoryPaths = 10
	}
	minAnchorPathsForChild := envutil.Int("LIBRARY_TAXONOMY_ANCHOR_MIN_PATHS_FOR_CHILD", 5)
	if minAnchorPathsForChild < 3 {
		minAnchorPathsForChild = 5
	}
	if minAnchorPathsForChild > 50 {
		minAnchorPathsForChild = 50
	}
	clusterCohesionThreshold := float64(0.82)
	if v := envutil.Int("LIBRARY_TAXONOMY_CLUSTER_COHESION_THRESHOLD_PCT", 82); v >= 50 && v <= 98 {
		clusterCohesionThreshold = float64(v) / 100.0
	}

	// Path embeddings cached per call.
	pathEmbByID := map[uuid.UUID][]float32{}
	pathByID := map[uuid.UUID]*types.Path{}
	for _, p := range readyPaths {
		if p == nil || p.ID == uuid.Nil {
			continue
		}
		pathByID[p.ID] = p
	}

	getEmb := func(p *types.Path) ([]float32, error) {
		if p == nil || p.ID == uuid.Nil {
			return nil, fmt.Errorf("missing path")
		}
		if v := pathEmbByID[p.ID]; len(v) > 0 {
			return v, nil
		}
		emb, err := getOrComputePathEmbedding(ctx, deps, in.UserID, p)
		if err != nil {
			return nil, err
		}
		emb = normalizeUnit(emb)
		pathEmbByID[p.ID] = emb
		return emb, nil
	}

	for _, facet := range defaultTaxonomyFacets {
		facet = normalizeFacet(facet)
		if facet == "" {
			continue
		}

		root, inbox, err := ensureFacetDefaults(ctx, deps, in.UserID, facet)
		if err != nil {
			return out, err
		}
		if root == nil || inbox == nil {
			return out, fmt.Errorf("library_taxonomy_refine: missing facet defaults")
		}

		anchors, err := ensureTopicAnchors(ctx, deps, in.UserID, facet, root)
		if err != nil {
			return out, err
		}

		nodes, err := deps.TaxNodes.GetByUserFacet(dbctx.Context{Ctx: ctx}, in.UserID, facet)
		if err != nil {
			return out, err
		}
		mems, err := deps.Membership.GetByUserFacet(dbctx.Context{Ctx: ctx}, in.UserID, facet)
		if err != nil {
			return out, err
		}

		readySet := map[uuid.UUID]bool{}
		readyIDs := make([]uuid.UUID, 0, len(readyPaths))
		for _, p := range readyPaths {
			if p == nil || p.ID == uuid.Nil {
				continue
			}
			readySet[p.ID] = true
			readyIDs = append(readyIDs, p.ID)
		}

		nodeByID := map[uuid.UUID]*types.LibraryTaxonomyNode{}
		categoryByID := map[uuid.UUID]*types.LibraryTaxonomyNode{}
		allNonRootNodes := make([]*types.LibraryTaxonomyNode, 0, len(nodes))
		for _, n := range nodes {
			if n == nil || n.ID == uuid.Nil {
				continue
			}
			nodeByID[n.ID] = n
			k := strings.ToLower(strings.TrimSpace(n.Kind))
			if k == "root" || k == "inbox" {
				continue
			}
			if emb, ok := parseFloat32ArrayJSON(n.Embedding); ok && len(emb) > 0 {
				// normalize for cosine-based routing
				n.Embedding = datatypes.JSON(toJSON(normalizeUnit(emb)))
			}
			allNonRootNodes = append(allNonRootNodes, n)
			if k != "anchor" {
				categoryByID[n.ID] = n
			}
		}

		hasCategoryMembership := map[uuid.UUID]bool{}
		anchorIDsByPath := map[uuid.UUID][]uuid.UUID{}
		anchorMemberIDs := map[uuid.UUID][]uuid.UUID{}
		for _, m := range mems {
			if m == nil || m.PathID == uuid.Nil || m.NodeID == uuid.Nil {
				continue
			}
			if !readySet[m.PathID] {
				continue
			}
			if m.NodeID == root.ID {
				continue
			}
			if m.NodeID == inbox.ID {
				continue
			}
			n := nodeByID[m.NodeID]
			if n == nil {
				continue
			}
			kind := strings.ToLower(strings.TrimSpace(n.Kind))
			if kind == "anchor" {
				anchorIDsByPath[m.PathID] = append(anchorIDsByPath[m.PathID], m.NodeID)
				anchorMemberIDs[m.NodeID] = append(anchorMemberIDs[m.NodeID], m.PathID)
				continue
			}
			hasCategoryMembership[m.PathID] = true
		}
		for pid, ids := range anchorIDsByPath {
			anchorIDsByPath[pid] = dedupeUUIDs(ids)
		}

		unsorted := make([]*types.Path, 0, len(readyPaths))
		for _, p := range readyPaths {
			if p == nil || p.ID == uuid.Nil {
				continue
			}
			if !hasCategoryMembership[p.ID] {
				unsorted = append(unsorted, p)
			}
		}

		// Phase 0: normalize topic anchor memberships deterministically (primary + optional secondary).
		// This keeps anchors acting like domains and prevents noisy multi-anchor duplication.
		if facet == "topic" && deps.DB != nil && len(anchors) > 0 && len(readyIDs) > 0 {
			type anchorEmb struct {
				id  uuid.UUID
				key string
				emb []float32
			}
			anchorEmbeds := make([]anchorEmb, 0, len(anchors))
			anchorIDs := make([]uuid.UUID, 0, len(anchors))
			for _, a := range anchors {
				if a == nil || a.ID == uuid.Nil {
					continue
				}
				emb, ok := parseFloat32ArrayJSON(a.Embedding)
				if !ok || len(emb) == 0 {
					continue
				}
				anchorEmbeds = append(anchorEmbeds, anchorEmb{id: a.ID, key: strings.TrimSpace(a.Key), emb: emb})
				anchorIDs = append(anchorIDs, a.ID)
			}
			if len(anchorEmbeds) > 0 && len(anchorIDs) > 0 {
				secondaryMin := float64(0.88)
				if v := envutil.Int("LIBRARY_TAXONOMY_SECONDARY_ANCHOR_MIN_SIMILARITY_THRESHOLD_PCT", 88); v >= 50 && v <= 99 {
					secondaryMin = float64(v) / 100.0
				}
				secondaryMaxGap := float64(0.02)
				if v := envutil.Int("LIBRARY_TAXONOMY_SECONDARY_ANCHOR_MAX_GAP_PCT", 2); v >= 0 && v <= 20 {
					secondaryMaxGap = float64(v) / 100.0
				}

				toWeight := func(score float64) float64 {
					w := (score + 1.0) / 2.0
					if w < 0.01 {
						w = 0.01
					}
					return clamp01(w)
				}

				anchorMems := make([]*types.LibraryTaxonomyMembership, 0, len(readyIDs)*2)
				normalizedPathIDs := make([]uuid.UUID, 0, len(readyIDs))
				for _, p := range readyPaths {
					if p == nil || p.ID == uuid.Nil {
						continue
					}
					emb, err := getEmb(p)
					if err != nil || len(emb) == 0 {
						continue
					}
					type scored struct {
						id    uuid.UUID
						key   string
						score float64
					}
					scoredAnchors := make([]scored, 0, len(anchorEmbeds))
					for _, a := range anchorEmbeds {
						if a.id == uuid.Nil || len(a.emb) == 0 {
							continue
						}
						scoredAnchors = append(scoredAnchors, scored{
							id:    a.id,
							key:   a.key,
							score: cosineSimilarity(emb, a.emb),
						})
					}
					sort.Slice(scoredAnchors, func(i, j int) bool {
						if scoredAnchors[i].score != scoredAnchors[j].score {
							return scoredAnchors[i].score > scoredAnchors[j].score
						}
						return scoredAnchors[i].key < scoredAnchors[j].key
					})
					if len(scoredAnchors) == 0 {
						continue
					}
					primary := scoredAnchors[0]
					secondary := scored{id: uuid.Nil}
					if len(scoredAnchors) > 1 {
						secondary = scoredAnchors[1]
					}
					allowSecondary := secondary.id != uuid.Nil &&
						secondary.score >= secondaryMin &&
						(primary.score-secondary.score) <= secondaryMaxGap
					normalizedPathIDs = append(normalizedPathIDs, p.ID)
					anchorMems = append(anchorMems, &types.LibraryTaxonomyMembership{
						ID:         uuid.New(),
						UserID:     in.UserID,
						Facet:      facet,
						PathID:     p.ID,
						NodeID:     primary.id,
						Weight:     toWeight(primary.score),
						AssignedBy: "refine",
						Metadata:   datatypes.JSON(toJSON(map[string]any{"reason": "primary_anchor_by_embedding"})),
						Version:    1,
						CreatedAt:  now,
						UpdatedAt:  now,
					})
					if allowSecondary {
						anchorMems = append(anchorMems, &types.LibraryTaxonomyMembership{
							ID:         uuid.New(),
							UserID:     in.UserID,
							Facet:      facet,
							PathID:     p.ID,
							NodeID:     secondary.id,
							Weight:     toWeight(secondary.score),
							AssignedBy: "refine",
							Metadata:   datatypes.JSON(toJSON(map[string]any{"reason": "secondary_anchor_by_embedding"})),
							Version:    1,
							CreatedAt:  now,
							UpdatedAt:  now,
						})
					}
				}
				normalizedPathIDs = dedupeUUIDs(normalizedPathIDs)
				if len(anchorMems) > 0 && len(normalizedPathIDs) > 0 {
					if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
						if err := tx.Where("user_id = ? AND facet = ? AND node_id IN ? AND path_id IN ?", in.UserID, facet, anchorIDs, normalizedPathIDs).
							Delete(&types.LibraryTaxonomyMembership{}).Error; err != nil {
							return err
						}
						return deps.Membership.UpsertMany(dbctx.Context{Ctx: ctx, Tx: tx}, anchorMems)
					}); err != nil {
						return out, err
					}
					// Reload memberships so downstream phases see normalized anchors.
					mems, err = deps.Membership.GetByUserFacet(dbctx.Context{Ctx: ctx}, in.UserID, facet)
					if err != nil {
						return out, err
					}
				}
			}
		}

		// Build anchor -> descendant category id lists from subsumes edges.
		subsumesEdges, err := deps.TaxEdges.GetByUserFacetKind(dbctx.Context{Ctx: ctx}, in.UserID, facet, "subsumes")
		if err != nil {
			return out, err
		}
		childrenByParent := map[uuid.UUID][]uuid.UUID{}
		for _, e := range subsumesEdges {
			if e == nil || e.FromNodeID == uuid.Nil || e.ToNodeID == uuid.Nil {
				continue
			}
			childrenByParent[e.FromNodeID] = append(childrenByParent[e.FromNodeID], e.ToNodeID)
		}

		descCatsByAnchor := map[uuid.UUID][]uuid.UUID{}
		for _, a := range anchors {
			if a == nil || a.ID == uuid.Nil {
				continue
			}
			q := []uuid.UUID{a.ID}
			seen := map[uuid.UUID]bool{a.ID: true}
			outIDs := make([]uuid.UUID, 0, 32)
			for len(q) > 0 {
				cur := q[0]
				q = q[1:]
				for _, cid := range childrenByParent[cur] {
					if cid == uuid.Nil || seen[cid] {
						continue
					}
					seen[cid] = true
					n := nodeByID[cid]
					if n == nil {
						continue
					}
					kind := strings.ToLower(strings.TrimSpace(n.Kind))
					if kind == "root" || kind == "inbox" {
						continue
					}
					if kind != "anchor" {
						outIDs = append(outIDs, cid)
					}
					q = append(q, cid)
				}
			}
			descCatsByAnchor[a.ID] = dedupeUUIDs(outIDs)
		}

		// Phase 1: assign "unsorted" (anchor-only) paths to existing earned categories under their anchors.
		reassignedIDs := make([]uuid.UUID, 0, len(unsorted))
		newMems := make([]*types.LibraryTaxonomyMembership, 0, len(unsorted))
		for _, p := range unsorted {
			if p == nil || p.ID == uuid.Nil {
				continue
			}
			anchorIDs := anchorIDsByPath[p.ID]
			if len(anchorIDs) == 0 {
				continue
			}
			emb, err := getEmb(p)
			if err != nil || len(emb) == 0 {
				continue
			}

			candSet := map[uuid.UUID]bool{}
			for _, aid := range anchorIDs {
				for _, cid := range descCatsByAnchor[aid] {
					if cid == uuid.Nil || candSet[cid] {
						continue
					}
					n := categoryByID[cid]
					if n == nil {
						continue
					}
					if intFromStats(n.Stats, "member_count", 0) > 0 && intFromStats(n.Stats, "member_count", 0) < minCategoryPaths {
						continue
					}
					candSet[cid] = true
				}
			}
			if len(candSet) == 0 {
				continue
			}

			bestScore := 0.0
			bestID := uuid.Nil
			for cid := range candSet {
				n := categoryByID[cid]
				if n == nil {
					continue
				}
				cent, ok := parseFloat32ArrayJSON(n.Embedding)
				if !ok || len(cent) == 0 {
					continue
				}
				s := cosineSimilarity(emb, cent)
				if s > bestScore {
					bestScore = s
					bestID = cid
				}
			}

			if bestID != uuid.Nil && bestScore >= assignThreshold {
				reassignedIDs = append(reassignedIDs, p.ID)
				hasCategoryMembership[p.ID] = true
				newMems = append(newMems, &types.LibraryTaxonomyMembership{
					ID:         uuid.New(),
					UserID:     in.UserID,
					Facet:      facet,
					PathID:     p.ID,
					NodeID:     bestID,
					Weight:     clamp01(bestScore),
					AssignedBy: "refine",
					Metadata:   datatypes.JSON(toJSON(map[string]any{"reason": "assigned_by_similarity_under_anchor"})),
					Version:    1,
					CreatedAt:  now,
					UpdatedAt:  now,
				})
			}
		}
		if len(newMems) > 0 {
			if err := deps.Membership.UpsertMany(dbctx.Context{Ctx: ctx}, newMems); err != nil {
				return out, err
			}
			deleteMembershipsForNodeAndPaths(ctx, deps, in.UserID, facet, inbox.ID, reassignedIDs)
			out.PathsReassigned += len(reassignedIDs)
			out.MembersUpserted += len(newMems)
		}

		// Phase 2: create new categories bottom-up under seeded anchors, only when earned.
		type pendingCluster struct {
			ParentNodeID   uuid.UUID
			ParentNodeKey  string
			ParentNodeName string
			Cluster        kCluster
		}

		if maxNewNodes > 0 && len(anchors) > 0 {
			proposed := make([]refineProposedNode, 0, maxNewNodes)
			clusterByClient := map[string]pendingCluster{}

			remainingBudget := maxNewNodes
			for _, a := range anchors {
				if remainingBudget <= 0 {
					break
				}
				if a == nil || a.ID == uuid.Nil {
					continue
				}
				anchorPaths := dedupeUUIDs(anchorMemberIDs[a.ID])
				if len(anchorPaths) < minAnchorPathsForChild {
					continue
				}

				unsortedIDs := make([]uuid.UUID, 0, len(anchorPaths))
				for _, pid := range anchorPaths {
					if pid == uuid.Nil || !readySet[pid] {
						continue
					}
					if !hasCategoryMembership[pid] {
						unsortedIDs = append(unsortedIDs, pid)
					}
				}
				if len(unsortedIDs) < minCategoryPaths {
					continue
				}

				vecs := make([]pathVec, 0, len(unsortedIDs))
				for _, pid := range unsortedIDs {
					p := pathByID[pid]
					if p == nil {
						continue
					}
					emb, err := getEmb(p)
					if err != nil || len(emb) == 0 {
						continue
					}
					vecs = append(vecs, pathVec{Path: p, Vec: emb})
				}
				if len(vecs) < minCategoryPaths {
					continue
				}

				maxForAnchor := remainingBudget
				if maxForAnchor > 2 {
					maxForAnchor = 2
				}
				k := chooseK(len(vecs), maxForAnchor)
				clusters := kmeans(vecs, k)
				sort.Slice(clusters, func(i, j int) bool { return len(clusters[i].Members) > len(clusters[j].Members) })

				for _, cl := range clusters {
					if remainingBudget <= 0 {
						break
					}
					if len(cl.Members) < minCategoryPaths {
						continue
					}
					if clusterCohesion(cl) < clusterCohesionThreshold {
						continue
					}
					clientID := "anchor_cluster_" + uuid.New().String()
					clusterByClient[clientID] = pendingCluster{
						ParentNodeID:   a.ID,
						ParentNodeKey:  strings.TrimSpace(a.Key),
						ParentNodeName: strings.TrimSpace(a.Name),
						Cluster:        cl,
					}

					prop := refineProposedNode{
						ClientID:       clientID,
						ParentNodeID:   a.ID.String(),
						ParentNodeKey:  strings.TrimSpace(a.Key),
						ParentNodeName: strings.TrimSpace(a.Name),
						PathCount:      len(cl.Members),
					}
					samples := sampleClusterPaths(cl, 3)
					prop.SamplePaths = make([]struct {
						PathID      string `json:"path_id"`
						Title       string `json:"title"`
						Description string `json:"description"`
					}, 0, len(samples))
					for _, sp := range samples {
						prop.SamplePaths = append(prop.SamplePaths, struct {
							PathID      string `json:"path_id"`
							Title       string `json:"title"`
							Description string `json:"description"`
						}{
							PathID:      sp.Path.ID.String(),
							Title:       strings.TrimSpace(sp.Path.Title),
							Description: strings.TrimSpace(sp.Path.Description),
						})
					}
					proposed = append(proposed, prop)
					remainingBudget--
				}
			}

			if len(proposed) > 0 {
				existingNodes := make([]map[string]any, 0, len(allNonRootNodes))
				for _, n := range allNonRootNodes {
					if n == nil || n.ID == uuid.Nil {
						continue
					}
					existingNodes = append(existingNodes, map[string]any{
						"id":          n.ID.String(),
						"key":         strings.TrimSpace(n.Key),
						"kind":        strings.TrimSpace(n.Kind),
						"name":        strings.TrimSpace(n.Name),
						"description": strings.TrimSpace(n.Description),
					})
				}

				constraints := map[string]any{
					"max_new_nodes":         maxNewNodes,
					"min_category_paths":    minCategoryPaths,
					"existing_nodes":        existingNodes,
					"avoid_duplicate_names": true,
					"seeded_anchors_only":   true,
				}

				prompt, err := prompts.Build(prompts.PromptLibraryTaxonomyRefine, prompts.Input{
					TaxonomyFacet:           facet,
					TaxonomyCandidatesJSON:  string(toJSON(proposed)),
					TaxonomyConstraintsJSON: string(toJSON(constraints)),
				})
				if err != nil {
					return out, err
				}
				obj, err := deps.AI.GenerateJSON(ctx, prompt.System, prompt.User, prompt.SchemaName, prompt.Schema)
				if err != nil {
					return out, err
				}
				raw, _ := json.Marshal(obj)
				var modelOut refineModelOut
				if err := json.Unmarshal(raw, &modelOut); err != nil {
					return out, fmt.Errorf("library_taxonomy_refine: invalid model output: %w", err)
				}

				createdNodes, createdEdges, createdMembers, reassigned := 0, 0, 0, 0
				for _, d := range modelOut.Decisions {
					if !d.ShouldCreate {
						continue
					}
					clientID := strings.TrimSpace(d.ClientID)
					pc, ok := clusterByClient[clientID]
					if !ok {
						continue
					}
					if len(pc.Cluster.Members) < minCategoryPaths {
						continue
					}
					name := strings.TrimSpace(d.Name)
					desc := strings.TrimSpace(d.Description)
					if name == "" {
						continue
					}

					newID := uuid.New()
					stats := map[string]any{"member_count": len(pc.Cluster.Members)}
					node := &types.LibraryTaxonomyNode{
						ID:          newID,
						UserID:      in.UserID,
						Facet:       facet,
						Key:         "cat_" + newID.String(),
						Kind:        "category",
						Name:        name,
						Description: desc,
						Embedding:   datatypes.JSON(toJSON(pc.Cluster.Centroid)),
						Stats:       datatypes.JSON(toJSON(stats)),
						Version:     1,
						CreatedAt:   now,
						UpdatedAt:   now,
					}
					if err := deps.TaxNodes.UpsertMany(dbctx.Context{Ctx: ctx}, []*types.LibraryTaxonomyNode{node}); err != nil {
						return out, err
					}
					createdNodes++

					edge := &types.LibraryTaxonomyEdge{
						ID:         uuid.New(),
						UserID:     in.UserID,
						Facet:      facet,
						Kind:       "subsumes",
						FromNodeID: pc.ParentNodeID,
						ToNodeID:   node.ID,
						Weight:     1,
						Metadata:   datatypes.JSON(toJSON(map[string]any{"reason": "earned_bottom_up"})),
						Version:    1,
						CreatedAt:  now,
						UpdatedAt:  now,
					}
					if err := deps.TaxEdges.UpsertMany(dbctx.Context{Ctx: ctx}, []*types.LibraryTaxonomyEdge{edge}); err != nil {
						return out, err
					}
					createdEdges++

					pathIDs := make([]uuid.UUID, 0, len(pc.Cluster.Members))
					members := make([]*types.LibraryTaxonomyMembership, 0, len(pc.Cluster.Members))
					for _, pv := range pc.Cluster.Members {
						if pv.Path == nil || pv.Path.ID == uuid.Nil {
							continue
						}
						pathIDs = append(pathIDs, pv.Path.ID)
						w := cosineSimilarity(pv.Vec, pc.Cluster.Centroid)
						members = append(members, &types.LibraryTaxonomyMembership{
							ID:         uuid.New(),
							UserID:     in.UserID,
							Facet:      facet,
							PathID:     pv.Path.ID,
							NodeID:     node.ID,
							Weight:     clamp01(w),
							AssignedBy: "refine",
							Metadata:   datatypes.JSON(toJSON(map[string]any{"reason": "clustered_under_anchor", "parent": pc.ParentNodeKey})),
							Version:    1,
							CreatedAt:  now,
							UpdatedAt:  now,
						})
					}
					if len(members) > 0 {
						if err := deps.Membership.UpsertMany(dbctx.Context{Ctx: ctx}, members); err != nil {
							return out, err
						}
						createdMembers += len(members)
					}
					if len(pathIDs) > 0 {
						deleteMembershipsForNodeAndPaths(ctx, deps, in.UserID, facet, inbox.ID, pathIDs)
						reassigned += len(pathIDs)
					}
				}

				out.NodesCreated += createdNodes
				out.EdgesUpserted += createdEdges
				out.MembersUpserted += createdMembers
				out.PathsReassigned += reassigned
			}
		}

		// Phase 2b: refresh centroids + member_count stats for all category nodes from current memberships.
		{
			nodesNow, err := deps.TaxNodes.GetByUserFacet(dbctx.Context{Ctx: ctx}, in.UserID, facet)
			if err != nil {
				return out, err
			}
			memsNow, err := deps.Membership.GetByUserFacet(dbctx.Context{Ctx: ctx}, in.UserID, facet)
			if err != nil {
				return out, err
			}

			pathIDsByNode := map[uuid.UUID][]uuid.UUID{}
			for _, m := range memsNow {
				if m == nil || m.NodeID == uuid.Nil || m.PathID == uuid.Nil {
					continue
				}
				if m.NodeID == root.ID || m.NodeID == inbox.ID {
					continue
				}
				pathIDsByNode[m.NodeID] = append(pathIDsByNode[m.NodeID], m.PathID)
			}

			toUpdate := make([]*types.LibraryTaxonomyNode, 0, len(nodesNow))
			for _, n := range nodesNow {
				if n == nil || n.ID == uuid.Nil || n.ID == root.ID || n.ID == inbox.ID {
					continue
				}
				if strings.EqualFold(strings.TrimSpace(n.Kind), "anchor") {
					// Anchor embeddings are derived from their definitions (stable); do not drift them.
					continue
				}
				ids := dedupeUUIDs(pathIDsByNode[n.ID])
				if len(ids) == 0 {
					// Keep node as-is; avoid churn.
					continue
				}
				vecs := make([][]float32, 0, len(ids))
				for _, pid := range ids {
					p := pathByID[pid]
					if p == nil {
						continue
					}
					emb, err := getEmb(p)
					if err != nil || len(emb) == 0 {
						continue
					}
					vecs = append(vecs, emb)
				}
				stats := setIntStat(n.Stats, "member_count", len(ids))
				if mean, ok := meanVector(vecs); ok && len(mean) > 0 {
					cent := normalizeUnit(mean)
					n.Embedding = datatypes.JSON(toJSON(cent))
					var sum float64
					var cnt int
					for _, v := range vecs {
						if len(v) == 0 {
							continue
						}
						sum += cosineSimilarity(v, cent)
						cnt++
					}
					if cnt > 0 {
						stats["cohesion"] = clamp01(sum / float64(cnt))
					}
				}
				n.Stats = datatypes.JSON(toJSON(stats))
				n.UpdatedAt = now
				toUpdate = append(toUpdate, n)
			}
			if len(toUpdate) > 0 {
				_ = deps.TaxNodes.UpsertMany(dbctx.Context{Ctx: ctx}, toUpdate)
			}
		}

		// Phase 3: refresh related edges across existing nodes (top-N per node).
		if maxRelatedPerNode > 0 {
			nodesForRelated, err := deps.TaxNodes.GetByUserFacet(dbctx.Context{Ctx: ctx}, in.UserID, facet)
			if err != nil {
				return out, err
			}
			relNodes := make([]*types.LibraryTaxonomyNode, 0, len(nodesForRelated))
			for _, n := range nodesForRelated {
				if n == nil || n.ID == uuid.Nil {
					continue
				}
				k := strings.ToLower(strings.TrimSpace(n.Kind))
				if k == "root" || k == "inbox" {
					continue
				}
				if emb, ok := parseFloat32ArrayJSON(n.Embedding); ok && len(emb) > 0 {
					n.Embedding = datatypes.JSON(toJSON(normalizeUnit(emb)))
				}
				relNodes = append(relNodes, n)
			}
			if len(relNodes) > 1 {
				edges, err := buildRelatedEdges(in.UserID, facet, root.ID, inbox.ID, relNodes, maxRelatedPerNode, relatedThreshold)
				if err != nil {
					return out, err
				}
				if len(edges) > 0 {
					if err := deps.TaxEdges.UpsertMany(dbctx.Context{Ctx: ctx}, edges); err != nil {
						return out, err
					}
					out.EdgesUpserted += len(edges)
				}
			}
		}

		out.FacetsProcessed++
	}

	_ = deps.State.UpdateFields(dbctx.Context{Ctx: ctx}, in.UserID, map[string]interface{}{
		"dirty":                  false,
		"new_paths_since_refine": 0,
		"pending_unsorted_paths": 0,
		"last_refined_at":        &now,
		"refine_lock_until":      nil,
	})

	_ = BuildAndPersistLibraryTaxonomySnapshot(ctx, deps, in.UserID)
	return out, nil
}

func clusterCohesion(cl kCluster) float64 {
	if len(cl.Members) == 0 || len(cl.Centroid) == 0 {
		return 0
	}
	var sum float64
	for _, m := range cl.Members {
		if len(m.Vec) == 0 {
			continue
		}
		sum += cosineSimilarity(m.Vec, cl.Centroid)
	}
	return sum / float64(len(cl.Members))
}

func deleteMembershipsForNodeAndPaths(ctx context.Context, deps LibraryTaxonomyRouteDeps, userID uuid.UUID, facet string, nodeID uuid.UUID, pathIDs []uuid.UUID) {
	if deps.DB == nil || userID == uuid.Nil || nodeID == uuid.Nil || strings.TrimSpace(facet) == "" || len(pathIDs) == 0 {
		return
	}
	ids := dedupeUUIDs(pathIDs)
	if len(ids) == 0 {
		return
	}
	_ = deps.DB.WithContext(ctx).
		Where("user_id = ? AND facet = ? AND node_id = ? AND path_id IN ?", userID, facet, nodeID, ids).
		Delete(&types.LibraryTaxonomyMembership{}).Error
}

func chooseK(n, maxNewNodes int) int {
	if n <= 1 {
		return 1
	}
	k := int(math.Round(math.Sqrt(float64(n))))
	if k < 2 {
		k = 2
	}
	if maxNewNodes > 0 && k > maxNewNodes {
		k = maxNewNodes
	}
	if k > n {
		k = n
	}
	return k
}

func normalizeUnit(v []float32) []float32 {
	if len(v) == 0 {
		return v
	}
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum <= 0 {
		return v
	}
	den := float32(1.0 / math.Sqrt(sum))
	out := make([]float32, len(v))
	for i := range v {
		out[i] = v[i] * den
	}
	return out
}

func kmeans(vecs []pathVec, k int) []kCluster {
	if len(vecs) == 0 {
		return nil
	}
	if k < 1 {
		k = 1
	}
	if k > len(vecs) {
		k = len(vecs)
	}

	// Deterministic k-means++: start with first vector, then pick farthest each time.
	centroids := make([][]float32, 0, k)
	centroids = append(centroids, vecs[0].Vec)
	for len(centroids) < k {
		bestIdx := 0
		bestDist := -1.0
		for i := range vecs {
			d := 1.0
			for _, c := range centroids {
				s := cosineSimilarity(vecs[i].Vec, c)
				dist := 1.0 - s
				if dist < d {
					d = dist
				}
			}
			if d > bestDist {
				bestDist = d
				bestIdx = i
			}
		}
		centroids = append(centroids, vecs[bestIdx].Vec)
	}

	assign := make([]int, len(vecs))
	for i := range assign {
		assign[i] = -1
	}

	for iter := 0; iter < 10; iter++ {
		changed := false
		clusters := make([]kCluster, k)
		for i := range clusters {
			clusters[i].Centroid = centroids[i]
		}

		for i, pv := range vecs {
			best := 0
			bestScore := -1.0
			for c := 0; c < k; c++ {
				s := cosineSimilarity(pv.Vec, centroids[c])
				if s > bestScore {
					bestScore = s
					best = c
				}
			}
			if assign[i] != best {
				assign[i] = best
				changed = true
			}
			clusters[best].Members = append(clusters[best].Members, pv)
		}

		for i := 0; i < k; i++ {
			if len(clusters[i].Members) == 0 {
				continue
			}
			tmp := make([][]float32, 0, len(clusters[i].Members))
			for _, m := range clusters[i].Members {
				tmp = append(tmp, m.Vec)
			}
			if mean, ok := meanVector(tmp); ok && len(mean) > 0 {
				centroids[i] = normalizeUnit(mean)
				clusters[i].Centroid = centroids[i]
			}
		}

		if !changed {
			return clusters
		}
	}

	final := make([]kCluster, k)
	for i := range final {
		final[i].Centroid = centroids[i]
	}
	for i, pv := range vecs {
		if assign[i] < 0 || assign[i] >= k {
			assign[i] = 0
		}
		final[assign[i]].Members = append(final[assign[i]].Members, pv)
	}
	// Drop empties
	out := make([]kCluster, 0, len(final))
	for _, c := range final {
		if len(c.Members) == 0 {
			continue
		}
		out = append(out, c)
	}
	return out
}

func sampleClusterPaths(cl kCluster, n int) []pathVec {
	if n <= 0 || len(cl.Members) <= n {
		return cl.Members
	}
	type scored struct {
		p pathVec
		s float64
	}
	sc := make([]scored, 0, len(cl.Members))
	for _, m := range cl.Members {
		sc = append(sc, scored{p: m, s: cosineSimilarity(m.Vec, cl.Centroid)})
	}
	sort.Slice(sc, func(i, j int) bool { return sc[i].s > sc[j].s })
	out := make([]pathVec, 0, n)
	for i := 0; i < len(sc) && len(out) < n; i++ {
		out = append(out, sc[i].p)
	}
	return out
}

func chooseParents(rootID, inboxID, newNodeID uuid.UUID, centroid []float32, existing []*types.LibraryTaxonomyNode, maxParents int, threshold float64) []uuid.UUID {
	parents := []uuid.UUID{rootID}
	type scored struct {
		id uuid.UUID
		s  float64
	}
	sc := make([]scored, 0, len(existing))
	for _, n := range existing {
		if n == nil || n.ID == uuid.Nil || n.ID == rootID || n.ID == inboxID || n.ID == newNodeID {
			continue
		}
		emb, ok := parseFloat32ArrayJSON(n.Embedding)
		if !ok || len(emb) == 0 {
			continue
		}
		s := cosineSimilarity(centroid, emb)
		if s >= threshold {
			sc = append(sc, scored{id: n.ID, s: s})
		}
	}
	sort.Slice(sc, func(i, j int) bool { return sc[i].s > sc[j].s })
	for i := 0; i < len(sc) && len(parents) < 1+maxParents; i++ {
		parents = append(parents, sc[i].id)
	}
	return dedupeUUIDs(parents)
}

func chooseRelated(rootID, inboxID, newNodeID uuid.UUID, centroid []float32, existing []*types.LibraryTaxonomyNode, maxRelated int, threshold float64) []uuid.UUID {
	type scored struct {
		id uuid.UUID
		s  float64
	}
	sc := make([]scored, 0, len(existing))
	for _, n := range existing {
		if n == nil || n.ID == uuid.Nil || n.ID == rootID || n.ID == inboxID || n.ID == newNodeID {
			continue
		}
		emb, ok := parseFloat32ArrayJSON(n.Embedding)
		if !ok || len(emb) == 0 {
			continue
		}
		s := cosineSimilarity(centroid, emb)
		if s >= threshold {
			sc = append(sc, scored{id: n.ID, s: s})
		}
	}
	sort.Slice(sc, func(i, j int) bool { return sc[i].s > sc[j].s })
	ids := make([]uuid.UUID, 0, maxRelated)
	for i := 0; i < len(sc) && len(ids) < maxRelated; i++ {
		ids = append(ids, sc[i].id)
	}
	return dedupeUUIDs(ids)
}

func mustNodeVec(id uuid.UUID, nodes []*types.LibraryTaxonomyNode) []float32 {
	for _, n := range nodes {
		if n == nil || n.ID != id {
			continue
		}
		if v, ok := parseFloat32ArrayJSON(n.Embedding); ok && len(v) > 0 {
			return v
		}
	}
	return nil
}

func buildRelatedEdges(userID uuid.UUID, facet string, rootID, inboxID uuid.UUID, nodes []*types.LibraryTaxonomyNode, maxPerNode int, threshold float64) ([]*types.LibraryTaxonomyEdge, error) {
	if userID == uuid.Nil || facet == "" || maxPerNode <= 0 {
		return nil, nil
	}

	type scored struct {
		id uuid.UUID
		s  float64
	}

	vecByID := map[uuid.UUID][]float32{}
	for _, n := range nodes {
		if n == nil || n.ID == uuid.Nil || n.ID == rootID || n.ID == inboxID {
			continue
		}
		v, ok := parseFloat32ArrayJSON(n.Embedding)
		if !ok || len(v) == 0 {
			continue
		}
		vecByID[n.ID] = normalizeUnit(v)
	}

	ids := make([]uuid.UUID, 0, len(vecByID))
	for id := range vecByID {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })

	now := time.Now().UTC()
	edges := make([]*types.LibraryTaxonomyEdge, 0, len(ids)*maxPerNode*2)
	for _, from := range ids {
		fromVec := vecByID[from]
		if len(fromVec) == 0 {
			continue
		}
		sc := make([]scored, 0, len(ids))
		for _, to := range ids {
			if to == from {
				continue
			}
			s := cosineSimilarity(fromVec, vecByID[to])
			if s >= threshold {
				sc = append(sc, scored{id: to, s: s})
			}
		}
		sort.Slice(sc, func(i, j int) bool { return sc[i].s > sc[j].s })
		if len(sc) > maxPerNode {
			sc = sc[:maxPerNode]
		}
		for _, it := range sc {
			edges = append(edges, &types.LibraryTaxonomyEdge{
				ID:         uuid.New(),
				UserID:     userID,
				Facet:      facet,
				Kind:       "related",
				FromNodeID: from,
				ToNodeID:   it.id,
				Weight:     clamp01(it.s),
				Metadata:   datatypes.JSON(toJSON(map[string]any{"reason": "pairwise_similarity"})),
				Version:    1,
				CreatedAt:  now,
				UpdatedAt:  now,
			})
		}
	}
	return edges, nil
}

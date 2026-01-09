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

const snapshotMaxPathsPerNode = 12

type taxonomySnapshotV1 struct {
	Version     int                            `json:"version"`
	GeneratedAt string                         `json:"generated_at"`
	UserID      string                         `json:"user_id"`
	Facets      map[string]taxonomyFacetSnapV1 `json:"facets"`
}

type taxonomyFacetSnapV1 struct {
	Facet       string                      `json:"facet"`
	Title       string                      `json:"title"`
	RootNodeID  string                      `json:"root_node_id"`
	InboxNodeID string                      `json:"inbox_node_id"`
	Nodes       []taxonomyNodeSnapV1        `json:"nodes"`
	Edges       []taxonomyEdgeSnapV1        `json:"edges"`
	Memberships []taxonomyNodeMembersSnapV1 `json:"memberships"`
}

type taxonomyNodeSnapV1 struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MemberCount int    `json:"member_count"`
}

type taxonomyEdgeSnapV1 struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"`
	FromNodeID string  `json:"from_node_id"`
	ToNodeID   string  `json:"to_node_id"`
	Weight     float64 `json:"weight"`
}

type taxonomyNodeMembersSnapV1 struct {
	NodeID string                 `json:"node_id"`
	Paths  []taxonomyPathMemberV1 `json:"paths"`
}

type taxonomyPathMemberV1 struct {
	PathID string  `json:"path_id"`
	Weight float64 `json:"weight"`
}

func BuildAndPersistLibraryTaxonomySnapshot(ctx context.Context, deps LibraryTaxonomyRouteDeps, userID uuid.UUID) error {
	if userID == uuid.Nil {
		return fmt.Errorf("library_taxonomy_snapshot: missing user_id")
	}
	if deps.Snapshots == nil || deps.State == nil || deps.TaxNodes == nil || deps.TaxEdges == nil || deps.Membership == nil {
		return fmt.Errorf("library_taxonomy_snapshot: missing deps")
	}

	now := time.Now().UTC()
	out := taxonomySnapshotV1{
		Version:     1,
		GeneratedAt: now.Format(time.RFC3339Nano),
		UserID:      userID.String(),
		Facets:      map[string]taxonomyFacetSnapV1{},
	}

	for _, facet := range defaultTaxonomyFacets {
		facet = normalizeFacet(facet)
		if facet == "" {
			continue
		}

		root, inbox, err := ensureFacetDefaults(ctx, deps, userID, facet)
		if err != nil {
			return err
		}
		if root == nil || inbox == nil {
			return fmt.Errorf("library_taxonomy_snapshot: facet defaults missing")
		}
		_, _ = ensureTopicAnchors(ctx, deps, userID, facet, root)

		nodes, err := deps.TaxNodes.GetByUserFacet(dbctx.Context{Ctx: ctx}, userID, facet)
		if err != nil {
			return err
		}
		edges, err := deps.TaxEdges.GetByUserFacet(dbctx.Context{Ctx: ctx}, userID, facet)
		if err != nil {
			return err
		}
		mems, err := deps.Membership.GetByUserFacet(dbctx.Context{Ctx: ctx}, userID, facet)
		if err != nil {
			return err
		}

		memByNode := map[uuid.UUID][]*types.LibraryTaxonomyMembership{}
		for _, m := range mems {
			if m == nil || m.NodeID == uuid.Nil || m.PathID == uuid.Nil {
				continue
			}
			memByNode[m.NodeID] = append(memByNode[m.NodeID], m)
		}

		nodeSnaps := make([]taxonomyNodeSnapV1, 0, len(nodes))
		for _, n := range nodes {
			if n == nil || n.ID == uuid.Nil {
				continue
			}
			memberCount := intFromStats(n.Stats, "member_count", 0)
			if memberCount <= 0 {
				memberCount = len(memByNode[n.ID])
			}
			// Keep root/inbox even if empty; skip empty categories to keep the snapshot compact.
			kind := strings.TrimSpace(n.Kind)
			if !strings.EqualFold(kind, "root") && !strings.EqualFold(kind, "inbox") && memberCount == 0 {
				continue
			}
			nodeSnaps = append(nodeSnaps, taxonomyNodeSnapV1{
				ID:          n.ID.String(),
				Key:         strings.TrimSpace(n.Key),
				Kind:        kind,
				Name:        strings.TrimSpace(n.Name),
				Description: strings.TrimSpace(n.Description),
				MemberCount: memberCount,
			})
		}
		sort.Slice(nodeSnaps, func(i, j int) bool {
			// Root first, then inbox, then by member_count desc.
			ik := strings.ToLower(strings.TrimSpace(nodeSnaps[i].Kind))
			jk := strings.ToLower(strings.TrimSpace(nodeSnaps[j].Kind))
			if ik != jk {
				if ik == "root" {
					return true
				}
				if jk == "root" {
					return false
				}
				if ik == "inbox" {
					return true
				}
				if jk == "inbox" {
					return false
				}
			}
			if nodeSnaps[i].MemberCount != nodeSnaps[j].MemberCount {
				return nodeSnaps[i].MemberCount > nodeSnaps[j].MemberCount
			}
			return nodeSnaps[i].Name < nodeSnaps[j].Name
		})

		edgeSnaps := make([]taxonomyEdgeSnapV1, 0, len(edges))
		for _, e := range edges {
			if e == nil || e.ID == uuid.Nil || e.FromNodeID == uuid.Nil || e.ToNodeID == uuid.Nil {
				continue
			}
			edgeSnaps = append(edgeSnaps, taxonomyEdgeSnapV1{
				ID:         e.ID.String(),
				Kind:       strings.TrimSpace(e.Kind),
				FromNodeID: e.FromNodeID.String(),
				ToNodeID:   e.ToNodeID.String(),
				Weight:     e.Weight,
			})
		}

		nodeMembers := make([]taxonomyNodeMembersSnapV1, 0, len(memByNode))
		for nodeID, ms := range memByNode {
			if nodeID == uuid.Nil || len(ms) == 0 {
				continue
			}
			sort.Slice(ms, func(i, j int) bool {
				if ms[i] == nil || ms[j] == nil {
					return false
				}
				if ms[i].Weight != ms[j].Weight {
					return ms[i].Weight > ms[j].Weight
				}
				return ms[i].UpdatedAt.After(ms[j].UpdatedAt)
			})
			limit := snapshotMaxPathsPerNode
			if limit < 1 {
				limit = 1
			}
			if len(ms) < limit {
				limit = len(ms)
			}
			paths := make([]taxonomyPathMemberV1, 0, limit)
			for i := 0; i < limit; i++ {
				m := ms[i]
				if m == nil || m.PathID == uuid.Nil {
					continue
				}
				paths = append(paths, taxonomyPathMemberV1{
					PathID: m.PathID.String(),
					Weight: clamp01(m.Weight),
				})
			}
			if len(paths) == 0 {
				continue
			}
			nodeMembers = append(nodeMembers, taxonomyNodeMembersSnapV1{
				NodeID: nodeID.String(),
				Paths:  paths,
			})
		}
		sort.Slice(nodeMembers, func(i, j int) bool { return nodeMembers[i].NodeID < nodeMembers[j].NodeID })

		out.Facets[facet] = taxonomyFacetSnapV1{
			Facet:       facet,
			Title:       titleCaseFacet(facet),
			RootNodeID:  root.ID.String(),
			InboxNodeID: inbox.ID.String(),
			Nodes:       nodeSnaps,
			Edges:       edgeSnaps,
			Memberships: nodeMembers,
		}
	}

	existing, err := deps.Snapshots.GetByUserID(dbctx.Context{Ctx: ctx}, userID)
	if err != nil {
		return err
	}
	if existing != nil && existing.Version > 0 {
		out.Version = existing.Version + 1
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("library_taxonomy_snapshot: marshal: %w", err)
	}
	if err := deps.Snapshots.Upsert(dbctx.Context{Ctx: ctx}, &types.LibraryTaxonomySnapshot{
		ID:           uuid.New(),
		UserID:       userID,
		Version:      out.Version,
		SnapshotJSON: datatypes.JSON(raw),
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		return err
	}

	_ = deps.State.UpdateFields(dbctx.Context{Ctx: ctx}, userID, map[string]interface{}{
		"last_snapshot_built_at": &now,
	})

	return nil
}

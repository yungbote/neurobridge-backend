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
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/prompts"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/envutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
)

type LibraryTaxonomyRouteDeps struct {
	DB  *gorm.DB
	Log *logger.Logger

	AI openai.Client
	// Optional: sync the computed taxonomy into Neo4j for fast traversals.
	Graph *neo4jdb.Client

	Path        repos.PathRepo
	PathNodes   repos.PathNodeRepo
	Clusters    repos.ConceptClusterRepo
	TaxNodes    repos.LibraryTaxonomyNodeRepo
	TaxEdges    repos.LibraryTaxonomyEdgeRepo
	Membership  repos.LibraryTaxonomyMembershipRepo
	State       repos.LibraryTaxonomyStateRepo
	Snapshots   repos.LibraryTaxonomySnapshotRepo
	PathVectors repos.LibraryPathEmbeddingRepo
}

type LibraryTaxonomyRouteInput struct {
	PathID uuid.UUID `json:"path_id"`
	Force  bool      `json:"force,omitempty"`
}

type LibraryTaxonomyRouteOutput struct {
	UserID uuid.UUID `json:"user_id"`
	PathID uuid.UUID `json:"path_id"`

	FacetsProcessed int `json:"facets_processed"`
	NodesCreated    int `json:"nodes_created"`
	EdgesUpserted   int `json:"edges_upserted"`
	MembersUpserted int `json:"members_upserted"`

	AssignedToInboxFacets []string `json:"assigned_to_inbox_facets,omitempty"`

	ShouldEnqueueRefine bool `json:"should_enqueue_refine"`
}

type routeCandidateNode struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type routeCandidatesPayload struct {
	RootNodeID     string               `json:"root_node_id"`
	InboxNodeID    string               `json:"inbox_node_id"`
	MaxMemberships int                  `json:"max_memberships"`
	MaxNewNodes    int                  `json:"max_new_nodes"`
	ExistingNodes  []routeCandidateNode `json:"existing_nodes"`
}

type routePathSummary struct {
	PathID        string   `json:"path_id"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	NodeTitles    []string `json:"node_titles"`
	ClusterLabels []string `json:"cluster_labels"`
	ClusterTags   []string `json:"cluster_tags"`
}

type routeModelMembership struct {
	NodeID string  `json:"node_id"`
	Weight float64 `json:"weight"`
	Reason string  `json:"reason"`
}

type routeModelNewNode struct {
	ClientID         string   `json:"client_id"`
	Kind             string   `json:"kind"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	ParentNodeIDs    []string `json:"parent_node_ids"`
	RelatedNodeIDs   []string `json:"related_node_ids"`
	MembershipWeight float64  `json:"membership_weight"`
	Reason           string   `json:"reason"`
}

type routeModelOut struct {
	Version     int                    `json:"version"`
	Warnings    []string               `json:"warnings"`
	Diagnostics map[string]any         `json:"diagnostics"`
	Facet       string                 `json:"facet"`
	Memberships []routeModelMembership `json:"memberships"`
	NewNodes    []routeModelNewNode    `json:"new_nodes"`
}

func LibraryTaxonomyRoute(ctx context.Context, deps LibraryTaxonomyRouteDeps, in LibraryTaxonomyRouteInput) (LibraryTaxonomyRouteOutput, error) {
	out := LibraryTaxonomyRouteOutput{PathID: in.PathID}
	if deps.DB == nil || deps.Log == nil || deps.AI == nil || deps.Path == nil || deps.PathNodes == nil || deps.Clusters == nil || deps.TaxNodes == nil || deps.TaxEdges == nil || deps.Membership == nil || deps.State == nil || deps.Snapshots == nil || deps.PathVectors == nil {
		return out, fmt.Errorf("library_taxonomy_route: missing deps")
	}
	if in.PathID == uuid.Nil {
		return out, fmt.Errorf("library_taxonomy_route: missing path_id")
	}

	pathRow, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, in.PathID)
	if err != nil {
		return out, err
	}
	if pathRow == nil || pathRow.ID == uuid.Nil || pathRow.UserID == nil || *pathRow.UserID == uuid.Nil {
		return out, fmt.Errorf("library_taxonomy_route: path not found")
	}
	userID := *pathRow.UserID
	out.UserID = userID

	// Ensure per-user state exists.
	now := time.Now().UTC()
	st, err := deps.State.GetByUserID(dbctx.Context{Ctx: ctx}, userID)
	if err != nil {
		return out, err
	}
	if st == nil {
		st = &types.LibraryTaxonomyState{
			ID:        uuid.New(),
			UserID:    userID,
			Version:   1,
			Dirty:     true,
			CreatedAt: now,
			UpdatedAt: now,
		}
		_ = deps.State.Upsert(dbctx.Context{Ctx: ctx}, st)
	}

	// Compute (and cache) a path embedding once; reused across facets.
	pathEmb, err := getOrComputePathEmbedding(ctx, deps, userID, pathRow)
	if err != nil {
		return out, err
	}

	nodeRows, _ := deps.PathNodes.GetByPathIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{pathRow.ID})
	nodeTitles := make([]string, 0, len(nodeRows))
	for _, n := range nodeRows {
		if n == nil {
			continue
		}
		t := strings.TrimSpace(n.Title)
		if t != "" {
			nodeTitles = append(nodeTitles, t)
		}
	}
	if len(nodeTitles) > 16 {
		nodeTitles = nodeTitles[:16]
	}

	clusterLabels, clusterTags, err := getConceptClusterSignals(ctx, deps, pathRow.ID)
	if err != nil {
		return out, err
	}

	maxMemberships := envutil.Int("LIBRARY_TAXONOMY_MAX_MEMBERSHIPS_PER_FACET", 4)
	if maxMemberships < 1 {
		maxMemberships = 1
	}
	if maxMemberships > 8 {
		maxMemberships = 8
	}
	maxNewNodes := envutil.Int("LIBRARY_TAXONOMY_MAX_NEW_NODES_PER_FACET", 2)
	if maxNewNodes < 0 {
		maxNewNodes = 0
	}
	if maxNewNodes > 4 {
		maxNewNodes = 4
	}

	assignedInbox := make([]string, 0)
	for _, facet := range defaultTaxonomyFacets {
		facet = normalizeFacet(facet)
		if facet == "" {
			continue
		}

		maxNewNodesForFacet := maxNewNodes
		// For the topic facet we seed ultra-stable domain anchors and do not create mid/low-level
		// nodes during the routing step. Those must be earned bottom-up via refinement.
		if facet == "topic" {
			maxNewNodesForFacet = 0
		}

		constraints := map[string]any{
			"max_memberships":       maxMemberships,
			"max_new_nodes":         maxNewNodesForFacet,
			"prefer_existing":       true,
			"avoid_duplicate_names": true,
			"require_seeded_anchor": facet == "topic",
			"disallow_new_nodes":    maxNewNodesForFacet == 0,
		}

		// Idempotency: if this path already has memberships in this facet (and not forced), skip.
		if !in.Force {
			existing, err := deps.Membership.GetByUserFacetAndPathIDs(dbctx.Context{Ctx: ctx}, userID, facet, []uuid.UUID{pathRow.ID})
			if err == nil && len(existing) > 0 {
				out.FacetsProcessed++
				continue
			}
		}

		root, inbox, err := ensureFacetDefaults(ctx, deps, userID, facet)
		if err != nil {
			return out, err
		}
		if root == nil || inbox == nil {
			return out, fmt.Errorf("library_taxonomy_route: missing facet defaults")
		}

		anchors, err := ensureTopicAnchors(ctx, deps, userID, facet, root)
		if err != nil {
			return out, err
		}

		// Candidate selection: always include seeded anchors, then pick top-N other nodes by embedding similarity.
		allNodes, err := deps.TaxNodes.GetByUserFacet(dbctx.Context{Ctx: ctx}, userID, facet)
		if err != nil {
			return out, err
		}
		exclude := map[uuid.UUID]bool{root.ID: true, inbox.ID: true}
		anchorCandidates := make([]routeCandidateNode, 0, len(anchors))
		for _, a := range anchors {
			if a == nil || a.ID == uuid.Nil {
				continue
			}
			exclude[a.ID] = true
			anchorCandidates = append(anchorCandidates, routeCandidateNode{
				ID:          a.ID.String(),
				Kind:        strings.TrimSpace(a.Kind),
				Name:        strings.TrimSpace(a.Name),
				Description: strings.TrimSpace(a.Description),
			})
		}
		candidates := append(anchorCandidates, selectCandidateNodes(allNodes, root.ID, inbox.ID, pathEmb, 24, exclude)...)

		candPayload := routeCandidatesPayload{
			RootNodeID:     root.ID.String(),
			InboxNodeID:    inbox.ID.String(),
			MaxMemberships: maxMemberships,
			MaxNewNodes:    maxNewNodesForFacet,
			ExistingNodes:  candidates,
		}
		pathSummary := routePathSummary{
			PathID:        pathRow.ID.String(),
			Title:         strings.TrimSpace(pathRow.Title),
			Description:   strings.TrimSpace(pathRow.Description),
			NodeTitles:    nodeTitles,
			ClusterLabels: clusterLabels,
			ClusterTags:   clusterTags,
		}

		prompt, err := prompts.Build(prompts.PromptLibraryTaxonomyRoute, prompts.Input{
			TaxonomyFacet:           facet,
			TaxonomyCandidatesJSON:  string(toJSON(candPayload)),
			TaxonomyPathSummaryJSON: string(toJSON(pathSummary)),
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
		var modelOut routeModelOut
		if err := json.Unmarshal(raw, &modelOut); err != nil {
			return out, fmt.Errorf("library_taxonomy_route: invalid model output: %w", err)
		}

		nodesCreated, edgesUpserted, membersUpserted, usedInbox, err := applyRouteResult(ctx, deps, userID, facet, pathRow.ID, pathEmb, root, inbox, modelOut, maxMemberships, maxNewNodesForFacet)
		if err != nil {
			return out, err
		}
		out.NodesCreated += nodesCreated
		out.EdgesUpserted += edgesUpserted
		out.MembersUpserted += membersUpserted
		if usedInbox {
			assignedInbox = append(assignedInbox, facet)
		}

		out.FacetsProcessed++
	}
	out.AssignedToInboxFacets = assignedInbox

	// Build snapshot after route.
	if err := BuildAndPersistLibraryTaxonomySnapshot(ctx, deps, userID); err != nil {
		deps.Log.Warn("library_taxonomy_route snapshot failed (ignored)", "error", err, "user_id", userID.String())
	}

	// Update state + decide whether to enqueue refine.
	newPaths := st.NewPathsSinceRefine + 1
	pendingUnsorted := st.PendingUnsortedPaths
	if len(assignedInbox) > 0 {
		pendingUnsorted++
	}
	_ = deps.State.UpdateFields(dbctx.Context{Ctx: ctx}, userID, map[string]interface{}{
		"dirty":                  true,
		"new_paths_since_refine": newPaths,
		"pending_unsorted_paths": pendingUnsorted,
		"last_routed_at":         &now,
	})

	// Best-effort: project the updated taxonomy into Neo4j.
	if deps.Graph != nil {
		for _, facet := range defaultTaxonomyFacets {
			if err := syncLibraryTaxonomyFacetToNeo4j(ctx, deps, userID, facet); err != nil && deps.Log != nil {
				deps.Log.Warn("neo4j library taxonomy sync failed (continuing)", "error", err, "user_id", userID.String(), "facet", facet)
			}
		}
	}

	refineNewPathsThreshold := envutil.Int("LIBRARY_TAXONOMY_REFINE_NEW_PATHS_THRESHOLD", 5)
	if refineNewPathsThreshold < 1 {
		refineNewPathsThreshold = 1
	}
	refineUnsortedThreshold := envutil.Int("LIBRARY_TAXONOMY_REFINE_UNSORTED_THRESHOLD", 3)
	if refineUnsortedThreshold < 1 {
		refineUnsortedThreshold = 1
	}
	if newPaths >= refineNewPathsThreshold || pendingUnsorted >= refineUnsortedThreshold {
		out.ShouldEnqueueRefine = true
	}

	return out, nil
}

func selectCandidateNodes(nodes []*types.LibraryTaxonomyNode, rootID, inboxID uuid.UUID, pathEmb []float32, limit int, exclude map[uuid.UUID]bool) []routeCandidateNode {
	type scored struct {
		n     *types.LibraryTaxonomyNode
		score float64
	}
	scoredNodes := make([]scored, 0, len(nodes))
	for _, n := range nodes {
		if n == nil || n.ID == uuid.Nil || n.ID == rootID || n.ID == inboxID {
			continue
		}
		if exclude != nil && exclude[n.ID] {
			continue
		}
		emb, ok := parseFloat32ArrayJSON(n.Embedding)
		if !ok || len(emb) == 0 || len(pathEmb) == 0 {
			continue
		}
		scoredNodes = append(scoredNodes, scored{n: n, score: cosineSimilarity(pathEmb, emb)})
	}
	sort.Slice(scoredNodes, func(i, j int) bool { return scoredNodes[i].score > scoredNodes[j].score })
	if limit <= 0 {
		limit = 24
	}
	if len(scoredNodes) > limit {
		scoredNodes = scoredNodes[:limit]
	}
	out := make([]routeCandidateNode, 0, len(scoredNodes)+2)
	for _, s := range scoredNodes {
		out = append(out, routeCandidateNode{
			ID:          s.n.ID.String(),
			Kind:        strings.TrimSpace(s.n.Kind),
			Name:        strings.TrimSpace(s.n.Name),
			Description: strings.TrimSpace(s.n.Description),
		})
	}
	return out
}

func ensureFacetDefaults(ctx context.Context, deps LibraryTaxonomyRouteDeps, userID uuid.UUID, facet string) (*types.LibraryTaxonomyNode, *types.LibraryTaxonomyNode, error) {
	facet = normalizeFacet(facet)
	rows, err := deps.TaxNodes.GetByUserFacetKeys(dbctx.Context{Ctx: ctx}, userID, facet, []string{"root", "inbox"})
	if err != nil {
		return nil, nil, err
	}
	var root *types.LibraryTaxonomyNode
	var inbox *types.LibraryTaxonomyNode
	for _, r := range rows {
		if r == nil {
			continue
		}
		switch strings.TrimSpace(r.Key) {
		case "root":
			root = r
		case "inbox":
			inbox = r
		}
	}
	toUpsert := make([]*types.LibraryTaxonomyNode, 0, 2)
	now := time.Now().UTC()
	if root == nil {
		root = &types.LibraryTaxonomyNode{
			ID:          uuid.New(),
			UserID:      userID,
			Facet:       facet,
			Key:         "root",
			Kind:        "root",
			Name:        titleCaseFacet(facet),
			Description: "High-level organization for your library.",
			Embedding:   datatypes.JSON([]byte(`[]`)),
			Stats:       datatypes.JSON([]byte(`{}`)),
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		toUpsert = append(toUpsert, root)
	}
	if inbox == nil {
		inbox = &types.LibraryTaxonomyNode{
			ID:          uuid.New(),
			UserID:      userID,
			Facet:       facet,
			Key:         "inbox",
			Kind:        "inbox",
			Name:        "Unsorted",
			Description: "Needs organization.",
			Embedding:   datatypes.JSON([]byte(`[]`)),
			Stats:       datatypes.JSON([]byte(`{}`)),
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		toUpsert = append(toUpsert, inbox)
	}
	if len(toUpsert) > 0 {
		if err := deps.TaxNodes.UpsertMany(dbctx.Context{Ctx: ctx}, toUpsert); err != nil {
			return nil, nil, err
		}
	}
	return root, inbox, nil
}

func getConceptClusterSignals(ctx context.Context, deps LibraryTaxonomyRouteDeps, pathID uuid.UUID) ([]string, []string, error) {
	rows, err := deps.Clusters.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID)
	if err != nil {
		return nil, nil, err
	}
	labels := make([]string, 0, len(rows))
	tags := make([]string, 0, 32)
	for _, r := range rows {
		if r == nil {
			continue
		}
		if s := strings.TrimSpace(r.Label); s != "" {
			labels = append(labels, s)
		}
		var meta map[string]any
		_ = json.Unmarshal(r.Metadata, &meta)
		if meta == nil {
			continue
		}
		if arr, ok := meta["tags"].([]any); ok {
			for _, it := range arr {
				t := strings.TrimSpace(fmt.Sprint(it))
				if t != "" {
					tags = append(tags, t)
				}
			}
		}
	}
	labels = stableStrings(labels)
	tags = stableStrings(tags)
	if len(labels) > 12 {
		labels = labels[:12]
	}
	if len(tags) > 24 {
		tags = tags[:24]
	}
	return labels, tags, nil
}

func getOrComputePathEmbedding(ctx context.Context, deps LibraryTaxonomyRouteDeps, userID uuid.UUID, path *types.Path) ([]float32, error) {
	if path == nil || path.ID == uuid.Nil {
		return nil, fmt.Errorf("path required")
	}
	// Prefer deterministic embedding derived from concept clusters (cheap and stable).
	clusters, err := deps.Clusters.GetByScope(dbctx.Context{Ctx: ctx}, "path", &path.ID)
	if err != nil {
		return nil, err
	}
	sort.Slice(clusters, func(i, j int) bool {
		if clusters[i] == nil || clusters[j] == nil {
			return false
		}
		return clusters[i].ID.String() < clusters[j].ID.String()
	})

	hashParts := []string{path.ID.String(), strings.TrimSpace(path.Title), strings.TrimSpace(path.Description), path.UpdatedAt.Format(time.RFC3339Nano)}
	vecs := make([][]float32, 0, len(clusters))
	for _, c := range clusters {
		if c == nil {
			continue
		}
		hashParts = append(hashParts, c.ID.String(), c.UpdatedAt.Format(time.RFC3339Nano), strings.TrimSpace(c.VectorID))
		if emb, ok := parseFloat32ArrayJSON(c.Embedding); ok && len(emb) > 0 {
			vecs = append(vecs, emb)
		}
	}
	sourcesHash := hashStrings(hashParts...)

	// Check cache.
	if rows, err := deps.PathVectors.GetByUserAndPathIDs(dbctx.Context{Ctx: ctx}, userID, []uuid.UUID{path.ID}); err == nil && len(rows) > 0 && rows[0] != nil {
		row := rows[0]
		if strings.TrimSpace(row.SourcesHash) == sourcesHash {
			if emb, ok := parseFloat32ArrayJSON(row.Embedding); ok && len(emb) > 0 {
				return emb, nil
			}
		}
	}

	var out []float32
	var model string
	if mean, ok := meanVector(vecs); ok && len(mean) > 0 {
		out = mean
		model = "concept_cluster_avg@1"
	} else {
		// Fallback: embed path title + description.
		doc := strings.TrimSpace(path.Title + "\n" + path.Description)
		embs, err := deps.AI.Embed(ctx, []string{doc})
		if err != nil || len(embs) == 0 {
			return nil, fmt.Errorf("embed path: %w", err)
		}
		out = embs[0]
		embedModel := strings.TrimSpace(os.Getenv("OPENAI_EMBED_MODEL"))
		if embedModel == "" {
			embedModel = "text-embedding-3-small"
		}
		model = "openai:" + embedModel
	}

	// Persist cache.
	_ = deps.PathVectors.UpsertMany(dbctx.Context{Ctx: ctx}, []*types.LibraryPathEmbedding{
		{
			ID:          uuid.New(),
			UserID:      userID,
			PathID:      path.ID,
			Model:       model,
			Embedding:   datatypes.JSON(toJSON(out)),
			SourcesHash: sourcesHash,
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		},
	})
	return out, nil
}

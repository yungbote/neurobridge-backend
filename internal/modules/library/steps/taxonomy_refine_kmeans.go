package steps

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

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

package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/clients/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/learning/keys"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type ChainSignatureBuildDeps struct {
	DB        *gorm.DB
	Log       *logger.Logger
	Concepts  repos.ConceptRepo
	Clusters  repos.ConceptClusterRepo
	Members   repos.ConceptClusterMemberRepo
	Edges     repos.ConceptEdgeRepo
	Chains    repos.ChainSignatureRepo
	AI        openai.Client
	Vec       pc.VectorStore
	Saga      services.SagaService
	Bootstrap services.LearningBuildBootstrapService
}

type ChainSignatureBuildInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
}

type ChainSignatureBuildOutput struct {
	PathID          uuid.UUID `json:"path_id"`
	ChainsUpserted  int       `json:"chains_upserted"`
	PineconeBatches int       `json:"pinecone_batches"`
}

func ChainSignatureBuild(ctx context.Context, deps ChainSignatureBuildDeps, in ChainSignatureBuildInput) (ChainSignatureBuildOutput, error) {
	out := ChainSignatureBuildOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Concepts == nil || deps.Clusters == nil || deps.Members == nil || deps.Edges == nil || deps.Chains == nil || deps.AI == nil || deps.Bootstrap == nil || deps.Saga == nil {
		return out, fmt.Errorf("chain_signature_build: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("chain_signature_build: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("chain_signature_build: missing material_set_id")
	}
	if in.SagaID == uuid.Nil {
		return out, fmt.Errorf("chain_signature_build: missing saga_id")
	}

	pathID, err := deps.Bootstrap.EnsurePath(dbctx.Context{Ctx: ctx}, in.OwnerUserID, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	concepts, err := deps.Concepts.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID)
	if err != nil {
		return out, err
	}
	if len(concepts) == 0 {
		return out, fmt.Errorf("chain_signature_build: no concepts for path")
	}
	conceptIDToKey := map[uuid.UUID]string{}
	conceptKeyToName := map[string]string{}
	allKeys := make([]string, 0, len(concepts))
	for _, c := range concepts {
		if c == nil {
			continue
		}
		conceptIDToKey[c.ID] = c.Key
		conceptKeyToName[c.Key] = c.Name
		allKeys = append(allKeys, c.Key)
	}
	allKeys = dedupeStrings(allKeys)
	sort.Strings(allKeys)

	// Candidate chains: one per cluster, or one global chain if no clusters.
	clusters, err := deps.Clusters.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID)
	if err != nil {
		return out, err
	}

	type chainCandidate struct {
		Label       string
		ConceptKeys []string
		Edges       []keys.ChainEdge
		ChainKey    string
		VectorID    string
		Doc         string
		Emb         []float32
	}
	cands := make([]chainCandidate, 0)

	if len(clusters) == 0 {
		edges := chainEdgesForConceptKeys(dbctx.Context{Ctx: ctx}, deps.Edges, conceptIDToKey, allKeys)
		ck := keys.ChainKey(allKeys, edges)
		cands = append(cands, chainCandidate{
			Label:       "global",
			ConceptKeys: allKeys,
			Edges:       edges,
			ChainKey:    ck,
			VectorID:    "chain:" + ck,
		})
	} else {
		clusterIDs := make([]uuid.UUID, 0, len(clusters))
		clusterLabel := map[uuid.UUID]string{}
		for _, cl := range clusters {
			if cl == nil || cl.ID == uuid.Nil {
				continue
			}
			clusterIDs = append(clusterIDs, cl.ID)
			clusterLabel[cl.ID] = cl.Label
		}
		members, err := deps.Members.GetByClusterIDs(dbctx.Context{Ctx: ctx}, clusterIDs)
		if err != nil {
			return out, err
		}
		byCluster := map[uuid.UUID][]uuid.UUID{}
		for _, m := range members {
			if m == nil || m.ClusterID == uuid.Nil || m.ConceptID == uuid.Nil {
				continue
			}
			byCluster[m.ClusterID] = append(byCluster[m.ClusterID], m.ConceptID)
		}
		for cid, mids := range byCluster {
			keysArr := make([]string, 0, len(mids))
			for _, mid := range mids {
				if k := strings.TrimSpace(conceptIDToKey[mid]); k != "" {
					keysArr = append(keysArr, k)
				}
			}
			keysArr = dedupeStrings(keysArr)
			sort.Strings(keysArr)
			if len(keysArr) == 0 {
				continue
			}
			edges := chainEdgesForConceptKeys(dbctx.Context{Ctx: ctx}, deps.Edges, conceptIDToKey, keysArr)
			ck := keys.ChainKey(keysArr, edges)
			cands = append(cands, chainCandidate{
				Label:       strings.TrimSpace(clusterLabel[cid]),
				ConceptKeys: keysArr,
				Edges:       edges,
				ChainKey:    ck,
				VectorID:    "chain:" + ck,
			})
		}
	}

	if len(cands) == 0 {
		return out, fmt.Errorf("chain_signature_build: no chain candidates")
	}

	// Stable ordering for deterministic batch behavior.
	sort.Slice(cands, func(i, j int) bool { return cands[i].VectorID < cands[j].VectorID })

	docs := make([]string, 0, len(cands))
	for i := range cands {
		label := strings.TrimSpace(cands[i].Label)
		if label == "" {
			label = "chain"
		}
		names := make([]string, 0, len(cands[i].ConceptKeys))
		for _, k := range cands[i].ConceptKeys {
			if nm := strings.TrimSpace(conceptKeyToName[k]); nm != "" {
				names = append(names, nm)
			}
		}
		doc := strings.TrimSpace(label + "\nConcepts: " + strings.Join(names, ", "))
		if doc == "" {
			doc = "chain " + cands[i].VectorID
		}
		cands[i].Doc = doc
		docs = append(docs, doc)
	}

	embs, err := deps.AI.Embed(ctx, docs)
	if err != nil {
		return out, err
	}
	if len(embs) != len(cands) {
		return out, fmt.Errorf("chain_signature_build: embedding count mismatch (got %d want %d)", len(embs), len(cands))
	}
	for i := range cands {
		cands[i].Emb = embs[i]
	}

	ns := index.ChainsNamespace("path", &pathID)
	const batchSize = 64

	if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		if _, err := deps.Bootstrap.EnsurePath(dbc, in.OwnerUserID, in.MaterialSetID); err != nil {
			return err
		}

		for _, c := range cands {
			edgesJSON, _ := json.Marshal(map[string]any{"edges": c.Edges})
			row := &types.ChainSignature{
				ID:          uuid.New(),
				ChainKey:    c.ChainKey,
				Scope:       "path",
				ScopeID:     &pathID,
				ConceptKeys: datatypes.JSON(mustJSON(c.ConceptKeys)),
				EdgesJSON:   datatypes.JSON(edgesJSON),
				ChainDoc:    c.Doc,
				Embedding:   datatypes.JSON(mustJSON(c.Emb)),
				VectorID:    c.VectorID,
				Metadata:    datatypes.JSON(mustJSON(map[string]any{"label": c.Label})),
			}
			if err := deps.Chains.UpsertByChainKey(dbc, row); err != nil {
				return err
			}
			out.ChainsUpserted++
		}

		if deps.Vec != nil {
			for start := 0; start < len(cands); start += batchSize {
				end := start + batchSize
				if end > len(cands) {
					end = len(cands)
				}
				ids := make([]string, 0, end-start)
				for _, c := range cands[start:end] {
					ids = append(ids, c.VectorID)
				}
				if len(ids) == 0 {
					continue
				}
				if err := deps.Saga.AppendAction(dbc, in.SagaID, services.SagaActionKindPineconeDeleteIDs, map[string]any{
					"namespace": ns,
					"ids":       ids,
				}); err != nil {
					return err
				}
			}
		}

		return nil
	}); err != nil {
		return out, err
	}

	if deps.Vec != nil {
		for start := 0; start < len(cands); start += batchSize {
			end := start + batchSize
			if end > len(cands) {
				end = len(cands)
			}
			pv := make([]pc.Vector, 0, end-start)
			for _, c := range cands[start:end] {
				if len(c.Emb) == 0 {
					continue
				}
				pv = append(pv, pc.Vector{
					ID:     c.VectorID,
					Values: c.Emb,
					Metadata: map[string]any{
						"type":      "chain",
						"chain_key": c.ChainKey,
						"path_id":   pathID.String(),
					},
				})
			}
			if len(pv) == 0 {
				continue
			}
			if err := deps.Vec.Upsert(ctx, ns, pv); err != nil {
				deps.Log.Warn("pinecone upsert failed (continuing)", "namespace", ns, "err", err.Error())
				break
			}
			out.PineconeBatches++
		}
	}

	return out, nil
}

func chainEdgesForConceptKeys(dbc dbctx.Context, edgeRepo repos.ConceptEdgeRepo, idToKey map[uuid.UUID]string, conceptKeys []string) []keys.ChainEdge {
	if edgeRepo == nil || len(conceptKeys) == 0 {
		return nil
	}

	// Map concept keys back to IDs to query edges.
	keySet := map[string]bool{}
	for _, k := range conceptKeys {
		keySet[k] = true
	}
	conceptIDs := make([]uuid.UUID, 0, len(conceptKeys))
	for id, k := range idToKey {
		if keySet[k] {
			conceptIDs = append(conceptIDs, id)
		}
	}
	edges, err := edgeRepo.GetByConceptIDs(dbc, conceptIDs)
	if err != nil {
		return nil
	}

	out := make([]keys.ChainEdge, 0, len(edges))
	for _, e := range edges {
		if e == nil {
			continue
		}
		fk := strings.TrimSpace(idToKey[e.FromConceptID])
		tk := strings.TrimSpace(idToKey[e.ToConceptID])
		if fk == "" || tk == "" || !keySet[fk] || !keySet[tk] {
			continue
		}
		out = append(out, keys.ChainEdge{
			From: fk,
			To:   tk,
			Type: strings.TrimSpace(e.EdgeType),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		if out[i].To != out[j].To {
			return out[i].To < out[j].To
		}
		return out[i].Type < out[j].Type
	})
	return out
}

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

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/prompts"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type ConceptClusterBuildDeps struct {
	DB        *gorm.DB
	Log       *logger.Logger
	Concepts  repos.ConceptRepo
	Clusters  repos.ConceptClusterRepo
	Members   repos.ConceptClusterMemberRepo
	AI        openai.Client
	Vec       pc.VectorStore
	Saga      services.SagaService
	Bootstrap services.LearningBuildBootstrapService
}

type ConceptClusterBuildInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
}

type ConceptClusterBuildOutput struct {
	PathID          uuid.UUID `json:"path_id"`
	ClustersMade    int       `json:"clusters_made"`
	MembersMade     int       `json:"members_made"`
	PineconeBatches int       `json:"pinecone_batches"`
}

func ConceptClusterBuild(ctx context.Context, deps ConceptClusterBuildDeps, in ConceptClusterBuildInput) (ConceptClusterBuildOutput, error) {
	out := ConceptClusterBuildOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Concepts == nil || deps.Clusters == nil || deps.Members == nil || deps.AI == nil || deps.Bootstrap == nil || deps.Saga == nil {
		return out, fmt.Errorf("concept_cluster_build: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("concept_cluster_build: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("concept_cluster_build: missing material_set_id")
	}
	if in.SagaID == uuid.Nil {
		return out, fmt.Errorf("concept_cluster_build: missing saga_id")
	}

	pathID, err := deps.Bootstrap.EnsurePath(dbctx.Context{Ctx: ctx}, in.OwnerUserID, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	existing, err := deps.Clusters.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID)
	if err != nil {
		return out, err
	}
	if len(existing) > 0 {
		return out, nil
	}

	concepts, err := deps.Concepts.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID)
	if err != nil {
		return out, err
	}
	if len(concepts) == 0 {
		return out, fmt.Errorf("concept_cluster_build: no concepts for path")
	}

	// Stable ConceptsJSON input.
	type cjson struct {
		Key     string `json:"key"`
		Name    string `json:"name"`
		Summary string `json:"summary"`
	}
	carr := make([]cjson, 0, len(concepts))
	keyToID := map[string]uuid.UUID{}
	for _, c := range concepts {
		if c == nil {
			continue
		}
		keyToID[c.Key] = c.ID
		carr = append(carr, cjson{Key: c.Key, Name: c.Name, Summary: c.Summary})
	}
	sort.Slice(carr, func(i, j int) bool { return carr[i].Key < carr[j].Key })
	conceptsJSON, _ := json.Marshal(map[string]any{"concepts": carr})

	p, err := prompts.Build(prompts.PromptConceptClusters, prompts.Input{ConceptsJSON: string(conceptsJSON)})
	if err != nil {
		return out, err
	}
	obj, err := deps.AI.GenerateJSON(ctx, p.System, p.User, p.SchemaName, p.Schema)
	if err != nil {
		return out, err
	}
	clustersOut := parseConceptClusters(obj)
	if len(clustersOut) == 0 {
		return out, fmt.Errorf("concept_cluster_build: 0 clusters returned")
	}

	// Stable ordering for doc/embedding alignment.
	sort.Slice(clustersOut, func(i, j int) bool { return clustersOut[i].Label < clustersOut[j].Label })

	docs := make([]string, 0, len(clustersOut))
	for _, c := range clustersOut {
		doc := strings.TrimSpace(c.Label + "\n" + c.Rationale + "\n" + strings.Join(c.Tags, ", ") + "\n" + strings.Join(c.ConceptKeys, ", "))
		docs = append(docs, doc)
	}

	embs, err := deps.AI.Embed(ctx, docs)
	if err != nil {
		return out, err
	}
	if len(embs) != len(clustersOut) {
		return out, fmt.Errorf("concept_cluster_build: embedding count mismatch (got %d want %d)", len(embs), len(clustersOut))
	}

	type clusterRow struct {
		Row *types.ConceptCluster
		Emb []float32
	}
	rows := make([]clusterRow, 0, len(clustersOut))
	for i := range clustersOut {
		id := uuid.New()
		rows = append(rows, clusterRow{
			Row: &types.ConceptCluster{
				ID:        id,
				Scope:     "path",
				ScopeID:   &pathID,
				Label:     clustersOut[i].Label,
				Metadata:  datatypes.JSON(mustJSON(map[string]any{"tags": clustersOut[i].Tags, "rationale": clustersOut[i].Rationale, "concept_keys": clustersOut[i].ConceptKeys})),
				Embedding: datatypes.JSON(mustJSON(embs[i])),
				VectorID:  "concept_cluster:" + id.String(),
			},
			Emb: embs[i],
		})
	}

	ns := index.ConceptClustersNamespace("path", &pathID)
	const batchSize = 64

	if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		if _, err := deps.Bootstrap.EnsurePath(dbc, in.OwnerUserID, in.MaterialSetID); err != nil {
			return err
		}

		toCreate := make([]*types.ConceptCluster, 0, len(rows))
		for _, r := range rows {
			toCreate = append(toCreate, r.Row)
		}
		if _, err := deps.Clusters.Create(dbc, toCreate); err != nil {
			return err
		}
		out.ClustersMade = len(toCreate)

		members := make([]*types.ConceptClusterMember, 0)
		for i, c := range clustersOut {
			clusterID := rows[i].Row.ID
			for _, key := range dedupeStrings(c.ConceptKeys) {
				cid := keyToID[key]
				if cid == uuid.Nil {
					continue
				}
				members = append(members, &types.ConceptClusterMember{
					ID:        uuid.New(),
					ClusterID: clusterID,
					ConceptID: cid,
					Weight:    1,
				})
			}
		}
		if n, _ := deps.Members.CreateIgnoreDuplicates(dbc, members); n > 0 {
			out.MembersMade = n
		}

		if deps.Vec != nil {
			for start := 0; start < len(rows); start += batchSize {
				end := start + batchSize
				if end > len(rows) {
					end = len(rows)
				}
				ids := make([]string, 0, end-start)
				for _, r := range rows[start:end] {
					if r.Row != nil {
						ids = append(ids, r.Row.VectorID)
					}
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
		for start := 0; start < len(rows); start += batchSize {
			end := start + batchSize
			if end > len(rows) {
				end = len(rows)
			}
			pv := make([]pc.Vector, 0, end-start)
			for _, r := range rows[start:end] {
				if r.Row == nil || len(r.Emb) == 0 {
					continue
				}
				pv = append(pv, pc.Vector{
					ID:     r.Row.VectorID,
					Values: r.Emb,
					Metadata: map[string]any{
						"type":       "concept_cluster",
						"cluster_id": r.Row.ID.String(),
						"label":      r.Row.Label,
						"path_id":    pathID.String(),
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

type conceptClusterItem struct {
	Label       string   `json:"label"`
	ConceptKeys []string `json:"concept_keys"`
	Tags        []string `json:"tags"`
	Rationale   string   `json:"rationale"`
}

func parseConceptClusters(obj map[string]any) []conceptClusterItem {
	raw, ok := obj["clusters"]
	if !ok || raw == nil {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]conceptClusterItem, 0, len(arr))
	for _, x := range arr {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		label := strings.TrimSpace(stringFromAny(m["label"]))
		if label == "" {
			continue
		}
		out = append(out, conceptClusterItem{
			Label:       label,
			ConceptKeys: dedupeStrings(stringSliceFromAny(m["concept_keys"])),
			Tags:        dedupeStrings(stringSliceFromAny(m["tags"])),
			Rationale:   strings.TrimSpace(stringFromAny(m["rationale"])),
		})
	}
	return out
}

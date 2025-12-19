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
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/clients/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/learning/prompts"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type ConceptGraphBuildDeps struct {
	DB     *gorm.DB
	Log    *logger.Logger
	Files  repos.MaterialFileRepo
	Chunks repos.MaterialChunkRepo

	Concepts repos.ConceptRepo
	Evidence repos.ConceptEvidenceRepo
	Edges    repos.ConceptEdgeRepo

	AI        openai.Client
	Vec       pc.VectorStore
	Saga      services.SagaService
	Bootstrap services.LearningBuildBootstrapService
}

type ConceptGraphBuildInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
}

type ConceptGraphBuildOutput struct {
	PathID          uuid.UUID `json:"path_id"`
	ConceptsMade    int       `json:"concepts_made"`
	EdgesMade       int       `json:"edges_made"`
	PineconeBatches int       `json:"pinecone_batches"`
}

func ConceptGraphBuild(ctx context.Context, deps ConceptGraphBuildDeps, in ConceptGraphBuildInput) (ConceptGraphBuildOutput, error) {
	out := ConceptGraphBuildOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Files == nil || deps.Chunks == nil || deps.Concepts == nil || deps.Evidence == nil || deps.Edges == nil || deps.AI == nil || deps.Bootstrap == nil || deps.Saga == nil {
		return out, fmt.Errorf("concept_graph_build: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("concept_graph_build: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("concept_graph_build: missing material_set_id")
	}
	if in.SagaID == uuid.Nil {
		return out, fmt.Errorf("concept_graph_build: missing saga_id")
	}

	pathID, err := deps.Bootstrap.EnsurePath(ctx, nil, in.OwnerUserID, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	existing, err := deps.Concepts.GetByScope(ctx, nil, "path", &pathID)
	if err != nil {
		return out, err
	}
	if len(existing) > 0 {
		// Canonical graph already exists. Skip regeneration to preserve stability.
		return out, nil
	}

	// ---- Build prompts inputs (grounded excerpts) ----
	files, err := deps.Files.GetByMaterialSetID(ctx, nil, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	fileIDs := make([]uuid.UUID, 0, len(files))
	for _, f := range files {
		if f != nil && f.ID != uuid.Nil {
			fileIDs = append(fileIDs, f.ID)
		}
	}
	chunks, err := deps.Chunks.GetByMaterialFileIDs(ctx, nil, fileIDs)
	if err != nil {
		return out, err
	}
	if len(chunks) == 0 {
		return out, fmt.Errorf("concept_graph_build: no chunks for material set")
	}
	excerpts := stratifiedChunkExcerpts(chunks, 14, 700)
	if strings.TrimSpace(excerpts) == "" {
		return out, fmt.Errorf("concept_graph_build: empty excerpts")
	}

	// ---- Prompt: Concept inventory ----
	invPrompt, err := prompts.Build(prompts.PromptConceptInventory, prompts.Input{Excerpts: excerpts})
	if err != nil {
		return out, err
	}
	invObj, err := deps.AI.GenerateJSON(ctx, invPrompt.System, invPrompt.User, invPrompt.SchemaName, invPrompt.Schema)
	if err != nil {
		return out, err
	}

	conceptsOut, err := parseConceptInventory(invObj)
	if err != nil {
		return out, err
	}
	if len(conceptsOut) == 0 {
		return out, fmt.Errorf("concept_graph_build: concept inventory returned 0 concepts")
	}

	// Stable ordering for deterministic IDs/embeddings batching.
	sort.Slice(conceptsOut, func(i, j int) bool { return conceptsOut[i].Key < conceptsOut[j].Key })

	// ---- Prompt: Concept edges ----
	conceptsJSONBytes, _ := json.Marshal(map[string]any{"concepts": conceptsOut})
	edgesPrompt, err := prompts.Build(prompts.PromptConceptEdges, prompts.Input{
		ConceptsJSON: string(conceptsJSONBytes),
		Excerpts:     excerpts,
	})
	if err != nil {
		return out, err
	}
	edgesObj, err := deps.AI.GenerateJSON(ctx, edgesPrompt.System, edgesPrompt.User, edgesPrompt.SchemaName, edgesPrompt.Schema)
	if err != nil {
		return out, err
	}
	edgesOut := parseConceptEdges(edgesObj)

	// ---- Embed concept docs (before tx) ----
	conceptDocs := make([]string, 0, len(conceptsOut))
	for _, c := range conceptsOut {
		doc := strings.TrimSpace(c.Name + "\n" + c.Summary + "\n" + strings.Join(c.KeyPoints, "\n"))
		if doc == "" {
			doc = c.Key
		}
		conceptDocs = append(conceptDocs, doc)
	}

	embs, err := deps.AI.Embed(ctx, conceptDocs)
	if err != nil {
		return out, err
	}
	if len(embs) != len(conceptsOut) {
		return out, fmt.Errorf("concept_graph_build: embedding count mismatch (got %d want %d)", len(embs), len(conceptsOut))
	}

	// ---- Persist canonical state + append saga actions (single tx) ----
	type conceptRow struct {
		Row *types.Concept
		Emb []float32
	}
	rows := make([]conceptRow, 0, len(conceptsOut))
	keyToID := map[string]uuid.UUID{}
	for i := range conceptsOut {
		id := uuid.New()
		keyToID[conceptsOut[i].Key] = id
		rows = append(rows, conceptRow{
			Row: &types.Concept{
				ID:        id,
				Scope:     "path",
				ScopeID:   &pathID,
				ParentID:  nil, // set after insert to avoid FK ordering issues
				Depth:     conceptsOut[i].Depth,
				SortIndex: conceptsOut[i].Importance,
				Key:       conceptsOut[i].Key,
				Name:      conceptsOut[i].Name,
				Summary:   conceptsOut[i].Summary,
				KeyPoints: datatypes.JSON(mustJSON(conceptsOut[i].KeyPoints)),
				VectorID:  "concept:" + id.String(),
				Metadata:  datatypes.JSON(mustJSON(map[string]any{"aliases": conceptsOut[i].Aliases, "importance": conceptsOut[i].Importance})),
			},
			Emb: embs[i],
		})
	}

	ns := index.ConceptsNamespace("path", &pathID)
	const batchSize = 64
	if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// EnsurePath inside the tx (no-op if already set).
		if _, err := deps.Bootstrap.EnsurePath(ctx, tx, in.OwnerUserID, in.MaterialSetID); err != nil {
			return err
		}

		// Create concepts (canonical).
		toCreate := make([]*types.Concept, 0, len(rows))
		for _, r := range rows {
			toCreate = append(toCreate, r.Row)
		}
		if _, err := deps.Concepts.Create(ctx, tx, toCreate); err != nil {
			return err
		}
		out.ConceptsMade = len(toCreate)

		// Parent links must be written after inserts to avoid FK ordering constraints.
		for _, c := range conceptsOut {
			if strings.TrimSpace(c.ParentKey) == "" {
				continue
			}
			childID := keyToID[c.Key]
			parentID := keyToID[c.ParentKey]
			if childID == uuid.Nil || parentID == uuid.Nil {
				continue
			}
			_ = deps.Concepts.UpdateFields(ctx, tx, childID, map[string]interface{}{"parent_id": parentID})
		}

		// Create evidences (canonical).
		evRows := make([]*types.ConceptEvidence, 0)
		for _, c := range conceptsOut {
			cid := keyToID[c.Key]
			for _, sid := range uuidSliceFromStrings(dedupeStrings(c.Citations)) {
				evRows = append(evRows, &types.ConceptEvidence{
					ID:              uuid.New(),
					ConceptID:       cid,
					MaterialChunkID: sid,
					Kind:            "grounding",
					Weight:          1,
					CreatedAt:       time.Now().UTC(),
					UpdatedAt:       time.Now().UTC(),
				})
			}
		}
		_, _ = deps.Evidence.CreateIgnoreDuplicates(ctx, tx, evRows)

		// Create edges (canonical).
		for _, e := range edgesOut {
			fid := keyToID[e.FromKey]
			tid := keyToID[e.ToKey]
			if fid == uuid.Nil || tid == uuid.Nil {
				continue
			}
			edge := &types.ConceptEdge{
				ID:            uuid.New(),
				FromConceptID: fid,
				ToConceptID:   tid,
				EdgeType:      e.EdgeType,
				Strength:      e.Strength,
				Evidence:      datatypes.JSON(mustJSON(map[string]any{"rationale": e.Rationale, "citations": e.Citations})),
			}
			_ = deps.Edges.Upsert(ctx, tx, edge)
			out.EdgesMade++
		}

		// Append Pinecone compensations for all concept vectors (if configured).
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
				if err := deps.Saga.AppendAction(ctx, tx, in.SagaID, services.SagaActionKindPineconeDeleteIDs, map[string]any{
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

	// ---- Upsert to Pinecone (best-effort; cache only) ----
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
						"type":       "concept",
						"concept_id": r.Row.ID.String(),
						"key":        r.Row.Key,
						"name":       r.Row.Name,
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

type conceptInvItem struct {
	Key        string   `json:"key"`
	Name       string   `json:"name"`
	ParentKey  string   `json:"parent_key"`
	Depth      int      `json:"depth"`
	Summary    string   `json:"summary"`
	KeyPoints  []string `json:"key_points"`
	Aliases    []string `json:"aliases"`
	Importance int      `json:"importance"`
	Citations  []string `json:"citations"`
}

type conceptEdgeItem struct {
	FromKey   string   `json:"from_key"`
	ToKey     string   `json:"to_key"`
	EdgeType  string   `json:"edge_type"`
	Strength  float64  `json:"strength"`
	Rationale string   `json:"rationale"`
	Citations []string `json:"citations"`
}

func parseConceptInventory(obj map[string]any) ([]conceptInvItem, error) {
	raw, ok := obj["concepts"]
	if !ok || raw == nil {
		return nil, fmt.Errorf("concept_inventory: missing concepts")
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("concept_inventory: concepts not array")
	}
	out := make([]conceptInvItem, 0, len(arr))
	for _, x := range arr {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		key := strings.TrimSpace(stringFromAny(m["key"]))
		name := strings.TrimSpace(stringFromAny(m["name"]))
		if key == "" || name == "" {
			continue
		}
		parentKey := strings.TrimSpace(stringFromAny(m["parent_key"]))
		out = append(out, conceptInvItem{
			Key:        key,
			Name:       name,
			ParentKey:  parentKey,
			Depth:      intFromAny(m["depth"], 0),
			Summary:    strings.TrimSpace(stringFromAny(m["summary"])),
			KeyPoints:  dedupeStrings(stringSliceFromAny(m["key_points"])),
			Aliases:    dedupeStrings(stringSliceFromAny(m["aliases"])),
			Importance: intFromAny(m["importance"], 0),
			Citations:  dedupeStrings(stringSliceFromAny(m["citations"])),
		})
	}
	return out, nil
}

func parseConceptEdges(obj map[string]any) []conceptEdgeItem {
	raw, ok := obj["edges"]
	if !ok || raw == nil {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]conceptEdgeItem, 0, len(arr))
	for _, x := range arr {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		fk := strings.TrimSpace(stringFromAny(m["from_key"]))
		tk := strings.TrimSpace(stringFromAny(m["to_key"]))
		et := strings.TrimSpace(stringFromAny(m["edge_type"]))
		if fk == "" || tk == "" || et == "" {
			continue
		}
		out = append(out, conceptEdgeItem{
			FromKey:   fk,
			ToKey:     tk,
			EdgeType:  et,
			Strength:  floatFromAny(m["strength"], 1),
			Rationale: strings.TrimSpace(stringFromAny(m["rationale"])),
			Citations: dedupeStrings(stringSliceFromAny(m["citations"])),
		})
	}
	return out
}

func intFromAny(v any, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		i, _ := t.Int64()
		return int(i)
	default:
		return def
	}
}

func floatFromAny(v any, def float64) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		n := json.Number(strings.TrimSpace(t))
		f, err := n.Float64()
		if err == nil {
			return f
		}
		return def
	default:
		return def
	}
}

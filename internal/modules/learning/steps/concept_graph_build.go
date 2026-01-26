package steps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/prompts"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"golang.org/x/sync/errgroup"
)

type ConceptGraphBuildDeps struct {
	DB     *gorm.DB
	Log    *logger.Logger
	Files  repos.MaterialFileRepo
	Chunks repos.MaterialChunkRepo
	Path   repos.PathRepo

	Concepts repos.ConceptRepo
	Evidence repos.ConceptEvidenceRepo
	Edges    repos.ConceptEdgeRepo

	Graph *neo4jdb.Client

	AI        openai.Client
	Vec       pc.VectorStore
	Saga      services.SagaService
	Bootstrap services.LearningBuildBootstrapService
}

type ConceptGraphBuildInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
}

type ConceptGraphBuildOutput struct {
	PathID          uuid.UUID `json:"path_id"`
	ConceptsMade    int       `json:"concepts_made"`
	EdgesMade       int       `json:"edges_made"`
	PineconeBatches int       `json:"pinecone_batches"`
}

func ConceptGraphBuild(ctx context.Context, deps ConceptGraphBuildDeps, in ConceptGraphBuildInput) (ConceptGraphBuildOutput, error) {
	out := ConceptGraphBuildOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Files == nil || deps.Chunks == nil || deps.Path == nil || deps.Concepts == nil || deps.Evidence == nil || deps.Edges == nil || deps.AI == nil || deps.Bootstrap == nil || deps.Saga == nil {
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

	pathID, err := resolvePathID(ctx, deps.Bootstrap, in.OwnerUserID, in.MaterialSetID, in.PathID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	existing, err := deps.Concepts.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID)
	if err != nil {
		return out, err
	}
	if len(existing) > 0 {
		// Best-effort: ensure canonical (global) concepts exist and path concepts have canonical IDs,
		// even for legacy paths that were generated before canonicalization existed.
		_ = deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			dbc := dbctx.Context{Ctx: ctx, Tx: tx}
			_ = advisoryXactLock(tx, "concept_canonicalize", pathID)
			rows, err := deps.Concepts.GetByScope(dbc, "path", &pathID)
			if err != nil {
				return err
			}
			_, _ = canonicalizePathConcepts(dbc, tx, deps.Concepts, rows, nil)
			return nil
		})

		// Canonical graph already exists. Skip regeneration to preserve stability.
		if deps.Graph != nil {
			if err := syncPathConceptGraphToNeo4j(ctx, deps, pathID); err != nil {
				deps.Log.Warn("neo4j concept graph sync failed (continuing)", "error", err, "path_id", pathID.String())
			}
		}
		return out, nil
	}

	// Optional: incorporate user intent/intake context (written by path_intake) to improve relevance and reduce noise.
	intentMD := ""
	var allowFiles map[uuid.UUID]bool
	if deps.Path != nil {
		if row, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, pathID); err == nil && row != nil && len(row.Metadata) > 0 && string(row.Metadata) != "null" {
			var meta map[string]any
			if json.Unmarshal(row.Metadata, &meta) == nil {
				if intake := mapFromAny(meta["intake"]); intake != nil {
					if !boolFromAny(intake["paths_confirmed"]) {
						return out, nil
					}
				}
				intentMD = strings.TrimSpace(stringFromAny(meta["intake_md"]))
				allowFiles = intakeMaterialAllowlistFromPathMeta(meta)
			}
		}
	}

	// ---- Build prompts inputs (grounded excerpts) ----
	files, err := deps.Files.GetByMaterialSetID(dbctx.Context{Ctx: ctx}, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	if len(allowFiles) > 0 {
		filtered := filterMaterialFilesByAllowlist(files, allowFiles)
		if len(filtered) > 0 {
			files = filtered
		} else {
			deps.Log.Warn("concept_graph_build: intake filter excluded all files; ignoring filter", "path_id", pathID.String())
		}
	}
	fileIDs := make([]uuid.UUID, 0, len(files))
	for _, f := range files {
		if f != nil && f.ID != uuid.Nil {
			fileIDs = append(fileIDs, f.ID)
		}
	}
	chunks, err := deps.Chunks.GetByMaterialFileIDs(dbctx.Context{Ctx: ctx}, fileIDs)
	if err != nil {
		return out, err
	}
	if len(chunks) == 0 {
		return out, fmt.Errorf("concept_graph_build: no chunks for material set")
	}

	allowedChunkIDs := map[string]bool{}
	for _, ch := range chunks {
		if ch != nil && ch.ID != uuid.Nil {
			allowedChunkIDs[ch.ID.String()] = true
		}
	}

	chunkByID := map[uuid.UUID]*types.MaterialChunk{}
	chunkEmbs := make([]chunkEmbedding, 0, len(chunks))
	for _, ch := range chunks {
		if ch == nil || ch.ID == uuid.Nil {
			continue
		}
		chunkByID[ch.ID] = ch
		if emb, ok := decodeEmbedding(ch.Embedding); ok && len(emb) > 0 {
			chunkEmbs = append(chunkEmbs, chunkEmbedding{ID: ch.ID, Emb: emb})
		}
	}
	sort.Slice(chunkEmbs, func(i, j int) bool { return chunkEmbs[i].ID.String() < chunkEmbs[j].ID.String() })

	perFile := envIntAllowZero("CONCEPT_GRAPH_EXCERPTS_PER_FILE", 14)
	excerptMaxChars := envIntAllowZero("CONCEPT_GRAPH_EXCERPT_MAX_CHARS", 700)
	excerptMaxLines := envIntAllowZero("CONCEPT_GRAPH_EXCERPT_MAX_LINES", 0)
	excerptMaxTotal := envIntAllowZero("CONCEPT_GRAPH_EXCERPT_MAX_TOTAL_CHARS", 0)
	excerpts, excerptChunkIDs := stratifiedChunkExcerptsWithLimitsAndIDs(
		chunks,
		perFile,
		excerptMaxChars,
		excerptMaxLines,
		excerptMaxTotal,
	)
	if strings.TrimSpace(excerpts) == "" {
		return out, fmt.Errorf("concept_graph_build: empty excerpts")
	}
	edgeExcerpts := stratifiedChunkExcerptsWithLimits(
		chunks,
		perFile,
		envIntAllowZero("CONCEPT_GRAPH_EDGE_EXCERPT_MAX_CHARS", 700),
		envIntAllowZero("CONCEPT_GRAPH_EDGE_EXCERPT_MAX_LINES", 0),
		envIntAllowZero("CONCEPT_GRAPH_EDGE_EXCERPT_MAX_TOTAL_CHARS", 0),
	)
	if strings.TrimSpace(edgeExcerpts) == "" {
		edgeExcerpts = excerpts
	}

	// ---- Prompt: Concept inventory ----
	invPrompt, err := prompts.Build(prompts.PromptConceptInventory, prompts.Input{Excerpts: excerpts, PathIntentMD: intentMD})
	if err != nil {
		return out, err
	}
	invObj, err := deps.AI.GenerateJSON(ctx, invPrompt.System, invPrompt.User, invPrompt.SchemaName, invPrompt.Schema)
	if err != nil {
		return out, err
	}

	invCoverage := parseConceptCoverage(invObj)
	conceptsOut, err := parseConceptInventory(invObj)
	if err != nil {
		return out, err
	}
	if len(conceptsOut) == 0 {
		return out, fmt.Errorf("concept_graph_build: concept inventory returned 0 concepts")
	}

	conceptsOut, normStats := normalizeConceptInventory(conceptsOut, allowedChunkIDs)
	if normStats.Modified > 0 {
		deps.Log.Info(
			"concept inventory normalized",
			"key_changes", normStats.KeysChanged,
			"depth_recomputed", normStats.DepthRecomputed,
			"parents_repaired", normStats.ParentsRepaired,
			"cycles_broken", normStats.ParentCyclesBroken,
			"citations_filtered", normStats.CitationsFiltered,
		)
	}

	conceptsOut, dupKeys := dedupeConceptInventoryByKey(conceptsOut)
	if dupKeys > 0 {
		deps.Log.Warn("concept inventory returned duplicate keys; deduped", "count", dupKeys)
	}
	if len(conceptsOut) == 0 {
		return out, fmt.Errorf("concept_graph_build: concept inventory returned 0 unique concepts")
	}

	// ---- Coverage completion (iterative delta passes) ----
	conceptsOut = completeConceptCoverage(ctx, deps, conceptCoverageInput{
		PathID:             pathID,
		MaterialSetID:      in.MaterialSetID,
		IntentMD:           intentMD,
		Chunks:             chunks,
		ChunkByID:          chunkByID,
		ChunkEmbs:          chunkEmbs,
		AllowedChunkIDs:    allowedChunkIDs,
		InitialChunkIDs:    excerptChunkIDs,
		InitialCoverage:    invCoverage,
		Concepts:           conceptsOut,
		MaterialFileFilter: allowFiles,
	})

	// Stable ordering for deterministic IDs/embeddings batching.
	sort.Slice(conceptsOut, func(i, j int) bool { return conceptsOut[i].Key < conceptsOut[j].Key })

	// ---- Prompt: Concept edges ----
	conceptsJSONBytes, _ := json.Marshal(map[string]any{"concepts": conceptsOut})
	edgesPrompt, err := prompts.Build(prompts.PromptConceptEdges, prompts.Input{
		ConceptsJSON: string(conceptsJSONBytes),
		Excerpts:     edgeExcerpts,
		PathIntentMD: intentMD,
	})
	if err != nil {
		return out, err
	}

	// ---- Embed concept docs + generate edges in parallel (before tx) ----
	conceptDocs := make([]string, 0, len(conceptsOut))
	for _, c := range conceptsOut {
		doc := strings.TrimSpace(c.Name + "\n" + c.Summary + "\n" + strings.Join(c.KeyPoints, "\n"))
		if doc == "" {
			doc = c.Key
		}
		conceptDocs = append(conceptDocs, doc)
	}

	var (
		edgesObj map[string]any
		embs     [][]float32
	)

	embedBatchSize := envIntAllowZero("CONCEPT_GRAPH_EMBED_BATCH_SIZE", 64)
	if embedBatchSize <= 0 {
		embedBatchSize = 64
	}
	embedConc := envIntAllowZero("CONCEPT_GRAPH_EMBED_CONCURRENCY", 20)
	if embedConc < 1 {
		embedConc = 1
	}
	embedBatched := func(ctx context.Context, docs []string) ([][]float32, error) {
		if len(docs) == 0 {
			return nil, fmt.Errorf("concept_graph_build: empty embed docs")
		}
		out := make([][]float32, len(docs))
		eg, egctx := errgroup.WithContext(ctx)
		eg.SetLimit(embedConc)
		for start := 0; start < len(docs); start += embedBatchSize {
			start := start
			end := start + embedBatchSize
			if end > len(docs) {
				end = len(docs)
			}
			eg.Go(func() error {
				v, err := deps.AI.Embed(egctx, docs[start:end])
				if err != nil {
					return err
				}
				if len(v) != end-start {
					return fmt.Errorf("concept_graph_build: embedding count mismatch (got %d want %d)", len(v), end-start)
				}
				for i := range v {
					out[start+i] = v[i]
				}
				return nil
			})
		}
		if err := eg.Wait(); err != nil {
			return nil, err
		}
		for i := range out {
			if len(out[i]) == 0 {
				return nil, fmt.Errorf("concept_graph_build: empty embedding at index %d", i)
			}
		}
		return out, nil
	}

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		obj, err := deps.AI.GenerateJSON(gctx, edgesPrompt.System, edgesPrompt.User, edgesPrompt.SchemaName, edgesPrompt.Schema)
		if err != nil {
			return err
		}
		edgesObj = obj
		return nil
	})
	g.Go(func() error {
		v, err := embedBatched(gctx, conceptDocs)
		if err != nil {
			return err
		}
		embs = v
		return nil
	})
	if err := g.Wait(); err != nil {
		return out, err
	}

	edgesOut := parseConceptEdges(edgesObj)
	edgesOut, edgeStats := normalizeConceptEdges(edgesOut, conceptsOut, allowedChunkIDs)
	if edgeStats.Modified > 0 {
		deps.Log.Info(
			"concept edges normalized",
			"dropped_missing_concepts", edgeStats.DroppedMissingConcepts,
			"dropped_self_loops", edgeStats.DroppedSelfLoops,
			"type_normalized", edgeStats.TypeNormalized,
			"strength_clamped", edgeStats.StrengthClamped,
			"citations_filtered", edgeStats.CitationsFiltered,
			"deduped", edgeStats.Deduped,
		)
	}

	if len(embs) != len(conceptsOut) {
		return out, fmt.Errorf("concept_graph_build: embedding count mismatch (got %d want %d)", len(embs), len(conceptsOut))
	}

	// ---- Semantic canonical concept matching (cross-path key unification) ----
	//
	// We keep concept.key stable per-path, but unify canonical/global concept identity using:
	//  1) explicit aliases (LLM-provided) normalized to keys, then
	//  2) embedding similarity against global canonical concept vectors.
	//
	// This produces a mapping: path_concept_key -> canonical_global_concept_id
	// used by canonicalizePathConcepts() to create global alias rows and backfill canonical_concept_id.
	semanticMatchByKey := map[string]uuid.UUID{}
	if deps.Vec != nil && deps.Concepts != nil && len(conceptsOut) > 0 {
		minScore := envFloatAllowZero("CANONICAL_CONCEPT_SEMANTIC_MIN_SCORE", 0.885)
		minGap := envFloatAllowZero("CANONICAL_CONCEPT_SEMANTIC_MIN_GAP", 0.02)
		topK := envIntAllowZero("CANONICAL_CONCEPT_SEMANTIC_TOP_K", 6)
		if topK < 1 {
			topK = 6
		}
		conc := envIntAllowZero("CANONICAL_CONCEPT_SEMANTIC_CONCURRENCY", 12)
		if conc < 1 {
			conc = 1
		}
		timeoutMS := envIntAllowZero("CANONICAL_CONCEPT_SEMANTIC_TIMEOUT_MS", 2500)
		if timeoutMS < 250 {
			timeoutMS = 250
		}

		keys := make([]string, 0, len(conceptsOut))
		aliasKeysByKey := map[string][]string{}
		aliasKeys := make([]string, 0, len(conceptsOut)*2)
		for _, c := range conceptsOut {
			k := strings.TrimSpace(strings.ToLower(c.Key))
			if k == "" {
				continue
			}
			keys = append(keys, k)
			for _, a := range c.Aliases {
				ak := normalizeConceptKey(a)
				if ak == "" || ak == k {
					continue
				}
				aliasKeysByKey[k] = append(aliasKeysByKey[k], ak)
				aliasKeys = append(aliasKeys, ak)
			}
		}
		keys = dedupeStrings(keys)
		queryKeys := dedupeStrings(append(keys, aliasKeys...))

		globalRootByKey := map[string]uuid.UUID{}
		if len(queryKeys) > 0 {
			if rows, err := deps.Concepts.GetByScopeAndKeys(dbctx.Context{Ctx: ctx}, "global", nil, queryKeys); err == nil {
				for _, g := range rows {
					if g == nil || g.ID == uuid.Nil {
						continue
					}
					k := strings.TrimSpace(strings.ToLower(g.Key))
					if k == "" {
						continue
					}
					root := g.ID
					if g.CanonicalConceptID != nil && *g.CanonicalConceptID != uuid.Nil {
						root = *g.CanonicalConceptID
					}
					if root != uuid.Nil {
						globalRootByKey[k] = root
					}
				}
			}
		}

		// Prefer alias-key matches before doing any vector search.
		todoIdx := make([]int, 0, 8)
		aliasMatched := 0
		for i := range conceptsOut {
			k := strings.TrimSpace(strings.ToLower(conceptsOut[i].Key))
			if k == "" {
				continue
			}
			if globalRootByKey[k] != uuid.Nil {
				continue // exact global key already exists; canonicalizePathConcepts will handle redirect chains.
			}
			aks := dedupeStrings(aliasKeysByKey[k])
			found := uuid.Nil
			for _, ak := range aks {
				if id := globalRootByKey[ak]; id != uuid.Nil {
					found = id
					break
				}
			}
			if found != uuid.Nil {
				semanticMatchByKey[k] = found
				aliasMatched++
				continue
			}
			todoIdx = append(todoIdx, i)
		}

		// Embedding-based semantic matching (global ANN search) for remaining missing keys.
		semanticMatched := 0
		if minScore > 0 && len(todoIdx) > 0 {
			globalNS := index.ConceptsNamespace("global", nil)
			filter := map[string]any{"type": "concept", "scope": "global", "canonical": true}

			var mu sync.Mutex
			eg, egctx := errgroup.WithContext(ctx)
			eg.SetLimit(conc)

			for _, idx := range todoIdx {
				idx := idx
				eg.Go(func() error {
					if idx < 0 || idx >= len(conceptsOut) || idx >= len(embs) {
						return nil
					}
					k := strings.TrimSpace(strings.ToLower(conceptsOut[idx].Key))
					if k == "" {
						return nil
					}
					if len(embs[idx]) == 0 {
						return nil
					}
					qctx, cancel := context.WithTimeout(egctx, time.Duration(timeoutMS)*time.Millisecond)
					matches, err := deps.Vec.QueryMatches(qctx, globalNS, embs[idx], topK, filter)
					cancel()
					if err != nil || len(matches) == 0 {
						return nil
					}
					best := matches[0]
					if best.Score < minScore {
						return nil
					}
					if len(matches) > 1 && (best.Score-matches[1].Score) < minGap {
						return nil
					}
					idStr := strings.TrimSpace(best.ID)
					if strings.HasPrefix(idStr, "concept:") {
						idStr = strings.TrimPrefix(idStr, "concept:")
					}
					cid, err := uuid.Parse(strings.TrimSpace(idStr))
					if err != nil || cid == uuid.Nil {
						return nil
					}
					mu.Lock()
					semanticMatchByKey[k] = cid
					semanticMatched++
					mu.Unlock()
					return nil
				})
			}
			_ = eg.Wait()
		}

		// Resolve any matched global concept IDs that are themselves aliases/redirects (one-hop best-effort).
		if len(semanticMatchByKey) > 0 {
			ids := make([]uuid.UUID, 0, len(semanticMatchByKey))
			seenIDs := map[uuid.UUID]bool{}
			for _, id := range semanticMatchByKey {
				if id != uuid.Nil && !seenIDs[id] {
					seenIDs[id] = true
					ids = append(ids, id)
				}
			}
			if len(ids) > 0 {
				if rows, err := deps.Concepts.GetByIDs(dbctx.Context{Ctx: ctx}, ids); err == nil {
					redir := map[uuid.UUID]uuid.UUID{}
					for _, r := range rows {
						if r == nil || r.ID == uuid.Nil {
							continue
						}
						if r.CanonicalConceptID != nil && *r.CanonicalConceptID != uuid.Nil {
							redir[r.ID] = *r.CanonicalConceptID
						}
					}
					if len(redir) > 0 {
						for k, id := range semanticMatchByKey {
							if to := redir[id]; to != uuid.Nil {
								semanticMatchByKey[k] = to
							}
						}
					}
				}
			}
		}

		if deps.Log != nil && (aliasMatched > 0 || semanticMatched > 0) {
			deps.Log.Info(
				"canonical concept semantic matches",
				"alias_matches", aliasMatched,
				"semantic_matches", semanticMatched,
				"candidates", len(conceptsOut),
			)
		}
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
	pineconeBatchSize := envIntAllowZero("CONCEPT_GRAPH_PINECONE_BATCH_SIZE", 64)
	if pineconeBatchSize <= 0 {
		pineconeBatchSize = 64
	}
	skipped := false
	txErr := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		// Ensure only one canonical graph write happens per path (race-safe + avoids unique index errors).
		if err := advisoryXactLock(tx, "concept_graph_build", pathID); err != nil {
			return err
		}

		// EnsurePath inside the tx (no-op if already set).
		if in.PathID == uuid.Nil {
			if _, err := deps.Bootstrap.EnsurePath(dbc, in.OwnerUserID, in.MaterialSetID); err != nil {
				return err
			}
		}

		// Race-safe idempotency guard: if another worker already persisted the canonical graph,
		// exit without attempting inserts (avoids unique constraint errors).
		existing, err := deps.Concepts.GetByScope(dbc, "path", &pathID)
		if err != nil {
			return err
		}
		if len(existing) > 0 {
			skipped = true
			return nil
		}

		// Create concepts (canonical).
		toCreate := make([]*types.Concept, 0, len(rows))
		for _, r := range rows {
			toCreate = append(toCreate, r.Row)
		}
		if _, err := deps.Concepts.Create(dbc, toCreate); err != nil {
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
			if err := deps.Concepts.UpdateFields(dbc, childID, map[string]interface{}{"parent_id": parentID}); err != nil {
				return err
			}
		}

		// Create evidences (canonical).
		evRows := make([]*types.ConceptEvidence, 0)
		for _, c := range conceptsOut {
			cid := keyToID[c.Key]
			for _, sid := range uuidSliceFromStrings(dedupeStrings(filterChunkIDStrings(c.Citations, allowedChunkIDs))) {
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
		if _, err := deps.Evidence.CreateIgnoreDuplicates(dbc, evRows); err != nil {
			return err
		}

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
				Evidence:      datatypes.JSON(mustJSON(map[string]any{"rationale": e.Rationale, "citations": filterChunkIDStrings(e.Citations, allowedChunkIDs)})),
			}
			if err := deps.Edges.Upsert(dbc, edge); err != nil {
				return err
			}
			out.EdgesMade++
		}

		// Append Pinecone compensations for all concept vectors (if configured).
		if deps.Vec != nil {
			for start := 0; start < len(rows); start += pineconeBatchSize {
				end := start + pineconeBatchSize
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
	})
	if txErr != nil {
		// If another worker won the race (or older installs have a mismatched unique index),
		// treat as a no-op as long as a canonical graph exists after the error.
		if isUniqueViolation(txErr, "") {
			existingAfter, err := deps.Concepts.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID)
			if err == nil && len(existingAfter) > 0 {
				if deps.Graph != nil {
					if err := syncPathConceptGraphToNeo4j(ctx, deps, pathID); err != nil {
						deps.Log.Warn("neo4j concept graph sync failed (continuing)", "error", err, "path_id", pathID.String())
					}
				}
				deps.Log.Warn("concept graph insert hit unique violation; graph already exists; skipping", "path_id", pathID.String())
				return out, nil
			}

			// Recovery path: if concepts were soft-deleted but a non-partial unique index prevents reinsertion,
			// restore the existing soft-deleted graph so downstream stages can proceed.
			restored := false
			_ = deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				if err := advisoryXactLock(tx, "concept_graph_build", pathID); err != nil {
					return err
				}

				var unscoped []*types.Concept
				if err := tx.Unscoped().
					Where("scope = ? AND scope_id IS NOT DISTINCT FROM ?", "path", &pathID).
					Find(&unscoped).Error; err != nil {
					return err
				}
				if len(unscoped) == 0 {
					return nil
				}

				now := time.Now().UTC()
				if err := tx.Unscoped().
					Model(&types.Concept{}).
					Where("scope = ? AND scope_id IS NOT DISTINCT FROM ?", "path", &pathID).
					Updates(map[string]any{"deleted_at": nil, "updated_at": now}).Error; err != nil {
					return err
				}
				ids := make([]uuid.UUID, 0, len(unscoped))
				for _, c := range unscoped {
					if c != nil && c.ID != uuid.Nil {
						ids = append(ids, c.ID)
					}
				}
				if len(ids) > 0 {
					_ = tx.Unscoped().
						Model(&types.ConceptEvidence{}).
						Where("concept_id IN ?", ids).
						Updates(map[string]any{"deleted_at": nil, "updated_at": now}).Error
					_ = tx.Unscoped().
						Model(&types.ConceptEdge{}).
						Where("from_concept_id IN ? OR to_concept_id IN ?", ids, ids).
						Updates(map[string]any{"deleted_at": nil, "updated_at": now}).Error
				}
				restored = true
				return nil
			})

			if restored {
				existingAfterRestore, err := deps.Concepts.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID)
				if err == nil && len(existingAfterRestore) > 0 {
					if deps.Graph != nil {
						if err := syncPathConceptGraphToNeo4j(ctx, deps, pathID); err != nil {
							deps.Log.Warn("neo4j concept graph sync failed (continuing)", "error", err, "path_id", pathID.String())
						}
					}
					deps.Log.Warn("concept graph restored after unique violation; continuing", "path_id", pathID.String())
					return out, nil
				}
			}
		}

		return out, txErr
	}

	if skipped {
		if deps.Graph != nil {
			if err := syncPathConceptGraphToNeo4j(ctx, deps, pathID); err != nil {
				deps.Log.Warn("neo4j concept graph sync failed (continuing)", "error", err, "path_id", pathID.String())
			}
		}
		return out, nil
	}

	// ---- Canonicalize concepts (best-effort; do not fail the core graph build) ----
	//
	// This links each path-scoped concept to a canonical/global concept ID (Concept.canonical_concept_id),
	// enabling cross-path mastery transfer and semantic matching.
	if deps.Concepts != nil && len(rows) > 0 {
		pathConcepts := make([]*types.Concept, 0, len(rows))
		for _, r := range rows {
			if r.Row != nil && r.Row.ID != uuid.Nil {
				pathConcepts = append(pathConcepts, r.Row)
			}
		}
		if len(pathConcepts) > 0 {
			if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				dbc := dbctx.Context{Ctx: ctx, Tx: tx}
				// Serialize canonicalization per path to avoid conflict churn under retries.
				_ = advisoryXactLock(tx, "concept_canonicalize", pathID)
				_, err := canonicalizePathConcepts(dbc, tx, deps.Concepts, pathConcepts, semanticMatchByKey)
				return err
			}); err != nil {
				deps.Log.Warn("concept_graph_build: canonicalization failed (continuing)", "error", err, "path_id", pathID.String())
			}
		}
	}

	// ---- Upsert to Pinecone (best-effort; cache only) ----
	if deps.Vec != nil {
		pineconeConc := envIntAllowZero("CONCEPT_GRAPH_PINECONE_CONCURRENCY", 20)
		if pineconeConc < 1 {
			pineconeConc = 1
		}

		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(pineconeConc)

		var batches int32
		for start := 0; start < len(rows); start += pineconeBatchSize {
			start := start
			end := start + pineconeBatchSize
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
			g.Go(func() error {
				if err := deps.Vec.Upsert(gctx, ns, pv); err != nil {
					deps.Log.Warn("pinecone upsert failed (continuing)", "namespace", ns, "err", err.Error())
					return nil
				}
				atomic.AddInt32(&batches, 1)
				return nil
			})
		}
		_ = g.Wait()
		out.PineconeBatches = int(atomic.LoadInt32(&batches))

		// Also upsert canonical/global concept vectors for cross-path semantic matching.
		//
		// We index by canonical concept ID (vector_id = "concept:<canonical_uuid>") into the global namespace,
		// which allows new paths to semantically match previously-learned concepts even when their keys differ.
		globalNS := index.ConceptsNamespace("global", nil)
		globalVectors := make([]pc.Vector, 0, len(rows))
		seenGlobal := map[string]bool{}
		for _, r := range rows {
			if r.Row == nil || len(r.Emb) == 0 || r.Row.CanonicalConceptID == nil || *r.Row.CanonicalConceptID == uuid.Nil {
				continue
			}
			cid := *r.Row.CanonicalConceptID
			vid := "concept:" + cid.String()
			if seenGlobal[vid] {
				continue
			}
			seenGlobal[vid] = true
			globalVectors = append(globalVectors, pc.Vector{
				ID:     vid,
				Values: r.Emb,
				Metadata: map[string]any{
					"type":        "concept",
					"scope":       "global",
					"canonical":   true,
					"concept_id":  cid.String(),
					"observedKey": r.Row.Key,
					"observedName": func() string {
						if strings.TrimSpace(r.Row.Name) != "" {
							return r.Row.Name
						}
						return r.Row.Key
					}(),
				},
			})
		}
		if len(globalVectors) > 0 {
			if err := deps.Vec.Upsert(ctx, globalNS, globalVectors); err != nil {
				deps.Log.Warn("pinecone global concept upsert failed (continuing)", "namespace", globalNS, "err", err.Error())
			}
		}
	}

	// ---- Upsert to Neo4j (best-effort; cache only) ----
	if deps.Graph != nil {
		if err := syncPathConceptGraphToNeo4j(ctx, deps, pathID); err != nil {
			deps.Log.Warn("neo4j concept graph sync failed (continuing)", "error", err, "path_id", pathID.String())
		}
	}

	return out, nil
}

type conceptNormStats struct {
	Modified           int
	KeysChanged        int
	DepthRecomputed    int
	ParentsRepaired    int
	ParentCyclesBroken int
	CitationsFiltered  int
}

func normalizeConceptInventory(in []conceptInvItem, allowedChunkIDs map[string]bool) ([]conceptInvItem, conceptNormStats) {
	stats := conceptNormStats{}
	if len(in) == 0 {
		return in, stats
	}

	// Normalize keys + parent keys (may create new collisions; merge best-effort).
	merged := map[string]conceptInvItem{}
	for _, c := range in {
		origKey := strings.TrimSpace(c.Key)
		key := normalizeConceptKey(origKey)
		if key == "" {
			continue
		}
		if key != origKey {
			stats.KeysChanged++
			stats.Modified++
		}
		c.Key = key

		origParent := strings.TrimSpace(c.ParentKey)
		parent := normalizeConceptKey(origParent)
		if parent != origParent {
			stats.ParentsRepaired++
			stats.Modified++
		}
		c.ParentKey = parent

		origCits := c.Citations
		filteredCits := filterChunkIDStrings(origCits, allowedChunkIDs)
		if !stringSlicesEqual(origCits, filteredCits) {
			if len(origCits) > len(filteredCits) {
				stats.CitationsFiltered += len(origCits) - len(filteredCits)
			} else {
				stats.CitationsFiltered++
			}
			stats.Modified++
		}
		c.Citations = filteredCits

		if existing, ok := merged[c.Key]; ok {
			// Merge duplicates: keep best name/summary, union lists.
			stats.Modified++
			existing.Name = stringsOrExisting(existing.Name, c.Name)
			existing.Summary = longerString(existing.Summary, c.Summary)
			existing.KeyPoints = dedupeStrings(append(existing.KeyPoints, c.KeyPoints...))
			existing.Aliases = dedupeStrings(append(existing.Aliases, c.Aliases...))
			existing.Citations = dedupeStrings(append(existing.Citations, c.Citations...))
			if existing.ParentKey == "" && c.ParentKey != "" {
				existing.ParentKey = c.ParentKey
			}
			if c.Importance > existing.Importance {
				existing.Importance = c.Importance
			}
			merged[c.Key] = existing
			continue
		}
		merged[c.Key] = c
	}

	// Ensure parents exist and break parent cycles.
	items := map[string]*conceptInvItem{}
	for k := range merged {
		c := merged[k]
		// Parent must exist and not self.
		if c.ParentKey == c.Key {
			c.ParentKey = ""
			stats.ParentsRepaired++
			stats.Modified++
		}
		if c.ParentKey != "" {
			if _, ok := merged[c.ParentKey]; !ok {
				c.ParentKey = ""
				stats.ParentsRepaired++
				stats.Modified++
			}
		}
		tmp := c
		items[k] = &tmp
	}

	for key, c := range items {
		if c == nil {
			continue
		}
		seen := map[string]bool{key: true}
		p := strings.TrimSpace(c.ParentKey)
		for p != "" {
			if seen[p] {
				// Break the cycle by dropping this node's parent.
				c.ParentKey = ""
				stats.ParentCyclesBroken++
				stats.Modified++
				break
			}
			seen[p] = true
			parent := items[p]
			if parent == nil {
				break
			}
			p = strings.TrimSpace(parent.ParentKey)
		}
	}

	// Recompute depths deterministically from parent links.
	memo := map[string]int{}
	var depthFor func(k string, visiting map[string]bool) int
	depthFor = func(k string, visiting map[string]bool) int {
		if v, ok := memo[k]; ok {
			return v
		}
		if visiting[k] {
			return 0
		}
		visiting[k] = true
		d := 0
		if c := items[k]; c != nil && strings.TrimSpace(c.ParentKey) != "" {
			p := strings.TrimSpace(c.ParentKey)
			if _, ok := items[p]; ok && p != k {
				d = depthFor(p, visiting) + 1
			}
		}
		delete(visiting, k)
		memo[k] = d
		return d
	}

	out := make([]conceptInvItem, 0, len(items))
	for k, c := range items {
		if c == nil {
			continue
		}
		newDepth := depthFor(k, map[string]bool{})
		if c.Depth != newDepth {
			stats.DepthRecomputed++
			stats.Modified++
		}
		c.Depth = newDepth
		out = append(out, *c)
	}

	// Stable ordering for deterministic downstream IDs.
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, stats
}

type edgeNormStats struct {
	Modified               int
	DroppedMissingConcepts int
	DroppedSelfLoops       int
	TypeNormalized         int
	StrengthClamped        int
	CitationsFiltered      int
	Deduped                int
}

func normalizeConceptEdges(in []conceptEdgeItem, concepts []conceptInvItem, allowedChunkIDs map[string]bool) ([]conceptEdgeItem, edgeNormStats) {
	stats := edgeNormStats{}
	if len(in) == 0 {
		return nil, stats
	}
	known := map[string]bool{}
	for _, c := range concepts {
		if strings.TrimSpace(c.Key) != "" {
			known[strings.TrimSpace(c.Key)] = true
		}
	}

	outMap := map[string]conceptEdgeItem{}
	for _, e := range in {
		fk0 := strings.TrimSpace(e.FromKey)
		tk0 := strings.TrimSpace(e.ToKey)
		et0 := strings.TrimSpace(e.EdgeType)
		fk := normalizeConceptKey(fk0)
		tk := normalizeConceptKey(tk0)
		et := strings.ToLower(strings.TrimSpace(et0))
		if fk == "" || tk == "" {
			continue
		}
		if fk != fk0 || tk != tk0 {
			stats.Modified++
		}
		if fk == tk {
			stats.DroppedSelfLoops++
			stats.Modified++
			continue
		}
		if !known[fk] || !known[tk] {
			stats.DroppedMissingConcepts++
			stats.Modified++
			continue
		}
		if et != "prereq" && et != "related" && et != "analogy" {
			et = "related"
			stats.TypeNormalized++
			stats.Modified++
		}
		str := e.Strength
		if str < 0 {
			str = 0
			stats.StrengthClamped++
			stats.Modified++
		} else if str > 1 {
			str = 1
			stats.StrengthClamped++
			stats.Modified++
		}

		cits := filterChunkIDStrings(e.Citations, allowedChunkIDs)
		if len(cits) != len(e.Citations) {
			stats.CitationsFiltered++
			stats.Modified++
		}

		key := fk + "|" + tk + "|" + et
		item := conceptEdgeItem{
			FromKey:   fk,
			ToKey:     tk,
			EdgeType:  et,
			Strength:  str,
			Rationale: strings.TrimSpace(e.Rationale),
			Citations: cits,
		}

		if existing, ok := outMap[key]; ok {
			stats.Deduped++
			stats.Modified++
			// Keep strongest, keep longer rationale, union citations.
			if item.Strength > existing.Strength {
				existing.Strength = item.Strength
			}
			existing.Rationale = longerString(existing.Rationale, item.Rationale)
			existing.Citations = dedupeStrings(append(existing.Citations, item.Citations...))
			outMap[key] = existing
			continue
		}
		outMap[key] = item
	}

	out := make([]conceptEdgeItem, 0, len(outMap))
	for _, v := range outMap {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FromKey != out[j].FromKey {
			return out[i].FromKey < out[j].FromKey
		}
		if out[i].ToKey != out[j].ToKey {
			return out[i].ToKey < out[j].ToKey
		}
		return out[i].EdgeType < out[j].EdgeType
	})
	return out, stats
}

func normalizeConceptKey(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	lastUnderscore := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		case r == '_' || r == '-' || unicode.IsSpace(r):
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
			}
		default:
			// Drop punctuation/symbols.
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return ""
	}
	// Keep keys compact.
	if len(out) > 64 {
		out = out[:64]
		out = strings.Trim(out, "_")
	}
	return out
}

func filterChunkIDStrings(in []string, allowed map[string]bool) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, err := uuid.Parse(s)
		if err != nil || id == uuid.Nil {
			continue
		}
		s = id.String()
		if allowed != nil && len(allowed) > 0 && !allowed[s] {
			continue
		}
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func stringsOrExisting(existing, candidate string) string {
	if strings.TrimSpace(existing) != "" {
		return existing
	}
	return strings.TrimSpace(candidate)
}

func longerString(a, b string) string {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if len(b) > len(a) {
		return b
	}
	return a
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i]) != strings.TrimSpace(b[i]) {
			return false
		}
	}
	return true
}

func advisoryXactLock(tx *gorm.DB, namespace string, id uuid.UUID) error {
	if tx == nil || namespace == "" || id == uuid.Nil {
		return nil
	}
	key := advisoryKey64(namespace, id)
	return tx.Exec("SELECT pg_advisory_xact_lock(?)", key).Error
}

func advisoryKey64(namespace string, id uuid.UUID) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(namespace))
	_, _ = h.Write([]byte{':'})
	_, _ = h.Write([]byte(id.String()))
	return int64(h.Sum64())
}

func isUniqueViolation(err error, constraint string) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" {
			if strings.TrimSpace(constraint) == "" {
				return true
			}
			return strings.EqualFold(strings.TrimSpace(pgErr.ConstraintName), strings.TrimSpace(constraint))
		}
	}

	// Fallback: string match (covers wrapped errors that lose type info).
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "sqlstate 23505") {
		if strings.TrimSpace(constraint) == "" {
			return true
		}
		return strings.Contains(msg, strings.ToLower(strings.TrimSpace(constraint)))
	}
	return false
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

func dedupeConceptInventoryByKey(in []conceptInvItem) ([]conceptInvItem, int) {
	if len(in) == 0 {
		return nil, 0
	}

	seen := make(map[string]conceptInvItem, len(in))
	dups := 0
	for _, c := range in {
		k := strings.TrimSpace(c.Key)
		if k == "" {
			continue
		}
		c.Key = k

		existing, ok := seen[k]
		if !ok {
			seen[k] = c
			continue
		}

		dups++
		if strings.TrimSpace(existing.Name) == "" && strings.TrimSpace(c.Name) != "" {
			existing.Name = c.Name
		}
		if strings.TrimSpace(existing.ParentKey) == "" && strings.TrimSpace(c.ParentKey) != "" {
			existing.ParentKey = c.ParentKey
		}
		if len(strings.TrimSpace(existing.Summary)) < len(strings.TrimSpace(c.Summary)) {
			existing.Summary = c.Summary
		}
		if c.Importance > existing.Importance {
			existing.Importance = c.Importance
		}

		// Keep depth consistent with parent linkage when duplicates disagree.
		if strings.TrimSpace(existing.ParentKey) == "" {
			existing.Depth = 0
		} else {
			if c.Depth > existing.Depth {
				existing.Depth = c.Depth
			}
			if existing.Depth <= 0 {
				existing.Depth = 1
			}
		}

		existing.KeyPoints = dedupeStrings(append(existing.KeyPoints, c.KeyPoints...))
		existing.Aliases = dedupeStrings(append(existing.Aliases, c.Aliases...))
		existing.Citations = dedupeStrings(append(existing.Citations, c.Citations...))

		seen[k] = existing
	}

	out := make([]conceptInvItem, 0, len(seen))
	for _, v := range seen {
		out = append(out, v)
	}
	return out, dups
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

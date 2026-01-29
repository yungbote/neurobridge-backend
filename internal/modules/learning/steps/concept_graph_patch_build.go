package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/prompts"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"golang.org/x/sync/errgroup"
)

type ConceptGraphPatchBuildInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
}

type ConceptGraphPatchBuildOutput = ConceptGraphBuildOutput

func ConceptGraphPatchBuild(ctx context.Context, deps ConceptGraphBuildDeps, in ConceptGraphPatchBuildInput) (ConceptGraphPatchBuildOutput, error) {
	out := ConceptGraphPatchBuildOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Files == nil || deps.Chunks == nil || deps.Path == nil || deps.Concepts == nil || deps.Evidence == nil || deps.Edges == nil || deps.AI == nil || deps.Bootstrap == nil || deps.Saga == nil {
		return out, fmt.Errorf("concept_graph_patch_build: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("concept_graph_patch_build: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("concept_graph_patch_build: missing material_set_id")
	}
	if in.SagaID == uuid.Nil {
		return out, fmt.Errorf("concept_graph_patch_build: missing saga_id")
	}

	pathID, err := resolvePathID(ctx, deps.Bootstrap, in.OwnerUserID, in.MaterialSetID, in.PathID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	adaptiveEnabled := adaptiveParamsEnabledForStage("concept_graph_patch_build")
	if model := strings.TrimSpace(os.Getenv("CONCEPT_GRAPH_MODEL")); model != "" && deps.AI != nil {
		deps.AI = openai.WithModel(deps.AI, model)
	}
	signals := AdaptiveSignals{}
	if adaptiveEnabled {
		signals = loadAdaptiveSignals(ctx, deps.DB, in.MaterialSetID, pathID)
	}
	adaptiveParams := map[string]any{}
	defer func() {
		if deps.Log != nil && adaptiveEnabled && len(adaptiveParams) > 0 {
			deps.Log.Info("concept_graph_patch_build: adaptive params", "adaptive", adaptiveStageMeta("concept_graph_patch_build", adaptiveEnabled, signals, adaptiveParams))
		}
		out.Adaptive = adaptiveStageMeta("concept_graph_patch_build", adaptiveEnabled, signals, adaptiveParams)
	}()

	existing, err := deps.Concepts.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID)
	if err != nil {
		return out, err
	}
	if len(existing) == 0 {
		if deps.Log != nil {
			deps.Log.Warn("concept_graph_patch_build: no existing concepts; skipping", "path_id", pathID.String())
		}
		return out, nil
	}

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

	files, err := deps.Files.GetByMaterialSetID(dbctx.Context{Ctx: ctx}, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	if len(allowFiles) > 0 {
		filtered := filterMaterialFilesByAllowlist(files, allowFiles)
		if len(filtered) > 0 {
			files = filtered
		} else if deps.Log != nil {
			deps.Log.Warn("concept_graph_patch_build: intake filter excluded all files; ignoring filter", "path_id", pathID.String())
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
		return out, fmt.Errorf("concept_graph_patch_build: no chunks for material set")
	}

	var patchInputHash string
	if deps.Artifacts != nil && artifactCacheEnabled() {
		allowFileIDs := make([]string, 0, len(allowFiles))
		for id := range allowFiles {
			if id != uuid.Nil {
				allowFileIDs = append(allowFileIDs, id.String())
			}
		}
		sort.Strings(allowFileIDs)
		conceptFP := make([]map[string]any, 0, len(existing))
		for _, c := range existing {
			if c == nil || c.ID == uuid.Nil {
				continue
			}
			conceptFP = append(conceptFP, map[string]any{
				"id":         c.ID.String(),
				"key":        strings.TrimSpace(c.Key),
				"updated_at": c.UpdatedAt.UTC().Format(time.RFC3339Nano),
			})
		}
		sort.Slice(conceptFP, func(i, j int) bool {
			return stringFromAny(conceptFP[i]["key"]) < stringFromAny(conceptFP[j]["key"])
		})
		payload := map[string]any{
			"files":       filesFingerprint(files),
			"chunks":      chunksFingerprint(chunks),
			"concepts":    conceptFP,
			"allow_files": allowFileIDs,
			"intent_md":   intentMD,
			"env":         envSnapshot([]string{"CONCEPT_GRAPH_"}, []string{"OPENAI_MODEL"}),
		}
		if h, err := computeArtifactHash("concept_graph_patch_build", in.MaterialSetID, pathID, payload); err == nil {
			patchInputHash = h
		}
		if patchInputHash != "" {
			if _, hit, err := artifactCacheGet(ctx, deps.Artifacts, in.OwnerUserID, in.MaterialSetID, pathID, "concept_graph_patch_build", patchInputHash); err == nil && hit {
				if deps.Log != nil {
					deps.Log.Info("concept_graph_patch_build: cache hit; skipping", "path_id", pathID.String())
				}
				return out, nil
			}
		}
	}

	allowedChunkIDs := map[string]bool{}
	chunkByID := map[uuid.UUID]*types.MaterialChunk{}
	chunkEmbs := make([]chunkEmbedding, 0, len(chunks))
	for _, ch := range chunks {
		if ch == nil || ch.ID == uuid.Nil {
			continue
		}
		allowedChunkIDs[ch.ID.String()] = true
		chunkByID[ch.ID] = ch
		if emb, ok := decodeEmbedding(ch.Embedding); ok && len(emb) > 0 {
			chunkEmbs = append(chunkEmbs, chunkEmbedding{ID: ch.ID, Emb: emb})
		}
	}
	sort.Slice(chunkEmbs, func(i, j int) bool { return chunkEmbs[i].ID.String() < chunkEmbs[j].ID.String() })

	patchPerFileCeiling := envIntAllowZero("CONCEPT_GRAPH_PATCH_EXCERPTS_PER_FILE", 4)
	if patchPerFileCeiling < 0 {
		patchPerFileCeiling = 0
	}
	patchPerFile := patchPerFileCeiling
	if adaptiveEnabled {
		patchPerFile = clampIntCeiling(int(math.Round(signals.AvgPagesPerFile/20.0)), 2, patchPerFileCeiling)
	}
	adaptiveParams["CONCEPT_GRAPH_PATCH_EXCERPTS_PER_FILE"] = map[string]any{
		"actual":  patchPerFile,
		"ceiling": patchPerFileCeiling,
	}
	patchMaxChars := envIntAllowZero("CONCEPT_GRAPH_PATCH_EXCERPT_MAX_CHARS", 650)
	patchMaxCharsCeiling := patchMaxChars
	if patchMaxChars <= 0 {
		patchMaxChars = 650
		patchMaxCharsCeiling = patchMaxChars
	}
	patchMaxLines := envIntAllowZero("CONCEPT_GRAPH_PATCH_EXCERPT_MAX_LINES", 0)
	patchMaxLinesCeiling := patchMaxLines
	patchMaxTotalCeiling := envIntAllowZero("CONCEPT_GRAPH_PATCH_EXCERPT_MAX_TOTAL_CHARS", 12000)
	if patchMaxTotalCeiling < 0 {
		patchMaxTotalCeiling = 0
	}
	if patchMaxTotalCeiling == 0 && !adaptiveEnabled {
		patchMaxTotalCeiling = 12000
	}
	patchMaxTotal := patchMaxTotalCeiling
	if adaptiveEnabled {
		patchMaxChars = clampIntCeiling(adjustExcerptCharsByContentType(patchMaxChars, signals.ContentType), 200, patchMaxCharsCeiling)
		if patchMaxLines > 0 {
			patchMaxLines = clampIntCeiling(adjustExcerptLinesByContentType(patchMaxLines, signals.ContentType), 8, patchMaxLinesCeiling)
		}
		patchMaxTotal = clampIntCeiling(int(math.Round(float64(signals.PageCount)*200)), 6000, patchMaxTotalCeiling)
	}
	adaptiveParams["CONCEPT_GRAPH_PATCH_EXCERPT_MAX_CHARS"] = map[string]any{"actual": patchMaxChars, "ceiling": patchMaxCharsCeiling}
	adaptiveParams["CONCEPT_GRAPH_PATCH_EXCERPT_MAX_LINES"] = map[string]any{"actual": patchMaxLines, "ceiling": patchMaxLinesCeiling}
	adaptiveParams["CONCEPT_GRAPH_PATCH_EXCERPT_MAX_TOTAL_CHARS"] = map[string]any{
		"actual":  patchMaxTotal,
		"ceiling": patchMaxTotalCeiling,
	}
	patchExcerpts, patchChunkIDs := buildConceptGraphExcerpts(chunks, patchPerFile, patchMaxChars, patchMaxLines, patchMaxTotal)
	if strings.TrimSpace(patchExcerpts) == "" {
		return out, fmt.Errorf("concept_graph_patch_build: empty excerpts")
	}

	edgeMaxChars := envIntAllowZero("CONCEPT_GRAPH_EDGE_EXCERPT_MAX_CHARS", 700)
	edgeMaxCharsCeiling := edgeMaxChars
	if edgeMaxChars <= 0 {
		edgeMaxChars = 700
		edgeMaxCharsCeiling = edgeMaxChars
	}
	edgeMaxLines := envIntAllowZero("CONCEPT_GRAPH_EDGE_EXCERPT_MAX_LINES", 0)
	edgeMaxLinesCeiling := edgeMaxLines
	edgeMaxTotalCeiling := envIntAllowZero("CONCEPT_GRAPH_EDGE_EXCERPT_MAX_TOTAL_CHARS", 0)
	if edgeMaxTotalCeiling < 0 {
		edgeMaxTotalCeiling = 0
	}
	if edgeMaxTotalCeiling == 0 {
		edgeMaxTotalCeiling = patchMaxTotalCeiling
	}
	edgeMaxTotal := edgeMaxTotalCeiling
	if adaptiveEnabled {
		edgeMaxChars = clampIntCeiling(adjustExcerptCharsByContentType(edgeMaxChars, signals.ContentType), 200, edgeMaxCharsCeiling)
		if edgeMaxLines > 0 {
			edgeMaxLines = clampIntCeiling(adjustExcerptLinesByContentType(edgeMaxLines, signals.ContentType), 8, edgeMaxLinesCeiling)
		}
		edgeMaxTotal = clampIntCeiling(int(math.Round(float64(signals.PageCount)*200)), 6000, edgeMaxTotalCeiling)
	}
	adaptiveParams["CONCEPT_GRAPH_EDGE_EXCERPT_MAX_CHARS"] = map[string]any{"actual": edgeMaxChars, "ceiling": edgeMaxCharsCeiling}
	adaptiveParams["CONCEPT_GRAPH_EDGE_EXCERPT_MAX_LINES"] = map[string]any{"actual": edgeMaxLines, "ceiling": edgeMaxLinesCeiling}
	adaptiveParams["CONCEPT_GRAPH_EDGE_EXCERPT_MAX_TOTAL_CHARS"] = map[string]any{"actual": edgeMaxTotal, "ceiling": edgeMaxTotalCeiling}
	var edgeExcerpts string
	if edgeMaxChars == patchMaxChars && edgeMaxLines == patchMaxLines && edgeMaxTotal == patchMaxTotal {
		edgeExcerpts = patchExcerpts
	} else {
		edgeExcerpts, _ = buildConceptGraphExcerpts(
			chunks,
			patchPerFile,
			edgeMaxChars,
			edgeMaxLines,
			edgeMaxTotal,
		)
		if strings.TrimSpace(edgeExcerpts) == "" {
			edgeExcerpts = patchExcerpts
		}
	}

	// Existing concepts -> inventory form.
	idToKey := map[uuid.UUID]string{}
	for _, c := range existing {
		if c != nil && c.ID != uuid.Nil && strings.TrimSpace(c.Key) != "" {
			idToKey[c.ID] = strings.TrimSpace(c.Key)
		}
	}
	existingByKey := map[string]*types.Concept{}
	inventory := make([]conceptInvItem, 0, len(existing))
	for _, row := range existing {
		if row == nil || row.ID == uuid.Nil {
			continue
		}
		key := strings.TrimSpace(row.Key)
		if key == "" {
			continue
		}
		existingByKey[key] = row
		parentKey := ""
		if row.ParentID != nil {
			parentKey = strings.TrimSpace(idToKey[*row.ParentID])
		}

		meta := map[string]any{}
		if len(row.Metadata) > 0 && strings.TrimSpace(string(row.Metadata)) != "" && strings.TrimSpace(string(row.Metadata)) != "null" {
			_ = json.Unmarshal(row.Metadata, &meta)
		}
		aliases := dedupeStrings(stringSliceFromAny(meta["aliases"]))
		importance := intFromAny(meta["importance"], row.SortIndex)
		var keyPoints []string
		if len(row.KeyPoints) > 0 {
			_ = json.Unmarshal(row.KeyPoints, &keyPoints)
		}

		inventory = append(inventory, conceptInvItem{
			Key:        key,
			Name:       strings.TrimSpace(row.Name),
			ParentKey:  parentKey,
			Depth:      row.Depth,
			Summary:    strings.TrimSpace(row.Summary),
			KeyPoints:  dedupeStrings(keyPoints),
			Aliases:    aliases,
			Importance: importance,
			Citations:  nil,
		})
	}

	conceptsJSON := conceptsJSONForDelta(inventory)

	// Quick probe: decide whether we need a full patch.
	var probeCoverage conceptCoverage
	var probeNew []conceptInvItem
	if deps.AI != nil {
		if p, err := prompts.Build(prompts.PromptConceptInventoryDelta, prompts.Input{
			PathIntentMD: intentMD,
			ConceptsJSON: conceptsJSON,
			Excerpts:     patchExcerpts,
		}); err == nil {
			timer := llmTimer(deps.Log, "concept_inventory_delta_probe", map[string]any{
				"stage":         "concept_graph_patch_build",
				"path_id":       pathID.String(),
				"excerpt_chars": len(patchExcerpts),
			})
			if obj, err := deps.AI.GenerateJSON(ctx, p.System, p.User, p.SchemaName, p.Schema); err == nil {
				timer(err)
				if nc, cov, perr := parseConceptInventoryDelta(obj); perr == nil {
					probeNew = nc
					probeCoverage = cov
				}
			} else {
				timer(err)
			}
		}
	}

	if !envBool("CONCEPT_GRAPH_PATCH_FORCE", false) {
		minConf := envFloatAllowZero("CONCEPT_GRAPH_PATCH_SKIP_MIN_CONF", 0.75)
		if adaptiveEnabled {
			minConf = clamp01(adjustThresholdByContentType("CONCEPT_GRAPH_PATCH_SKIP_MIN_CONF", minConf, signals.ContentType))
		}
		adaptiveParams["CONCEPT_GRAPH_PATCH_SKIP_MIN_CONF"] = map[string]any{"actual": minConf}
		maxMissing := envIntAllowZero("CONCEPT_GRAPH_PATCH_SKIP_MAX_MISSING_TOPICS", 0)
		maxMissingCeiling := maxMissing
		if adaptiveEnabled {
			maxMissing = adaptiveFromRatio(signals.ConceptCount, 0.02, 2, maxMissingCeiling)
		}
		// 0 means: skip only when there are zero missing topics (patch should run if any missing topics exist).
		if maxMissing < 0 {
			maxMissing = 0
		}
		adaptiveParams["CONCEPT_GRAPH_PATCH_SKIP_MAX_MISSING_TOPICS"] = map[string]any{"actual": maxMissing, "ceiling": maxMissingCeiling}
		if probeCoverage.Confidence >= minConf && len(probeCoverage.MissingTopics) <= maxMissing && len(probeNew) == 0 {
			if deps.Log != nil {
				deps.Log.Info("concept_graph_patch_build: coverage high; skipping patch", "path_id", pathID.String(), "confidence", probeCoverage.Confidence)
			}
			if patchInputHash != "" && deps.Artifacts != nil && artifactCacheEnabled() {
				_ = artifactCacheUpsert(ctx, deps.Artifacts, &types.LearningArtifact{
					OwnerUserID:   in.OwnerUserID,
					MaterialSetID: in.MaterialSetID,
					PathID:        pathID,
					ArtifactType:  "concept_graph_patch_build",
					InputHash:     patchInputHash,
					Version:       artifactHashVersion,
					Metadata: marshalMeta(map[string]any{
						"skipped":    true,
						"confidence": probeCoverage.Confidence,
					}),
				})
			}
			return out, nil
		}
	}

	conceptsOut := inventory
	if len(probeNew) > 0 {
		conceptsOut = append(conceptsOut, probeNew...)
	}
	conceptsOut, _ = normalizeConceptInventory(conceptsOut, allowedChunkIDs)
	conceptsOut, _ = dedupeConceptInventoryByKey(conceptsOut)
	if len(conceptsOut) == 0 {
		return out, fmt.Errorf("concept_graph_patch_build: no concepts to patch")
	}

	// Coverage completion (full delta passes for patch).
	coverageInput := conceptCoverageInput{
		PathID:             pathID,
		MaterialSetID:      in.MaterialSetID,
		IntentMD:           intentMD,
		Chunks:             chunks,
		ChunkByID:          chunkByID,
		ChunkEmbs:          chunkEmbs,
		AllowedChunkIDs:    allowedChunkIDs,
		InitialChunkIDs:    patchChunkIDs,
		InitialCoverage:    probeCoverage,
		Concepts:           conceptsOut,
		MaterialFileFilter: allowFiles,
		Passes:             envIntAllowZero("CONCEPT_GRAPH_PATCH_PASSES", 2),
		ExtraPerFile:       envIntAllowZero("CONCEPT_GRAPH_PATCH_COVERAGE_EXCERPTS_PER_FILE", 4),
		ExtraMaxChars:      envIntAllowZero("CONCEPT_GRAPH_PATCH_COVERAGE_EXCERPT_MAX_CHARS", 650),
		ExtraMaxTotal:      envIntAllowZero("CONCEPT_GRAPH_PATCH_COVERAGE_EXCERPT_MAX_TOTAL_CHARS", 20000),
		TargetedOnly:       envBool("CONCEPT_GRAPH_PATCH_TARGETED_ONLY", true),
		AdaptiveEnabled:    adaptiveEnabled,
		Signals:            signals,
		Stage:              "concept_graph_patch_build",
	}
	patchCoveragePassesCeiling := envIntAllowZero("CONCEPT_GRAPH_PATCH_PASSES", 2)
	patchCoveragePerFileCeiling := envIntAllowZero("CONCEPT_GRAPH_PATCH_COVERAGE_EXCERPTS_PER_FILE", 4)
	patchCoverageMaxCharsCeiling := envIntAllowZero("CONCEPT_GRAPH_PATCH_COVERAGE_EXCERPT_MAX_CHARS", 650)
	patchCoverageMaxTotalCeiling := envIntAllowZero("CONCEPT_GRAPH_PATCH_COVERAGE_EXCERPT_MAX_TOTAL_CHARS", 20000)
	if adaptiveEnabled {
		coverageInput.Passes = adaptiveFromRatio(signals.PageCount, 1.0/50.0, 1, patchCoveragePassesCeiling)
		coverageInput.ExtraPerFile = clampIntCeiling(int(math.Round(signals.AvgPagesPerFile/20.0)), 2, patchCoveragePerFileCeiling)
		maxChars := coverageInput.ExtraMaxChars
		if maxChars <= 0 {
			maxChars = patchCoverageMaxCharsCeiling
		}
		if maxChars <= 0 {
			maxChars = 650
			patchCoverageMaxCharsCeiling = maxChars
		}
		coverageInput.ExtraMaxChars = clampIntCeiling(adjustExcerptCharsByContentType(maxChars, signals.ContentType), 200, patchCoverageMaxCharsCeiling)
		coverageInput.ExtraMaxTotal = clampIntCeiling(int(math.Round(float64(signals.PageCount)*200)), 6000, patchCoverageMaxTotalCeiling)
	}
	adaptiveParams["CONCEPT_GRAPH_PATCH_PASSES"] = map[string]any{"actual": coverageInput.Passes, "ceiling": patchCoveragePassesCeiling}
	adaptiveParams["CONCEPT_GRAPH_PATCH_COVERAGE_EXCERPTS_PER_FILE"] = map[string]any{"actual": coverageInput.ExtraPerFile, "ceiling": patchCoveragePerFileCeiling}
	adaptiveParams["CONCEPT_GRAPH_PATCH_COVERAGE_EXCERPT_MAX_CHARS"] = map[string]any{"actual": coverageInput.ExtraMaxChars, "ceiling": patchCoverageMaxCharsCeiling}
	adaptiveParams["CONCEPT_GRAPH_PATCH_COVERAGE_EXCERPT_MAX_TOTAL_CHARS"] = map[string]any{"actual": coverageInput.ExtraMaxTotal, "ceiling": patchCoverageMaxTotalCeiling}
	coverageResult := completeConceptCoverage(ctx, deps, coverageInput)
	conceptsOut = coverageResult.Concepts
	for k, v := range coverageResult.AdaptiveParams {
		adaptiveParams[k] = v
	}

	conceptsOut, _ = normalizeConceptInventory(conceptsOut, allowedChunkIDs)
	conceptsOut, _ = dedupeConceptInventoryByKey(conceptsOut)

	newItems := make([]conceptInvItem, 0)
	for _, c := range conceptsOut {
		if strings.TrimSpace(c.Key) == "" {
			continue
		}
		if _, ok := existingByKey[c.Key]; !ok {
			newItems = append(newItems, c)
		}
	}
	if len(newItems) == 0 {
		if deps.Log != nil {
			deps.Log.Info("concept_graph_patch_build: no new concepts discovered", "path_id", pathID.String())
		}
		return out, nil
	}

	// ---- Generate edges for full concept set ----
	conceptsJSONBytes, _ := json.Marshal(map[string]any{"concepts": conceptsOut})
	edgesPrompt, err := prompts.Build(prompts.PromptConceptEdges, prompts.Input{
		ConceptsJSON: string(conceptsJSONBytes),
		Excerpts:     edgeExcerpts,
		PathIntentMD: intentMD,
	})
	if err != nil {
		return out, err
	}
	timer := llmTimer(deps.Log, "concept_edges", map[string]any{
		"stage":         "concept_graph_patch_build",
		"path_id":       pathID.String(),
		"concept_count": len(conceptsOut),
		"excerpt_chars": len(edgeExcerpts),
	})
	edgesObj, err := deps.AI.GenerateJSON(ctx, edgesPrompt.System, edgesPrompt.User, edgesPrompt.SchemaName, edgesPrompt.Schema)
	timer(err)
	if err != nil {
		return out, err
	}
	edgesOut := parseConceptEdges(edgesObj)
	edgesOut, _ = normalizeConceptEdges(edgesOut, conceptsOut, allowedChunkIDs)

	// ---- Embed new concepts for canonical matching + vector upsert ----
	embedBatchSize := envIntAllowZero("CONCEPT_GRAPH_EMBED_BATCH_SIZE", 128)
	if embedBatchSize <= 0 {
		embedBatchSize = 64
	}
	embedConc := envIntAllowZero("CONCEPT_GRAPH_EMBED_CONCURRENCY", 64)
	if embedConc < 1 {
		embedConc = 1
	}
	conceptDocs := make([]string, 0, len(newItems))
	for _, c := range newItems {
		doc := strings.TrimSpace(c.Name + "\n" + c.Summary + "\n" + strings.Join(c.KeyPoints, "\n"))
		if doc == "" {
			doc = c.Key
		}
		conceptDocs = append(conceptDocs, doc)
	}
	if len(conceptDocs) == 0 {
		return out, nil
	}

	embs := make([][]float32, len(conceptDocs))
	eg, egctx := errgroup.WithContext(ctx)
	eg.SetLimit(embedConc)
	for start := 0; start < len(conceptDocs); start += embedBatchSize {
		start := start
		end := start + embedBatchSize
		if end > len(conceptDocs) {
			end = len(conceptDocs)
		}
		eg.Go(func() error {
			timer := llmTimer(deps.Log, "concept_embeddings", map[string]any{
				"stage":       "concept_graph_patch_build",
				"path_id":     pathID.String(),
				"batch_size":  end - start,
				"batch_start": start,
			})
			v, err := deps.AI.Embed(egctx, conceptDocs[start:end])
			timer(err)
			if err != nil {
				return err
			}
			if len(v) != end-start {
				return fmt.Errorf("concept_graph_patch_build: embedding count mismatch (got %d want %d)", len(v), end-start)
			}
			for i := range v {
				embs[start+i] = v[i]
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return out, err
	}

	semanticMatchByKey, semanticParams := semanticMatchCanonicalConcepts(ctx, deps, newItems, embs, signals, signals.ContentType, adaptiveEnabled, nil)
	for k, v := range semanticParams {
		adaptiveParams[k] = v
	}

	// ---- Persist new concepts + edges (single tx) ----
	type conceptRow struct {
		Row *types.Concept
		Emb []float32
	}
	rows := make([]conceptRow, 0, len(newItems))
	keyToID := map[string]uuid.UUID{}
	for key, row := range existingByKey {
		if row != nil && row.ID != uuid.Nil {
			keyToID[key] = row.ID
		}
	}

	for i := range newItems {
		id := uuid.New()
		keyToID[newItems[i].Key] = id
		meta := map[string]any{
			"aliases":    newItems[i].Aliases,
			"importance": newItems[i].Importance,
		}
		rows = append(rows, conceptRow{
			Row: &types.Concept{
				ID:        id,
				Scope:     "path",
				ScopeID:   &pathID,
				ParentID:  nil, // set after insert
				Depth:     newItems[i].Depth,
				SortIndex: newItems[i].Importance,
				Key:       newItems[i].Key,
				Name:      newItems[i].Name,
				Summary:   newItems[i].Summary,
				KeyPoints: datatypes.JSON(mustJSON(newItems[i].KeyPoints)),
				VectorID:  "concept:" + id.String(),
				Metadata:  datatypes.JSON(mustJSON(meta)),
			},
			Emb: embs[i],
		})
	}

	ns := index.ConceptsNamespace("path", &pathID)
	pineconeBatchSize := envIntAllowZero("CONCEPT_GRAPH_PINECONE_BATCH_SIZE", 64)
	if pineconeBatchSize <= 0 {
		pineconeBatchSize = 64
	}

	txErr := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		if err := advisoryXactLock(tx, "concept_graph_build", pathID); err != nil {
			return err
		}

		toCreate := make([]*types.Concept, 0, len(rows))
		for _, r := range rows {
			toCreate = append(toCreate, r.Row)
		}
		if _, err := deps.Concepts.Create(dbc, toCreate); err != nil {
			return err
		}
		out.ConceptsMade = len(toCreate)

		for _, c := range newItems {
			if strings.TrimSpace(c.ParentKey) == "" {
				continue
			}
			childID := keyToID[c.Key]
			parentID := keyToID[c.ParentKey]
			if childID == uuid.Nil || parentID == uuid.Nil {
				continue
			}
			if err := deps.Concepts.UpdateFields(dbc, childID, map[string]any{"parent_id": parentID}); err != nil {
				return err
			}
		}

		evRows := make([]*types.ConceptEvidence, 0)
		for _, c := range newItems {
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
		return out, txErr
	}

	// Canonicalize new concepts (best-effort).
	if deps.Concepts != nil && len(rows) > 0 {
		pathConcepts := make([]*types.Concept, 0, len(rows))
		for _, r := range rows {
			if r.Row != nil && r.Row.ID != uuid.Nil {
				pathConcepts = append(pathConcepts, r.Row)
			}
		}
		if len(pathConcepts) > 0 {
			_ = deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				dbc := dbctx.Context{Ctx: ctx, Tx: tx}
				_ = advisoryXactLock(tx, "concept_canonicalize", pathID)
				_, err := canonicalizePathConcepts(dbc, tx, deps.Concepts, pathConcepts, semanticMatchByKey)
				return err
			})
		}
	}

	// Upsert vectors (best-effort).
	if deps.Vec != nil {
		pineconeConc := envIntAllowZero("CONCEPT_GRAPH_PINECONE_CONCURRENCY", 32)
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
					if deps.Log != nil {
						deps.Log.Warn("pinecone upsert failed (continuing)", "namespace", ns, "err", err.Error())
					}
					return nil
				}
				atomic.AddInt32(&batches, 1)
				return nil
			})
		}
		_ = g.Wait()
		out.PineconeBatches = int(atomic.LoadInt32(&batches))

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
			if err := deps.Vec.Upsert(ctx, globalNS, globalVectors); err != nil && deps.Log != nil {
				deps.Log.Warn("pinecone global concept upsert failed (continuing)", "namespace", globalNS, "err", err.Error())
			}
		}
	}

	if deps.Graph != nil {
		if err := syncPathConceptGraphToNeo4j(ctx, deps, pathID); err != nil && deps.Log != nil {
			deps.Log.Warn("neo4j concept graph sync failed (continuing)", "error", err, "path_id", pathID.String())
		}
	}

	if patchInputHash != "" && deps.Artifacts != nil && artifactCacheEnabled() {
		_ = artifactCacheUpsert(ctx, deps.Artifacts, &types.LearningArtifact{
			OwnerUserID:   in.OwnerUserID,
			MaterialSetID: in.MaterialSetID,
			PathID:        pathID,
			ArtifactType:  "concept_graph_patch_build",
			InputHash:     patchInputHash,
			Version:       artifactHashVersion,
			Metadata: marshalMeta(map[string]any{
				"concepts_made": out.ConceptsMade,
				"edges_made":    out.EdgesMade,
				"batches":       out.PineconeBatches,
			}),
		})
	}

	return out, nil
}

package steps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"os"
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
	DB       *gorm.DB
	Log      *logger.Logger
	Files    repos.MaterialFileRepo
	FileSigs repos.MaterialFileSignatureRepo
	Chunks   repos.MaterialChunkRepo
	Path     repos.PathRepo

	Concepts repos.ConceptRepo
	Evidence repos.ConceptEvidenceRepo
	Edges    repos.ConceptEdgeRepo

	Graph *neo4jdb.Client

	AI        openai.Client
	Vec       pc.VectorStore
	Saga      services.SagaService
	Bootstrap services.LearningBuildBootstrapService
	Artifacts repos.LearningArtifactRepo
}

type ConceptGraphBuildInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
	Mode          string
	Report        func(stage string, pct int, message string)
}

type ConceptGraphBuildOutput struct {
	PathID          uuid.UUID      `json:"path_id"`
	ConceptsMade    int            `json:"concepts_made"`
	EdgesMade       int            `json:"edges_made"`
	PineconeBatches int            `json:"pinecone_batches"`
	Adaptive        map[string]any `json:"adaptive,omitempty"`
}

func ConceptGraphBuild(ctx context.Context, deps ConceptGraphBuildDeps, in ConceptGraphBuildInput) (ConceptGraphBuildOutput, error) {
	out := ConceptGraphBuildOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Files == nil || deps.FileSigs == nil || deps.Chunks == nil || deps.Path == nil || deps.Concepts == nil || deps.Evidence == nil || deps.Edges == nil || deps.AI == nil || deps.Bootstrap == nil || deps.Saga == nil {
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

	reporter := newProgressReporter("concept_graph", in.Report, 2, 2*time.Second)
	reporter.Update(2, "Preparing concept graph")

	adaptiveEnabled := adaptiveParamsEnabledForStage("concept_graph_build")
	signals := AdaptiveSignals{}
	if adaptiveEnabled {
		signals = loadAdaptiveSignals(ctx, deps.DB, in.MaterialSetID, pathID)
	}
	adaptiveParams := map[string]any{}
	defer func() {
		if deps.Log != nil && adaptiveEnabled && len(adaptiveParams) > 0 {
			deps.Log.Info("concept_graph_build: adaptive params", "adaptive", adaptiveStageMeta("concept_graph_build", adaptiveEnabled, signals, adaptiveParams))
		}
		out.Adaptive = adaptiveStageMeta("concept_graph_build", adaptiveEnabled, signals, adaptiveParams)
	}()
	mode := strings.TrimSpace(strings.ToLower(in.Mode))
	fastMode := mode == "fast"
	if mode == "" {
		fastMode = envBool("CONCEPT_GRAPH_FAST_MODE", false)
	}
	if model := strings.TrimSpace(os.Getenv("CONCEPT_GRAPH_MODEL")); model != "" && deps.AI != nil {
		deps.AI = openai.WithModel(deps.AI, model)
	}

	existing, err := deps.Concepts.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID)
	if err != nil {
		return out, err
	}
	hasExisting := len(existing) > 0

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
	reporter.Update(4, fmt.Sprintf("Loaded %d files", len(files)))
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
	reporter.Update(6, fmt.Sprintf("Loaded %d chunks", len(chunks)))

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
	embByChunk := map[uuid.UUID][]float32{}
	for _, ce := range chunkEmbs {
		if ce.ID != uuid.Nil && len(ce.Emb) > 0 {
			embByChunk[ce.ID] = ce.Emb
		}
	}

	// Optional enrichment: formula extraction before concept graph prompts.
	if params := extractFormulasAndPersist(ctx, deps, chunks, allowedChunkIDs, signals, adaptiveEnabled); len(params) > 0 {
		for k, v := range params {
			adaptiveParams[k] = v
		}
	}
	reporter.Update(8, "Extracted formulas")

	type crossDocSectionResult struct {
		JSON   string
		Params map[string]any
	}
	crossDocCh := make(chan crossDocSectionResult, 1)
	go func() {
		jsonOut, _, params := buildCrossDocSectionGraph(ctx, deps, files, chunks, embByChunk, signals, adaptiveEnabled)
		crossDocCh <- crossDocSectionResult{JSON: jsonOut, Params: params}
	}()
	var crossDocSectionsJSON string
	crossDocLoaded := false
	awaitCrossDocSections := func() string {
		if crossDocLoaded {
			return crossDocSectionsJSON
		}
		res := <-crossDocCh
		crossDocSectionsJSON = res.JSON
		for k, v := range res.Params {
			adaptiveParams[k] = v
		}
		crossDocLoaded = true
		return crossDocSectionsJSON
	}

	perFileCeiling := envIntAllowZero("CONCEPT_GRAPH_EXCERPTS_PER_FILE", 14)
	if perFileCeiling < 0 {
		perFileCeiling = 0
	}
	perFile := perFileCeiling
	excerptMaxChars := envIntAllowZero("CONCEPT_GRAPH_EXCERPT_MAX_CHARS", 700)
	excerptMaxCharsCeiling := excerptMaxChars
	if excerptMaxChars <= 0 {
		excerptMaxChars = 700
		excerptMaxCharsCeiling = excerptMaxChars
	}
	excerptMaxLines := envIntAllowZero("CONCEPT_GRAPH_EXCERPT_MAX_LINES", 0)
	excerptMaxLinesCeiling := excerptMaxLines
	excerptMaxTotalCeiling := envIntAllowZero("CONCEPT_GRAPH_EXCERPT_MAX_TOTAL_CHARS", 45000)
	if excerptMaxTotalCeiling < 0 {
		excerptMaxTotalCeiling = 0
	}
	excerptMaxTotal := excerptMaxTotalCeiling
	if adaptiveEnabled && excerptMaxTotalCeiling != 0 {
		perFile = clampIntCeiling(int(math.Round(signals.AvgPagesPerFile/10.0)), 2, perFileCeiling)
		excerptMaxChars = clampIntCeiling(adjustExcerptCharsByContentType(excerptMaxChars, signals.ContentType), 200, excerptMaxCharsCeiling)
		if excerptMaxLines > 0 {
			excerptMaxLines = clampIntCeiling(adjustExcerptLinesByContentType(excerptMaxLines, signals.ContentType), 8, excerptMaxLinesCeiling)
		}
		excerptMaxTotal = clampIntCeiling(int(math.Round(float64(signals.PageCount)*250)), 8000, excerptMaxTotalCeiling)
	} else if adaptiveEnabled {
		perFile = clampIntCeiling(int(math.Round(signals.AvgPagesPerFile/10.0)), 2, perFileCeiling)
		excerptMaxChars = clampIntCeiling(adjustExcerptCharsByContentType(excerptMaxChars, signals.ContentType), 200, excerptMaxCharsCeiling)
		if excerptMaxLines > 0 {
			excerptMaxLines = clampIntCeiling(adjustExcerptLinesByContentType(excerptMaxLines, signals.ContentType), 8, excerptMaxLinesCeiling)
		}
	}
	adaptiveParams["CONCEPT_GRAPH_EXCERPTS_PER_FILE"] = map[string]any{
		"actual":  perFile,
		"ceiling": perFileCeiling,
	}
	adaptiveParams["CONCEPT_GRAPH_EXCERPT_MAX_TOTAL_CHARS"] = map[string]any{
		"actual":  excerptMaxTotal,
		"ceiling": excerptMaxTotalCeiling,
	}
	adaptiveParams["CONCEPT_GRAPH_EXCERPT_MAX_CHARS"] = map[string]any{"actual": excerptMaxChars, "ceiling": excerptMaxCharsCeiling}
	adaptiveParams["CONCEPT_GRAPH_EXCERPT_MAX_LINES"] = map[string]any{"actual": excerptMaxLines, "ceiling": excerptMaxLinesCeiling}
	excerpts, excerptChunkIDs := buildConceptGraphExcerpts(
		chunks,
		perFile,
		excerptMaxChars,
		excerptMaxLines,
		excerptMaxTotal,
	)
	if strings.TrimSpace(excerpts) == "" {
		return out, fmt.Errorf("concept_graph_build: empty excerpts")
	}
	reporter.Update(9, fmt.Sprintf("Built excerpts (%d chunks)", len(excerptChunkIDs)))
	edgeMaxChars := envIntAllowZero("CONCEPT_GRAPH_EDGE_EXCERPT_MAX_CHARS", 700)
	edgeMaxCharsCeiling := edgeMaxChars
	if edgeMaxChars <= 0 {
		edgeMaxChars = 700
		edgeMaxCharsCeiling = edgeMaxChars
	}
	edgeMaxLines := envIntAllowZero("CONCEPT_GRAPH_EDGE_EXCERPT_MAX_LINES", 0)
	edgeMaxLinesCeiling := edgeMaxLines
	edgeMaxTotalCeiling := envIntAllowZero("CONCEPT_GRAPH_EDGE_EXCERPT_MAX_TOTAL_CHARS", excerptMaxTotalCeiling)
	if edgeMaxTotalCeiling < 0 {
		edgeMaxTotalCeiling = 0
	}
	edgeMaxTotal := edgeMaxTotalCeiling
	if adaptiveEnabled {
		edgeMaxChars = clampIntCeiling(adjustExcerptCharsByContentType(edgeMaxChars, signals.ContentType), 200, edgeMaxCharsCeiling)
		if edgeMaxLines > 0 {
			edgeMaxLines = clampIntCeiling(adjustExcerptLinesByContentType(edgeMaxLines, signals.ContentType), 8, edgeMaxLinesCeiling)
		}
		if edgeMaxTotalCeiling != 0 {
			edgeMaxTotal = clampIntCeiling(int(math.Round(float64(signals.PageCount)*200)), 6000, edgeMaxTotalCeiling)
		}
	}
	adaptiveParams["CONCEPT_GRAPH_EDGE_EXCERPT_MAX_CHARS"] = map[string]any{"actual": edgeMaxChars, "ceiling": edgeMaxCharsCeiling}
	adaptiveParams["CONCEPT_GRAPH_EDGE_EXCERPT_MAX_LINES"] = map[string]any{"actual": edgeMaxLines, "ceiling": edgeMaxLinesCeiling}
	adaptiveParams["CONCEPT_GRAPH_EDGE_EXCERPT_MAX_TOTAL_CHARS"] = map[string]any{"actual": edgeMaxTotal, "ceiling": edgeMaxTotalCeiling}
	var edgeExcerpts string
	if edgeMaxChars == excerptMaxChars && edgeMaxLines == excerptMaxLines && edgeMaxTotal == excerptMaxTotal {
		edgeExcerpts = excerpts
	} else {
		edgeExcerpts, _ = buildConceptGraphExcerpts(
			chunks,
			perFile,
			edgeMaxChars,
			edgeMaxLines,
			edgeMaxTotal,
		)
		if strings.TrimSpace(edgeExcerpts) == "" {
			edgeExcerpts = excerpts
		}
	}

	// ---- Optional: seed concepts from file signatures ----
	sigByFile := map[uuid.UUID]*types.MaterialFileSignature{}
	var (
		setSeedKeys []string
		setSeedMeta conceptSeedMeta
		sigsForHash []*types.MaterialFileSignature
	)
	if deps.FileSigs != nil {
		if sigs, err := deps.FileSigs.GetByMaterialFileIDs(dbctx.Context{Ctx: ctx}, fileIDs); err == nil && len(sigs) > 0 {
			sigsForHash = sigs
			for _, sig := range sigs {
				if sig != nil && sig.MaterialFileID != uuid.Nil {
					sigByFile[sig.MaterialFileID] = sig
				}
			}
			keys, meta := buildConceptSeedFromSignatures(sigs, signals, signals.ContentType, adaptiveEnabled)
			setSeedKeys = keys
			setSeedMeta = meta
			for k, v := range meta.Params {
				adaptiveParams[k] = v
			}
		}
	}

	var conceptInputHash string
	if deps.Artifacts != nil && artifactCacheEnabled() {
		allowFileIDs := make([]string, 0, len(allowFiles))
		for id := range allowFiles {
			if id != uuid.Nil {
				allowFileIDs = append(allowFileIDs, id.String())
			}
		}
		sort.Strings(allowFileIDs)
		payload := map[string]any{
			"files":       filesFingerprint(files),
			"chunks":      chunksFingerprint(chunks),
			"signatures":  signaturesFingerprint(sigsForHash),
			"allow_files": allowFileIDs,
			"intent_md":   intentMD,
			"mode":        mode,
			"env":         envSnapshot([]string{"CONCEPT_GRAPH_"}, []string{"OPENAI_MODEL"}),
		}
		if h, err := computeArtifactHash("concept_graph_build", in.MaterialSetID, pathID, payload); err == nil {
			conceptInputHash = h
		}
	}

	if hasExisting {
		if conceptInputHash != "" && deps.Artifacts != nil && artifactCacheEnabled() {
			if _, hit, err := artifactCacheGet(ctx, deps.Artifacts, in.OwnerUserID, in.MaterialSetID, pathID, "concept_graph_build", conceptInputHash); err == nil && hit {
				return out, nil
			}
		}
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

	// ---- Prompt: Concept inventory (parallel per-file) ----
	chunksByFile := map[uuid.UUID][]*types.MaterialChunk{}
	for _, ch := range chunks {
		if ch == nil || ch.MaterialFileID == uuid.Nil {
			continue
		}
		chunksByFile[ch.MaterialFileID] = append(chunksByFile[ch.MaterialFileID], ch)
	}

	fileCount := len(fileIDs)
	invTargets := 0
	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		if len(chunksByFile[f.ID]) == 0 {
			continue
		}
		invTargets++
	}
	if invTargets < 1 {
		invTargets = 1
	}
	const invStart = 10
	const invEnd = 35
	reporter.Update(invStart, fmt.Sprintf("Inventorying concepts (%d files)", invTargets))
	invPerFileCeiling := envIntAllowZero("CONCEPT_GRAPH_FILE_EXCERPTS_PER_FILE", perFile)
	if invPerFileCeiling <= 0 {
		invPerFileCeiling = perFile
	}
	invPerFile := invPerFileCeiling
	if adaptiveEnabled {
		invPerFile = clampIntCeiling(int(math.Round(signals.AvgPagesPerFile/25.0)), 2, invPerFileCeiling)
	}
	if fileCount > 1 {
		invPerFile = clampIntCeiling(int(math.Ceil(float64(invPerFile)/float64(fileCount))), 1, invPerFileCeiling)
	}
	invMaxTotal := excerptMaxTotal
	if invMaxTotal <= 0 {
		if adaptiveEnabled {
			invMaxTotal = clampIntCeiling(int(math.Round(signals.AvgPagesPerFile*200)), 4000, 14000)
		} else {
			invMaxTotal = 12000
		}
	} else if fileCount > 1 {
		invMaxTotal = int(math.Ceil(float64(invMaxTotal) / float64(fileCount)))
	}
	if invMaxTotal < 2000 {
		invMaxTotal = 2000
	}
	invConc := envIntAllowZero("CONCEPT_GRAPH_FILE_INVENTORY_CONCURRENCY", 24)
	if invConc < 1 {
		invConc = 1
	}
	adaptiveParams["CONCEPT_GRAPH_FILE_EXCERPTS_PER_FILE"] = map[string]any{
		"actual":  invPerFile,
		"ceiling": invPerFileCeiling,
	}
	adaptiveParams["CONCEPT_GRAPH_FILE_EXCERPT_MAX_TOTAL_CHARS"] = map[string]any{
		"actual": invMaxTotal,
	}
	adaptiveParams["CONCEPT_GRAPH_FILE_INVENTORY_CONCURRENCY"] = map[string]any{
		"actual": invConc,
	}

	type fileInv struct {
		Coverage conceptCoverage
		Concepts []conceptInvItem
	}
	var (
		allConcepts    []conceptInvItem
		missingUnion   []string
		confSum        float64
		confCount      int
		filesAttempted int
		filesSucceeded int
		filesFailed    int
		invErrs        []error
	)

	var invMu sync.Mutex
	gInv, gInvCtx := errgroup.WithContext(ctx)
	gInv.SetLimit(invConc)
	var invDone int32
	for _, f := range files {
		f := f
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		fchunks := chunksByFile[f.ID]
		if len(fchunks) == 0 {
			continue
		}
		filesAttempted++
		gInv.Go(func() error {
			defer func() {
				done := int(atomic.AddInt32(&invDone, 1))
				reporter.UpdateRange(done, invTargets, invStart, invEnd, fmt.Sprintf("Inventorying concepts %d/%d", done, invTargets))
			}()
			if gInvCtx.Err() != nil {
				return nil
			}
			ex, _ := buildConceptGraphExcerpts(fchunks, invPerFile, excerptMaxChars, excerptMaxLines, invMaxTotal)
			if strings.TrimSpace(ex) == "" {
				return nil
			}

			seedJSON := ""
			seedMetaFile := conceptSeedMeta{}
			if sig := sigByFile[f.ID]; sig != nil {
				fileSignals := signals
				fileSignals.FileCount = 1
				keys, meta := buildConceptSeedFromSignatures([]*types.MaterialFileSignature{sig}, fileSignals, signals.ContentType, adaptiveEnabled)
				seedMetaFile = meta
				if meta.Usable && len(keys) > 0 {
					if b, err := json.Marshal(map[string]any{"seed_concept_keys": keys, "seed_quality": meta}); err == nil {
						seedJSON = string(b)
					}
				}
			}

			buildInventory := func(seed string) (conceptCoverage, []conceptInvItem, error) {
				invPrompt, err := prompts.Build(prompts.PromptConceptInventory, prompts.Input{
					Excerpts:             ex,
					PathIntentMD:         intentMD,
					CrossDocSectionsJSON: "",
					SeedConceptKeysJSON:  seed,
				})
				if err != nil {
					return conceptCoverage{}, nil, err
				}
				timer := llmTimer(deps.Log, "concept_inventory", map[string]any{
					"stage":         "concept_graph_build",
					"scope":         "file",
					"file_id":       f.ID.String(),
					"path_id":       pathID.String(),
					"excerpt_chars": len(ex),
				})
				invObj, err := deps.AI.GenerateJSON(gInvCtx, invPrompt.System, invPrompt.User, invPrompt.SchemaName, invPrompt.Schema)
				timer(err)
				if err != nil {
					return conceptCoverage{}, nil, err
				}
				cov := parseConceptCoverage(invObj)
				concepts, err := parseConceptInventory(invObj)
				if err != nil {
					return cov, nil, err
				}
				if len(concepts) == 0 {
					return cov, nil, fmt.Errorf("concept_graph_build: file inventory returned 0 concepts")
				}
				return cov, concepts, nil
			}

			invCoverage, conceptsOut, err := buildInventory(seedJSON)
			if err != nil {
				if errors.Is(err, context.Canceled) && ctx.Err() != nil {
					return err
				}
				invMu.Lock()
				invErrs = append(invErrs, err)
				filesFailed++
				invMu.Unlock()
				return nil
			}
			if seedJSON != "" && seedMetaFile.Usable {
				fileSignals := signals
				fileSignals.FileCount = 1
				seedWeak, _ := conceptInventoryWeak(invCoverage, conceptsOut, seedMetaFile, fileSignals, signals.ContentType, adaptiveEnabled)
				if seedWeak {
					invCoverage, conceptsOut, err = buildInventory("")
					if err != nil {
						if errors.Is(err, context.Canceled) && ctx.Err() != nil {
							return err
						}
						invMu.Lock()
						invErrs = append(invErrs, err)
						filesFailed++
						invMu.Unlock()
						return nil
					}
				}
			}

			conceptsOut, _ = normalizeConceptInventory(conceptsOut, allowedChunkIDs)
			conceptsOut, _ = dedupeConceptInventoryByKey(conceptsOut)
			if len(conceptsOut) == 0 {
				invMu.Lock()
				filesFailed++
				invMu.Unlock()
				return nil
			}

			invMu.Lock()
			allConcepts = append(allConcepts, conceptsOut...)
			if len(invCoverage.MissingTopics) > 0 {
				missingUnion = append(missingUnion, invCoverage.MissingTopics...)
			}
			if invCoverage.Confidence > 0 {
				confSum += invCoverage.Confidence
				confCount++
			}
			filesSucceeded++
			invMu.Unlock()
			return nil
		})
	}
	if err := gInv.Wait(); err != nil {
		return out, err
	}
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	if len(invErrs) > 0 && deps.Log != nil {
		deps.Log.Warn("concept_graph_build: per-file inventory errors", "count", len(invErrs))
	}

	invCoverage := conceptCoverage{
		Confidence:    0,
		MissingTopics: dedupeStrings(missingUnion),
	}
	if confCount > 0 {
		invCoverage.Confidence = confSum / float64(confCount)
	}

	conceptsOut := allConcepts

	minSuccessRatio := envFloatAllowZero("CONCEPT_GRAPH_FILE_INVENTORY_MIN_SUCCESS_RATIO", 0.6)
	if minSuccessRatio <= 0 {
		minSuccessRatio = 0.6
	}
	if minSuccessRatio > 1 {
		minSuccessRatio = 1
	}
	adaptiveParams["CONCEPT_GRAPH_FILE_INVENTORY_MIN_SUCCESS_RATIO"] = map[string]any{"actual": minSuccessRatio}

	filesAttemptedSafe := maxInt(filesAttempted, 1)
	successRatio := float64(filesSucceeded) / float64(filesAttemptedSafe)
	weakInv, weakParams := conceptInventoryWeak(invCoverage, conceptsOut, setSeedMeta, signals, signals.ContentType, adaptiveEnabled)
	for k, v := range weakParams {
		adaptiveParams[k] = v
	}
	needsFallback := filesSucceeded == 0 || successRatio < minSuccessRatio || weakInv
	if needsFallback {
		reporter.Update(invEnd, "Running global concept inventory")
		if deps.Log != nil {
			deps.Log.Warn("concept_graph_build: per-file inventory weak; falling back to global inventory",
				"path_id", pathID.String(),
				"files_attempted", filesAttempted,
				"files_succeeded", filesSucceeded,
				"files_failed", filesFailed,
				"success_ratio", successRatio,
				"weak", weakInv,
			)
		}

		crossDocSectionsJSON = awaitCrossDocSections()

		seedJSON := ""
		if setSeedMeta.Usable && len(setSeedKeys) > 0 {
			if b, err := json.Marshal(map[string]any{"seed_concept_keys": setSeedKeys, "seed_quality": setSeedMeta}); err == nil {
				seedJSON = string(b)
			}
		}
		buildGlobalInventory := func(seed string) (conceptCoverage, []conceptInvItem, error) {
			invPrompt, err := prompts.Build(prompts.PromptConceptInventory, prompts.Input{
				Excerpts:             excerpts,
				PathIntentMD:         intentMD,
				CrossDocSectionsJSON: crossDocSectionsJSON,
				SeedConceptKeysJSON:  seed,
			})
			if err != nil {
				return conceptCoverage{}, nil, err
			}
			timer := llmTimer(deps.Log, "concept_inventory", map[string]any{
				"stage":         "concept_graph_build",
				"scope":         "global",
				"path_id":       pathID.String(),
				"excerpt_chars": len(excerpts),
			})
			invObj, err := deps.AI.GenerateJSON(ctx, invPrompt.System, invPrompt.User, invPrompt.SchemaName, invPrompt.Schema)
			timer(err)
			if err != nil {
				return conceptCoverage{}, nil, err
			}
			cov := parseConceptCoverage(invObj)
			concepts, err := parseConceptInventory(invObj)
			if err != nil {
				return cov, nil, err
			}
			if len(concepts) == 0 {
				return cov, nil, fmt.Errorf("concept_graph_build: global inventory returned 0 concepts")
			}
			return cov, concepts, nil
		}

		globalCoverage, globalConcepts, err := buildGlobalInventory(seedJSON)
		if err != nil {
			return out, err
		}
		if seedJSON != "" && setSeedMeta.Usable {
			seedWeak, _ := conceptInventoryWeak(globalCoverage, globalConcepts, setSeedMeta, signals, signals.ContentType, adaptiveEnabled)
			if seedWeak {
				globalCoverage, globalConcepts, err = buildGlobalInventory("")
				if err != nil {
					return out, err
				}
			}
		}
		conceptsOut = globalConcepts
		invCoverage = globalCoverage
	}

	if len(conceptsOut) == 0 {
		return out, fmt.Errorf("concept_graph_build: concept inventory returned 0 concepts")
	}
	reporter.Update(invEnd, fmt.Sprintf("Inventory complete (%d concepts)", len(conceptsOut)))

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
	coverageInput := conceptCoverageInput{
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
		AdaptiveEnabled:    adaptiveEnabled,
		Signals:            signals,
		Stage:              "concept_graph_build",
	}
	coverageInput.TargetedOnly = envBool("CONCEPT_GRAPH_COVERAGE_TARGETED_ONLY", true)
	if fastMode {
		fastPasses := envIntAllowZero("CONCEPT_GRAPH_FAST_COVERAGE_PASSES", 1)
		fastPassesCeiling := fastPasses
		if adaptiveEnabled {
			fastPasses = adaptiveFromRatio(signals.PageCount, 1.0/80.0, 1, fastPassesCeiling)
		}
		fastPerFile := envIntAllowZero("CONCEPT_GRAPH_FAST_COVERAGE_EXCERPTS_PER_FILE", 3)
		fastPerFileCeiling := fastPerFile
		if adaptiveEnabled {
			fastPerFile = clampIntCeiling(int(math.Round(signals.AvgPagesPerFile/25.0)), 1, fastPerFileCeiling)
		}
		fastMaxChars := envIntAllowZero("CONCEPT_GRAPH_FAST_COVERAGE_EXCERPT_MAX_CHARS", 650)
		fastMaxCharsCeiling := fastMaxChars
		if fastMaxChars <= 0 {
			fastMaxChars = 650
			fastMaxCharsCeiling = fastMaxChars
		}
		if adaptiveEnabled {
			fastMaxChars = clampIntCeiling(adjustExcerptCharsByContentType(fastMaxChars, signals.ContentType), 200, fastMaxCharsCeiling)
		}
		fastMaxTotalCeiling := envIntAllowZero("CONCEPT_GRAPH_FAST_COVERAGE_EXCERPT_MAX_TOTAL_CHARS", 18000)
		fastMaxTotal := fastMaxTotalCeiling
		if adaptiveEnabled {
			fastMaxTotal = clampIntCeiling(int(math.Round(float64(signals.PageCount)*150)), 6000, fastMaxTotalCeiling)
		}
		coverageInput.Passes = fastPasses
		coverageInput.ExtraPerFile = fastPerFile
		coverageInput.ExtraMaxChars = fastMaxChars
		coverageInput.ExtraMaxTotal = fastMaxTotal
		coverageInput.TargetedOnly = true
		adaptiveParams["CONCEPT_GRAPH_FAST_COVERAGE_PASSES"] = map[string]any{"actual": coverageInput.Passes, "ceiling": fastPassesCeiling}
		adaptiveParams["CONCEPT_GRAPH_FAST_COVERAGE_EXCERPTS_PER_FILE"] = map[string]any{"actual": coverageInput.ExtraPerFile, "ceiling": fastPerFileCeiling}
		adaptiveParams["CONCEPT_GRAPH_FAST_COVERAGE_EXCERPT_MAX_CHARS"] = map[string]any{"actual": coverageInput.ExtraMaxChars, "ceiling": fastMaxCharsCeiling}
		adaptiveParams["CONCEPT_GRAPH_FAST_COVERAGE_EXCERPT_MAX_TOTAL_CHARS"] = map[string]any{"actual": coverageInput.ExtraMaxTotal, "ceiling": fastMaxTotalCeiling}
	}
	const coverageStart = 35
	const coverageEnd = 55
	reporter.Update(coverageStart, "Expanding coverage")
	coverageInput.Progress = func(pct int, msg string) {
		reporter.Update(pct, msg)
	}
	coverageInput.ProgressStart = coverageStart
	coverageInput.ProgressEnd = coverageEnd
	coverageResult := completeConceptCoverage(ctx, deps, coverageInput)
	conceptsOut = coverageResult.Concepts
	for k, v := range coverageResult.AdaptiveParams {
		adaptiveParams[k] = v
	}
	reporter.Update(coverageEnd, fmt.Sprintf("Coverage complete (%d concepts)", len(conceptsOut)))

	// ---- Assumed knowledge + concept alignment (parallel, deterministic) ----
	conceptMetaByKey := map[string]map[string]any{}
	type assumedResult struct {
		Concepts []conceptInvItem
		Meta     map[string]map[string]any
		Added    int
		Err      error
	}
	type alignResult struct {
		Alignment conceptAlignment
		Err       error
	}
	baseConcepts := make([]conceptInvItem, len(conceptsOut))
	copy(baseConcepts, conceptsOut)

	assumedCh := make(chan assumedResult, 1)
	alignCh := make(chan alignResult, 1)

	assumedEnabled := deps.AI != nil && strings.TrimSpace(excerpts) != "" && len(baseConcepts) > 0
	alignEnabled := deps.AI != nil && len(baseConcepts) > 0
	reporter.Update(56, "Assumed knowledge + alignment")

	if assumedEnabled {
		go func() {
			res := assumedResult{Concepts: baseConcepts, Meta: map[string]map[string]any{}}
			conceptsJSONBytes, _ := json.Marshal(map[string]any{"concepts": baseConcepts})
			assumedPrompt, err := prompts.Build(prompts.PromptAssumedKnowledge, prompts.Input{
				ConceptsJSON: string(conceptsJSONBytes),
				Excerpts:     excerpts,
				PathIntentMD: intentMD,
			})
			if err != nil {
				res.Err = err
				assumedCh <- res
				return
			}
			timer := llmTimer(deps.Log, "assumed_knowledge", map[string]any{
				"stage":         "concept_graph_build",
				"path_id":       pathID.String(),
				"concept_count": len(baseConcepts),
				"excerpt_chars": len(excerpts),
				"content_type":  signals.ContentType,
			})
			assumedObj, err := deps.AI.GenerateJSON(ctx, assumedPrompt.System, assumedPrompt.User, assumedPrompt.SchemaName, assumedPrompt.Schema)
			timer(err)
			if err != nil {
				res.Err = err
				assumedCh <- res
				return
			}
			assumed := parseAssumedKnowledge(assumedObj)
			if len(assumed.Assumed) == 0 {
				assumedCh <- res
				return
			}
			byKey := map[string]conceptInvItem{}
			for _, c := range baseConcepts {
				if strings.TrimSpace(c.Key) != "" {
					byKey[c.Key] = c
				}
			}
			added := 0
			for _, a := range assumed.Assumed {
				key := normalizeConceptKey(a.Key)
				if key == "" {
					continue
				}
				name := strings.TrimSpace(a.Name)
				if name == "" {
					name = key
				}
				reqs := make([]string, 0, len(a.RequiredBy))
				for _, rk := range a.RequiredBy {
					nk := normalizeConceptKey(rk)
					if nk != "" {
						reqs = append(reqs, nk)
					}
				}
				meta := res.Meta[key]
				if meta == nil {
					meta = map[string]any{}
				}
				meta["assumed"] = true
				if len(reqs) > 0 {
					meta["required_by"] = dedupeStrings(append(stringSliceFromAny(meta["required_by"]), reqs...))
				}
				if strings.TrimSpace(assumed.Notes) != "" && strings.TrimSpace(stringFromAny(meta["assumed_notes"])) == "" {
					meta["assumed_notes"] = strings.TrimSpace(assumed.Notes)
				}
				res.Meta[key] = meta

				item, exists := byKey[key]
				if exists {
					if strings.TrimSpace(item.Name) == "" {
						item.Name = name
					}
					if len(strings.TrimSpace(item.Summary)) < len(strings.TrimSpace(a.Summary)) {
						item.Summary = strings.TrimSpace(a.Summary)
					}
					item.Aliases = dedupeStrings(append(item.Aliases, a.Aliases...))
					item.Aliases = dedupeStrings(append(item.Aliases, a.Name, a.Key))
					item.Citations = dedupeStrings(append(item.Citations, a.Citations...))
					if a.Importance > item.Importance {
						item.Importance = a.Importance
					}
					byKey[key] = item
					continue
				}
				newItem := conceptInvItem{
					Key:        key,
					Name:       name,
					ParentKey:  "",
					Depth:      0,
					Summary:    strings.TrimSpace(a.Summary),
					KeyPoints:  nil,
					Aliases:    dedupeStrings(append(a.Aliases, a.Name, a.Key)),
					Importance: a.Importance,
					Citations:  dedupeStrings(filterChunkIDStrings(a.Citations, allowedChunkIDs)),
				}
				byKey[key] = newItem
				added++
			}
			if len(byKey) > 0 {
				res.Concepts = make([]conceptInvItem, 0, len(byKey))
				for _, v := range byKey {
					res.Concepts = append(res.Concepts, v)
				}
			}
			res.Added = added
			assumedCh <- res
		}()
	} else {
		assumedCh <- assumedResult{Concepts: baseConcepts, Meta: map[string]map[string]any{}}
	}

	if alignEnabled {
		crossDocSectionsJSON = awaitCrossDocSections()
		go func() {
			res := alignResult{}
			conceptsJSONBytes, _ := json.Marshal(map[string]any{"concepts": baseConcepts})
			alignPrompt, err := prompts.Build(prompts.PromptConceptAlignment, prompts.Input{
				ConceptsJSON:         string(conceptsJSONBytes),
				CrossDocSectionsJSON: crossDocSectionsJSON,
			})
			if err != nil {
				res.Err = err
				alignCh <- res
				return
			}
			timer := llmTimer(deps.Log, "concept_alignment", map[string]any{
				"stage":         "concept_graph_build",
				"path_id":       pathID.String(),
				"pass":          "initial",
				"concept_count": len(baseConcepts),
				"has_sections":  strings.TrimSpace(crossDocSectionsJSON) != "",
			})
			alignObj, err := deps.AI.GenerateJSON(ctx, alignPrompt.System, alignPrompt.User, alignPrompt.SchemaName, alignPrompt.Schema)
			timer(err)
			if err != nil {
				res.Err = err
				alignCh <- res
				return
			}
			res.Alignment = parseConceptAlignment(alignObj)
			alignCh <- res
		}()
	} else {
		alignCh <- alignResult{}
	}

	assumedRes := <-assumedCh
	conceptsOut = assumedRes.Concepts
	conceptMetaByKey = assumedRes.Meta
	assumedAdded := assumedRes.Added
	if assumedRes.Err != nil && deps.Log != nil {
		deps.Log.Warn("concept_graph_build: assumed knowledge failed (continuing)", "error", assumedRes.Err.Error(), "path_id", pathID.String())
	}
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	if deps.Log != nil && assumedAdded > 0 {
		deps.Log.Info("concept_graph_build: assumed knowledge added", "path_id", pathID.String(), "added", assumedAdded)
	}
	reporter.Update(60, fmt.Sprintf("Assumed knowledge done (+%d)", assumedAdded))

	alignRes := <-alignCh
	if assumedAdded > 0 && alignEnabled {
		// Re-run alignment on the updated concept list to preserve prior behavior.
		conceptsJSONBytes, _ := json.Marshal(map[string]any{"concepts": conceptsOut})
		alignPrompt, err := prompts.Build(prompts.PromptConceptAlignment, prompts.Input{
			ConceptsJSON:         string(conceptsJSONBytes),
			CrossDocSectionsJSON: crossDocSectionsJSON,
		})
		if err == nil {
			alignObj, err := deps.AI.GenerateJSON(ctx, alignPrompt.System, alignPrompt.User, alignPrompt.SchemaName, alignPrompt.Schema)
			if err == nil {
				alignment := parseConceptAlignment(alignObj)
				if len(alignment.Aliases) > 0 || len(alignment.Splits) > 0 {
					conceptsOut = applyConceptAlignment(conceptsOut, alignment, allowedChunkIDs, conceptMetaByKey)
				}
			} else if deps.Log != nil {
				deps.Log.Warn("concept_graph_build: concept alignment failed (continuing)", "error", err.Error(), "path_id", pathID.String())
			}
		}
	} else if alignEnabled {
		if alignRes.Err != nil {
			if deps.Log != nil {
				deps.Log.Warn("concept_graph_build: concept alignment failed (continuing)", "error", alignRes.Err.Error(), "path_id", pathID.String())
			}
		} else if len(alignRes.Alignment.Aliases) > 0 || len(alignRes.Alignment.Splits) > 0 {
			conceptsOut = applyConceptAlignment(conceptsOut, alignRes.Alignment, allowedChunkIDs, conceptMetaByKey)
		}
	}
	reporter.Update(65, "Concepts aligned")

	// Re-normalize after enrichments.
	conceptsOut, _ = normalizeConceptInventory(conceptsOut, allowedChunkIDs)
	conceptsOut, _ = dedupeConceptInventoryByKey(conceptsOut)
	if len(conceptMetaByKey) > 0 {
		trimmed := map[string]map[string]any{}
		for _, c := range conceptsOut {
			if meta := conceptMetaByKey[c.Key]; len(meta) > 0 {
				trimmed[c.Key] = meta
			}
		}
		conceptMetaByKey = trimmed
	}

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
	reporter.Update(68, "Generating edges + embeddings")
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

	embedBatchSize := envIntAllowZero("CONCEPT_GRAPH_EMBED_BATCH_SIZE", 128)
	if embedBatchSize <= 0 {
		embedBatchSize = 64
	}
	embedConc := envIntAllowZero("CONCEPT_GRAPH_EMBED_CONCURRENCY", 64)
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
				timer := llmTimer(deps.Log, "concept_embeddings", map[string]any{
					"stage":       "concept_graph_build",
					"path_id":     pathID.String(),
					"batch_size":  end - start,
					"batch_start": start,
				})
				v, err := deps.AI.Embed(egctx, docs[start:end])
				timer(err)
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
		timer := llmTimer(deps.Log, "concept_edges", map[string]any{
			"stage":         "concept_graph_build",
			"path_id":       pathID.String(),
			"concept_count": len(conceptsOut),
			"excerpt_chars": len(edgeExcerpts),
		})
		obj, err := deps.AI.GenerateJSON(gctx, edgesPrompt.System, edgesPrompt.User, edgesPrompt.SchemaName, edgesPrompt.Schema)
		timer(err)
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
	reporter.Update(80, "Edges + embeddings ready")

	edgesOut := parseConceptEdges(edgesObj)
	if len(conceptMetaByKey) > 0 && len(conceptsOut) > 0 {
		known := map[string]bool{}
		for _, c := range conceptsOut {
			if strings.TrimSpace(c.Key) != "" {
				known[c.Key] = true
			}
		}
		seenEdge := map[string]bool{}
		for _, e := range edgesOut {
			seenEdge[strings.TrimSpace(e.FromKey)+"->"+strings.TrimSpace(e.ToKey)] = true
		}
		for _, c := range conceptsOut {
			meta := conceptMetaByKey[c.Key]
			if meta == nil {
				continue
			}
			reqs := stringSliceFromAny(meta["required_by"])
			if len(reqs) == 0 {
				continue
			}
			for _, rk := range reqs {
				key := normalizeConceptKey(rk)
				if key == "" || key == c.Key || !known[key] {
					continue
				}
				ek := c.Key + "->" + key
				if seenEdge[ek] {
					continue
				}
				seenEdge[ek] = true
				edgesOut = append(edgesOut, conceptEdgeItem{
					FromKey:   c.Key,
					ToKey:     key,
					EdgeType:  "prereq",
					Strength:  0.85,
					Rationale: "assumed prerequisite",
					Citations: dedupeStrings(filterChunkIDStrings(c.Citations, allowedChunkIDs)),
				})
			}
		}
	}
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
	const semanticStart = 80
	const semanticEnd = 88
	semanticProgress := func(done, total int) {
		reporter.UpdateRange(done, total, semanticStart, semanticEnd, fmt.Sprintf("Matching canonical concepts %d/%d", done, total))
	}
	semanticMatchByKey, semanticParams := semanticMatchCanonicalConcepts(ctx, deps, conceptsOut, embs, signals, signals.ContentType, adaptiveEnabled, semanticProgress)
	for k, v := range semanticParams {
		adaptiveParams[k] = v
	}
	reporter.Update(semanticEnd, fmt.Sprintf("Canonical match complete (%d matched)", len(semanticMatchByKey)))

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
		meta := map[string]any{
			"aliases":    conceptsOut[i].Aliases,
			"importance": conceptsOut[i].Importance,
		}
		if extra := conceptMetaByKey[conceptsOut[i].Key]; len(extra) > 0 {
			for k, v := range extra {
				meta[k] = v
			}
		}
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
	skipped := false
	reporter.Update(90, "Persisting concept graph")
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
	reporter.Update(92, "Concept graph persisted")

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
		pineconeConc := envIntAllowZero("CONCEPT_GRAPH_PINECONE_CONCURRENCY", 32)
		if pineconeConc < 1 {
			pineconeConc = 1
		}
		pineconeStart := 92
		pineconeEnd := 96
		totalBatches := 0
		if pineconeBatchSize > 0 {
			totalBatches = int(math.Ceil(float64(len(rows)) / float64(pineconeBatchSize)))
		}
		if totalBatches < 1 {
			totalBatches = 1
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
				done := int(atomic.AddInt32(&batches, 1))
				reporter.UpdateRange(done, totalBatches, pineconeStart, pineconeEnd, fmt.Sprintf("Indexing concepts %d/%d", done, totalBatches))
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
		reporter.Update(pineconeEnd, fmt.Sprintf("Indexed concepts (%d batches)", out.PineconeBatches))
	}

	// ---- Upsert to Neo4j (best-effort; cache only) ----
	if deps.Graph != nil {
		if err := syncPathConceptGraphToNeo4j(ctx, deps, pathID); err != nil {
			deps.Log.Warn("neo4j concept graph sync failed (continuing)", "error", err, "path_id", pathID.String())
		}
	}
	reporter.Update(98, "Concept graph ready")

	if conceptInputHash != "" && deps.Artifacts != nil && artifactCacheEnabled() {
		_ = artifactCacheUpsert(ctx, deps.Artifacts, &types.LearningArtifact{
			OwnerUserID:   in.OwnerUserID,
			MaterialSetID: in.MaterialSetID,
			PathID:        pathID,
			ArtifactType:  "concept_graph_build",
			InputHash:     conceptInputHash,
			Version:       artifactHashVersion,
			Metadata: marshalMeta(map[string]any{
				"concepts_made":    out.ConceptsMade,
				"edges_made":       out.EdgesMade,
				"pinecone_batches": out.PineconeBatches,
			}),
		})
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

type conceptSeedMeta struct {
	TotalFiles     int            `json:"total_files"`
	FilesWithSeeds int            `json:"files_with_seeds"`
	SeedCount      int            `json:"seed_count"`
	AvgQuality     float64        `json:"avg_quality"`
	LowQuality     int            `json:"low_quality_files"`
	Usable         bool           `json:"usable"`
	Reason         string         `json:"reason,omitempty"`
	Params         map[string]any `json:"params,omitempty"`
}

func buildConceptSeedFromSignatures(sigs []*types.MaterialFileSignature, signals AdaptiveSignals, contentType string, adaptiveEnabled bool) ([]string, conceptSeedMeta) {
	meta := conceptSeedMeta{TotalFiles: len(sigs)}
	if len(sigs) == 0 {
		meta.Reason = "no_signatures"
		return nil, meta
	}

	minFiles := envIntAllowZero("CONCEPT_GRAPH_SEED_MIN_FILES", 1)
	if minFiles < 1 {
		minFiles = 1
	}
	minKeys := envIntAllowZero("CONCEPT_GRAPH_SEED_MIN_KEYS", 12)
	if minKeys < 1 {
		minKeys = 1
	}
	minQuality := envFloatAllowZero("CONCEPT_GRAPH_SEED_MIN_QUALITY", 0.45)
	if adaptiveEnabled {
		fc := maxInt(signals.FileCount, len(sigs))
		minFiles = clampIntCeiling(int(math.Round(float64(fc)*0.5)), 1, minFiles)
		minKeys = clampIntCeiling(int(math.Round(float64(fc)*3.0)), 6, minKeys)
		minQuality = clamp01(adjustThresholdByContentType("CONCEPT_GRAPH_SEED_MIN_QUALITY", minQuality, contentType))
	}
	meta.Params = map[string]any{
		"CONCEPT_GRAPH_SEED_MIN_FILES":   map[string]any{"actual": minFiles},
		"CONCEPT_GRAPH_SEED_MIN_KEYS":    map[string]any{"actual": minKeys},
		"CONCEPT_GRAPH_SEED_MIN_QUALITY": map[string]any{"actual": minQuality},
	}

	keys := make([]string, 0, 64)
	qualitySum := 0.0
	qualityCount := 0
	lowQuality := 0

	for _, sig := range sigs {
		if sig == nil {
			continue
		}
		rawKeys := jsonListFromRaw(sig.ConceptKeys)
		clean := make([]string, 0, len(rawKeys))
		for _, k := range rawKeys {
			nk := normalizeConceptKey(k)
			if nk != "" {
				clean = append(clean, nk)
			}
		}
		if len(clean) > 0 {
			meta.FilesWithSeeds++
		}
		keys = append(keys, clean...)

		score := signatureQualityScore(sig)
		if score > 0 {
			qualitySum += score
			qualityCount++
			if score < 0.4 {
				lowQuality++
			}
		}
	}

	keys = dedupeStrings(keys)
	meta.SeedCount = len(keys)
	if qualityCount > 0 {
		meta.AvgQuality = qualitySum / float64(qualityCount)
	}
	meta.LowQuality = lowQuality

	switch {
	case meta.FilesWithSeeds < minFiles:
		meta.Reason = "too_few_files"
	case meta.SeedCount < minKeys:
		meta.Reason = "too_few_keys"
	case meta.AvgQuality < minQuality:
		meta.Reason = "low_quality"
	default:
		meta.Usable = true
	}
	return keys, meta
}

func signatureQualityScore(sig *types.MaterialFileSignature) float64 {
	if sig == nil {
		return 0
	}
	q := map[string]any{}
	_ = json.Unmarshal(sig.Quality, &q)
	textQuality := strings.ToLower(strings.TrimSpace(stringFromAny(q["text_quality"])))
	coverage := floatFromAny(q["coverage"], 0.5)
	if coverage < 0 {
		coverage = 0
	}
	if coverage > 1 {
		coverage = 1
	}
	textScore := 0.5
	switch textQuality {
	case "high":
		textScore = 1.0
	case "medium":
		textScore = 0.7
	case "low":
		textScore = 0.3
	}
	score := (textScore + coverage) / 2.0
	rawKeys := jsonListFromRaw(sig.ConceptKeys)
	if len(rawKeys) < 6 {
		score = score * 0.6
	}
	return clamp01(score)
}

func conceptInventoryWeak(cov conceptCoverage, concepts []conceptInvItem, seedMeta conceptSeedMeta, signals AdaptiveSignals, contentType string, adaptiveEnabled bool) (bool, map[string]any) {
	minConcepts := envIntAllowZero("CONCEPT_GRAPH_SEED_MIN_CONCEPTS", 12)
	if minConcepts < 1 {
		minConcepts = 1
	}
	minCoverage := envFloatAllowZero("CONCEPT_GRAPH_SEED_MIN_COVERAGE_CONF", 0.35)
	if adaptiveEnabled {
		fc := maxInt(signals.FileCount, 1)
		minConcepts = clampIntCeiling(int(math.Round(float64(fc)*3.0)), 6, minConcepts)
		minCoverage = clamp01(adjustThresholdByContentType("CONCEPT_GRAPH_SEED_MIN_COVERAGE_CONF", minCoverage, contentType))
	}
	params := map[string]any{
		"CONCEPT_GRAPH_SEED_MIN_CONCEPTS":      map[string]any{"actual": minConcepts},
		"CONCEPT_GRAPH_SEED_MIN_COVERAGE_CONF": map[string]any{"actual": minCoverage},
	}
	if len(concepts) < minConcepts {
		return true, params
	}
	if seedMeta.SeedCount > 0 && len(concepts) < (seedMeta.SeedCount/2) {
		return true, params
	}
	if cov.Confidence > 0 && cov.Confidence < minCoverage {
		return true, params
	}
	return false, params
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

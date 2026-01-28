package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/yungbote/neurobridge-backend/internal/data/materialsetctx"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/prompts"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type MaterialSignalBuildDeps struct {
	DB           *gorm.DB
	Log          *logger.Logger
	Files        repos.MaterialFileRepo
	FileSigs     repos.MaterialFileSignatureRepo
	FileSections repos.MaterialFileSectionRepo
	Chunks       repos.MaterialChunkRepo
	Concepts     repos.ConceptRepo
	MaterialSets repos.MaterialSetRepo
	AI           openai.Client
	Bootstrap    services.LearningBuildBootstrapService
}

type MaterialSignalBuildInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
}

type MaterialSignalBuildOutput struct {
	PathID uuid.UUID `json:"path_id"`

	FilesTotal             int `json:"files_total"`
	IntentsUpserted        int `json:"intents_upserted"`
	ChunkSignalsUpserted   int `json:"chunk_signals_upserted"`
	SetCoverageUpserted    int `json:"set_coverage_upserted"`
	SetEdgesUpserted       int `json:"set_edges_upserted"`
	ChunkLinksUpserted     int `json:"chunk_links_upserted"`
	GlobalEdgesUpserted    int `json:"global_edges_upserted"`
	GlobalCoverageUpserted int `json:"global_coverage_upserted"`
	EmergentUpserted       int `json:"emergent_upserted"`

	Skipped bool           `json:"skipped"`
	Trace   map[string]any `json:"trace,omitempty"`
}

func MaterialSignalBuild(ctx context.Context, deps MaterialSignalBuildDeps, in MaterialSignalBuildInput) (MaterialSignalBuildOutput, error) {
	out := MaterialSignalBuildOutput{Trace: map[string]any{}}
	if deps.DB == nil || deps.Log == nil || deps.Files == nil || deps.FileSigs == nil || deps.FileSections == nil || deps.Chunks == nil || deps.Concepts == nil || deps.MaterialSets == nil || deps.AI == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("material_signal_build: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("material_signal_build: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("material_signal_build: missing material_set_id")
	}
	if in.SagaID == uuid.Nil {
		return out, fmt.Errorf("material_signal_build: missing saga_id")
	}

	if !envBool("MATERIAL_SIGNAL_ENABLED", true) {
		out.Skipped = true
		return out, nil
	}

	pathID, err := resolvePathID(ctx, deps.Bootstrap, in.OwnerUserID, in.MaterialSetID, in.PathID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	setCtx, err := materialsetctx.Resolve(ctx, deps.DB, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	sourceSetID := setCtx.SourceMaterialSetID

	files, err := deps.Files.GetByMaterialSetID(dbctx.Context{Ctx: ctx}, sourceSetID)
	if err != nil {
		return out, err
	}
	if len(setCtx.AllowFileIDs) > 0 {
		files = filterMaterialFilesByAllowlist(files, setCtx.AllowFileIDs)
	}
	out.FilesTotal = len(files)
	if len(files) == 0 {
		return out, fmt.Errorf("material_signal_build: no files for set")
	}

	fileIDs := make([]uuid.UUID, 0, len(files))
	for _, f := range files {
		if f != nil && f.ID != uuid.Nil {
			fileIDs = append(fileIDs, f.ID)
		}
	}

	// Load chunks + signatures + sections.
	chunks, err := deps.Chunks.GetByMaterialFileIDs(dbctx.Context{Ctx: ctx}, fileIDs)
	if err != nil {
		return out, err
	}
	chunksByFile := map[uuid.UUID][]*types.MaterialChunk{}
	for _, ch := range chunks {
		if ch == nil || ch.MaterialFileID == uuid.Nil {
			continue
		}
		chunksByFile[ch.MaterialFileID] = append(chunksByFile[ch.MaterialFileID], ch)
	}

	fileSigs, err := deps.FileSigs.GetByMaterialFileIDs(dbctx.Context{Ctx: ctx}, fileIDs)
	if err != nil {
		return out, err
	}
	sigByFile := map[uuid.UUID]*types.MaterialFileSignature{}
	for _, s := range fileSigs {
		if s != nil && s.MaterialFileID != uuid.Nil {
			sigByFile[s.MaterialFileID] = s
		}
	}

	sections, err := deps.FileSections.GetByMaterialFileIDs(dbctx.Context{Ctx: ctx}, fileIDs)
	if err != nil {
		return out, err
	}
	sectionsByFile := map[uuid.UUID][]*types.MaterialFileSection{}
	for _, s := range sections {
		if s != nil && s.MaterialFileID != uuid.Nil {
			sectionsByFile[s.MaterialFileID] = append(sectionsByFile[s.MaterialFileID], s)
		}
	}

	// Existing intents/signals (idempotency).
	force := strings.EqualFold(strings.TrimSpace(os.Getenv("MATERIAL_SIGNAL_FORCE_REBUILD")), "true")
	existingIntents := map[uuid.UUID]*types.MaterialIntent{}
	if !force {
		var rows []*types.MaterialIntent
		if err := deps.DB.WithContext(ctx).Model(&types.MaterialIntent{}).Where("material_file_id IN ?", fileIDs).Find(&rows).Error; err == nil {
			for _, r := range rows {
				if r != nil && r.MaterialFileID != uuid.Nil {
					existingIntents[r.MaterialFileID] = r
				}
			}
		}
	}
	existingSignals := map[uuid.UUID]*types.MaterialChunkSignal{}
	if !force {
		var rows []*types.MaterialChunkSignal
		if err := deps.DB.WithContext(ctx).Model(&types.MaterialChunkSignal{}).Where("material_file_id IN ?", fileIDs).Find(&rows).Error; err == nil {
			for _, r := range rows {
				if r != nil && r.MaterialChunkID != uuid.Nil {
					existingSignals[r.MaterialChunkID] = r
				}
			}
		}
	}

	// Load path concept mapping for canonical IDs.
	concepts, err := deps.Concepts.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID)
	if err != nil {
		return out, err
	}
	conceptByKey := map[string]*types.Concept{}
	for _, c := range concepts {
		if c == nil || c.ID == uuid.Nil {
			continue
		}
		k := strings.TrimSpace(strings.ToLower(c.Key))
		if k != "" {
			conceptByKey[k] = c
		}
	}

	settings := loadMaterialSignalSettings()
	intentByFile := map[uuid.UUID]*types.MaterialIntent{}
	var intentsMu sync.Mutex

	// ---- Level 1: per-file intent ----
	intentJobs := make(chan *types.MaterialFile)
	var intentWG sync.WaitGroup
	for i := 0; i < settings.IntentConcurrency; i++ {
		intentWG.Add(1)
		go func() {
			defer intentWG.Done()
			for f := range intentJobs {
				if f == nil || f.ID == uuid.Nil {
					continue
				}
				if !force {
					if existing := existingIntents[f.ID]; existing != nil {
						if !intentNeedsRebuild(existing) {
							intentsMu.Lock()
							intentByFile[f.ID] = existing
							intentsMu.Unlock()
							continue
						}
					}
				}
				intent, err := buildMaterialIntent(ctx, deps, f, sigByFile[f.ID], chunksByFile[f.ID], settings)
				if err != nil {
					if deps.Log != nil {
						deps.Log.Warn("material_signal_build: intent extraction failed (fallback)", "error", err, "file_id", f.ID.String())
					}
					intent = fallbackMaterialIntent(f, sigByFile[f.ID])
				}
				intentsMu.Lock()
				intentByFile[f.ID] = intent
				intentsMu.Unlock()
			}
		}()
	}
	for _, f := range files {
		intentJobs <- f
	}
	close(intentJobs)
	intentWG.Wait()

	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		if intentByFile[f.ID] == nil {
			intentByFile[f.ID] = fallbackMaterialIntent(f, sigByFile[f.ID])
		}
		intentByFile[f.ID].MaterialFileID = f.ID
		intentByFile[f.ID].MaterialSetID = in.MaterialSetID
	}

	intentRows := make([]*types.MaterialIntent, 0, len(intentByFile))
	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		intent := intentByFile[f.ID]
		if intent == nil {
			continue
		}
		intent.MaterialSetID = in.MaterialSetID
		intentRows = append(intentRows, intent)
	}
	intentRows = dedupeMaterialIntentRows(intentRows)

	// ---- Level 1: chunk signals ----
	type chunkSignalJob struct {
		FileID uuid.UUID
		Intent *types.MaterialIntent
		Batch  []chunkSignalInput
	}

	signalJobs := make(chan chunkSignalJob)
	var signalWG sync.WaitGroup
	signalMu := sync.Mutex{}
	allSignalRows := make([]*types.MaterialChunkSignal, 0)
	chunkMetaUpdates := map[uuid.UUID]map[string]any{}

	for i := 0; i < settings.SignalConcurrency; i++ {
		signalWG.Add(1)
		go func() {
			defer signalWG.Done()
			for job := range signalJobs {
				if job.FileID == uuid.Nil || len(job.Batch) == 0 {
					continue
				}
				rows, updates := buildChunkSignals(ctx, deps, job.Intent, job.Batch, settings)
				signalMu.Lock()
				allSignalRows = append(allSignalRows, rows...)
				for id, up := range updates {
					if existing := chunkMetaUpdates[id]; existing != nil {
						for k, v := range up {
							existing[k] = v
						}
					} else {
						chunkMetaUpdates[id] = up
					}
				}
				signalMu.Unlock()
			}
		}()
	}

	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		chArr := chunksByFile[f.ID]
		if len(chArr) == 0 {
			continue
		}
		sectionPathByChunk := buildSectionPathByChunk(chArr, sectionsByFile[f.ID])
		batches := buildChunkSignalBatches(chArr, sectionPathByChunk, existingSignals, settings)
		intent := intentByFile[f.ID]
		for _, b := range batches {
			signalJobs <- chunkSignalJob{FileID: f.ID, Intent: intent, Batch: b}
		}
	}
	close(signalJobs)
	signalWG.Wait()
	allSignalRows = dedupeMaterialChunkSignalRows(allSignalRows)

	// Persist intents + chunk signals.
	if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(intentRows) > 0 {
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "material_file_id"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"material_set_id",
					"from_state",
					"to_state",
					"core_thread",
					"destination_concepts",
					"prerequisite_concepts",
					"assumed_knowledge",
					"metadata",
					"updated_at",
				}),
			}).Create(&intentRows).Error; err != nil {
				return err
			}
			out.IntentsUpserted = len(intentRows)
		}

		if len(allSignalRows) > 0 {
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "material_chunk_id"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"material_file_id",
					"material_set_id",
					"role",
					"signal_strength",
					"floor_signal",
					"intent_alignment_score",
					"set_position_score",
					"novelty_score",
					"density_score",
					"complexity_score",
					"load_bearing_score",
					"compound_weight",
					"trajectory",
					"metadata",
					"updated_at",
				}),
			}).CreateInBatches(&allSignalRows, 200).Error; err != nil {
				return err
			}
			out.ChunkSignalsUpserted = len(allSignalRows)
		}
		return nil
	}); err != nil {
		return out, err
	}

	// Apply chunk metadata updates (section path + signal summary).
	if settings.WriteChunkMetadata && len(chunkMetaUpdates) > 0 {
		if err := applyChunkMetadataUpdates(ctx, deps.DB, chunkMetaUpdates, settings.MetadataUpdateConcurrency); err != nil && deps.Log != nil {
			deps.Log.Warn("material_signal_build: chunk metadata updates failed (continuing)", "error", err)
		}
	}

	// Load complete signal rows for set-level computations.
	var signalRows []*types.MaterialChunkSignal
	if err := deps.DB.WithContext(ctx).Model(&types.MaterialChunkSignal{}).Where("material_file_id IN ?", fileIDs).Find(&signalRows).Error; err != nil {
		return out, err
	}

	// ---- Level 2: set-level coverage + edges ----
	setCoverageRows, conceptWeights, fileConceptStats := buildSetConceptCoverage(signalRows, sigByFile, in.MaterialSetID, pathID, conceptByKey)
	setCoverageRows = dedupeMaterialSetCoverageRows(setCoverageRows)
	if len(setCoverageRows) > 0 {
		if err := deps.DB.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "material_set_id"}, {Name: "concept_key"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"path_id",
				"canonical_concept_id",
				"coverage_type",
				"depth",
				"score",
				"source_material_file_ids",
				"metadata",
				"updated_at",
			}),
		}).CreateInBatches(&setCoverageRows, 200).Error; err != nil {
			return out, err
		}
		out.SetCoverageUpserted = len(setCoverageRows)
	}

	// Update concept metadata with signal weights (best-effort).
	if len(conceptWeights) > 0 {
		if err := applyConceptSignalWeights(ctx, deps.DB, conceptByKey, conceptWeights); err != nil && deps.Log != nil {
			deps.Log.Warn("material_signal_build: concept weight update failed (continuing)", "error", err)
		}
	}

	edgeRows := buildMaterialEdges(in.MaterialSetID, fileConceptStats, settings)
	edgeRows = dedupeMaterialEdgeRows(edgeRows)
	if len(edgeRows) > 0 {
		if err := deps.DB.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "material_set_id"}, {Name: "from_material_file_id"}, {Name: "to_material_file_id"}, {Name: "edge_type"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"strength",
				"bridging_concepts",
				"metadata",
				"updated_at",
			}),
		}).CreateInBatches(&edgeRows, 200).Error; err != nil {
			return out, err
		}
		out.SetEdgesUpserted = len(edgeRows)
	}

	chunkLinkRows := buildChunkLinks(in.MaterialSetID, signalRows, settings)
	chunkLinkRows = dedupeChunkLinkRows(chunkLinkRows)
	if len(chunkLinkRows) > 0 {
		if err := deps.DB.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "material_set_id"}, {Name: "from_chunk_id"}, {Name: "to_chunk_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"relation",
				"strength",
				"metadata",
				"updated_at",
			}),
		}).CreateInBatches(&chunkLinkRows, 300).Error; err != nil {
			return out, err
		}
		out.ChunkLinksUpserted = len(chunkLinkRows)
	}

	if envBool("MATERIAL_SIGNAL_SET_ENABLED", true) {
		if err := upsertMaterialSetIntent(ctx, deps, in.MaterialSetID, files, intentByFile, setCoverageRows, edgeRows, settings); err != nil && deps.Log != nil {
			deps.Log.Warn("material_signal_build: set intent failed (continuing)", "error", err)
		}
	}

	var setIntent *types.MaterialSetIntent
	{
		var it types.MaterialSetIntent
		if err := deps.DB.WithContext(ctx).Model(&types.MaterialSetIntent{}).Where("material_set_id = ?", in.MaterialSetID).Take(&it).Error; err == nil && it.MaterialSetID != uuid.Nil {
			setIntent = &it
		}
	}
	if setIntent != nil {
		setPos := computeSetPositionScores(setIntent, signalRows)
		if len(setPos) > 0 {
			if err := upsertChunkSignalScores(ctx, deps.DB, setPos, nil); err != nil && deps.Log != nil {
				deps.Log.Warn("material_signal_build: set position update failed (continuing)", "error", err)
			}
		}
	}

	// ---- Level 3: cross-set analysis ----
	var crossSetByKey map[string]float64
	if envBool("MATERIAL_SIGNAL_GLOBAL_ENABLED", true) {
		globalOut, gerr := buildCrossSetSignals(ctx, deps, in.OwnerUserID, settings)
		if gerr != nil && deps.Log != nil {
			deps.Log.Warn("material_signal_build: cross-set analysis failed (continuing)", "error", gerr)
		} else {
			out.GlobalEdgesUpserted = globalOut.SetEdgesUpserted
			out.GlobalCoverageUpserted = globalOut.GlobalCoverageUpserted
			out.EmergentUpserted = globalOut.EmergentUpserted
			crossSetByKey = globalOut.CrossSetByKey
		}
	}

	if len(crossSetByKey) == 0 {
		crossSetByKey = loadCrossSetRelevanceByKey(ctx, deps.DB, in.OwnerUserID, in.MaterialSetID)
	}
	if len(signalRows) > 0 {
		if err := upsertChunkSignalCompoundWeights(ctx, deps.DB, signalRows, setIntent, crossSetByKey); err != nil && deps.Log != nil {
			deps.Log.Warn("material_signal_build: compound weight update failed (continuing)", "error", err)
		}
	}

	return out, nil
}

// ---------- helpers ----------

type materialSignalSettings struct {
	IntentConcurrency         int
	SignalConcurrency         int
	ChunkBatchSize            int
	ChunkExcerptChars         int
	MaxChunksPerFile          int
	WriteChunkMetadata        bool
	MetadataUpdateConcurrency int
	MaxLinksPerConcept        int
	MaxChunkLinks             int
}

func loadMaterialSignalSettings() materialSignalSettings {
	return materialSignalSettings{
		IntentConcurrency:         envInt("MATERIAL_SIGNAL_INTENT_CONCURRENCY", 4),
		SignalConcurrency:         envInt("MATERIAL_SIGNAL_CONCURRENCY", 6),
		ChunkBatchSize:            envInt("MATERIAL_SIGNAL_CHUNK_BATCH_SIZE", 32),
		ChunkExcerptChars:         envInt("MATERIAL_SIGNAL_CHUNK_EXCERPT_CHARS", 650),
		MaxChunksPerFile:          envIntAllowZero("MATERIAL_SIGNAL_MAX_CHUNKS_PER_FILE", 0),
		WriteChunkMetadata:        envBool("MATERIAL_SIGNAL_WRITE_CHUNK_METADATA", true),
		MetadataUpdateConcurrency: envInt("MATERIAL_SIGNAL_METADATA_UPDATE_CONCURRENCY", 4),
		MaxLinksPerConcept:        envIntAllowZero("MATERIAL_SIGNAL_MAX_LINKS_PER_CONCEPT", 6),
		MaxChunkLinks:             envIntAllowZero("MATERIAL_SIGNAL_MAX_CHUNK_LINKS", 1200),
	}
}

type chunkSignalInput struct {
	ChunkID     string `json:"chunk_id"`
	SectionPath string `json:"section_path,omitempty"`
	Page        int    `json:"page,omitempty"`
	Excerpt     string `json:"excerpt"`
}

func buildMaterialIntent(ctx context.Context, deps MaterialSignalBuildDeps, f *types.MaterialFile, sig *types.MaterialFileSignature, chunks []*types.MaterialChunk, settings materialSignalSettings) (*types.MaterialIntent, error) {
	if f == nil || f.ID == uuid.Nil {
		return nil, fmt.Errorf("missing file")
	}
	excerpts := buildChunkExcerptLines(chunks, 12, 600)
	if strings.TrimSpace(excerpts) == "" {
		excerpts = "No extractable excerpt available."
	}
	ctxJSON := buildMaterialContextJSON(f, sig)
	p, err := prompts.Build(prompts.PromptMaterialIntentExtract, prompts.Input{
		MaterialContextJSON: ctxJSON,
		Excerpts:            excerpts,
	})
	if err != nil {
		return nil, err
	}
	obj, err := deps.AI.GenerateJSON(ctx, p.System, p.User, p.SchemaName, p.Schema)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	intent := &types.MaterialIntent{
		ID:                   uuid.New(),
		MaterialFileID:       f.ID,
		MaterialSetID:        f.MaterialSetID,
		FromState:            strings.TrimSpace(stringFromAny(obj["from_state"])),
		ToState:              strings.TrimSpace(stringFromAny(obj["to_state"])),
		CoreThread:           strings.TrimSpace(stringFromAny(obj["core_thread"])),
		DestinationConcepts:  datatypes.JSON(mustJSON(dedupeStrings(stringSliceFromAny(obj["destination_concepts"])))),
		PrerequisiteConcepts: datatypes.JSON(mustJSON(dedupeStrings(stringSliceFromAny(obj["prerequisite_concepts"])))),
		AssumedKnowledge:     datatypes.JSON(mustJSON(dedupeStrings(stringSliceFromAny(obj["assumed_knowledge"])))),
		Metadata:             datatypes.JSON(mustJSON(map[string]any{"notes": dedupeStrings(stringSliceFromAny(obj["notes"]))})),
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	return intent, nil
}

func fallbackMaterialIntent(f *types.MaterialFile, sig *types.MaterialFileSignature) *types.MaterialIntent {
	now := time.Now().UTC()
	topics := []string{}
	concepts := []string{}
	summary := ""
	if sig != nil {
		topics = dedupeStrings(stringSliceFromAny(sig.Topics))
		concepts = dedupeStrings(stringSliceFromAny(sig.ConceptKeys))
		summary = strings.TrimSpace(sig.SummaryMD)
	}
	return &types.MaterialIntent{
		ID:                   uuid.New(),
		MaterialFileID:       f.ID,
		MaterialSetID:        f.MaterialSetID,
		FromState:            "basic familiarity with the topic",
		ToState:              "working understanding of the material",
		CoreThread:           strings.TrimSpace(firstNonEmpty(summary, strings.Join(topics, ", "), f.OriginalName)),
		DestinationConcepts:  datatypes.JSON(mustJSON(concepts)),
		PrerequisiteConcepts: datatypes.JSON(mustJSON([]string{})),
		AssumedKnowledge:     datatypes.JSON(mustJSON([]string{})),
		Metadata:             datatypes.JSON(mustJSON(map[string]any{"notes": []string{"fallback_intent"}})),
		CreatedAt:            now,
		UpdatedAt:            now,
	}
}

func intentNeedsRebuild(intent *types.MaterialIntent) bool {
	if intent == nil {
		return true
	}
	if intentHasFallbackNote(intent.Metadata) {
		return true
	}
	if strings.TrimSpace(intent.CoreThread) != "" {
		return false
	}
	if len(jsonListFromRaw(intent.DestinationConcepts)) > 0 {
		return false
	}
	if len(jsonListFromRaw(intent.PrerequisiteConcepts)) > 0 {
		return false
	}
	if len(jsonListFromRaw(intent.AssumedKnowledge)) > 0 {
		return false
	}
	return true
}

func intentHasFallbackNote(meta datatypes.JSON) bool {
	if len(meta) == 0 || strings.TrimSpace(string(meta)) == "" || strings.TrimSpace(string(meta)) == "null" {
		return false
	}
	var payload map[string]any
	if err := json.Unmarshal(meta, &payload); err != nil {
		return false
	}
	notes := dedupeStrings(stringSliceFromAny(payload["notes"]))
	for _, n := range notes {
		if strings.EqualFold(strings.TrimSpace(n), "fallback_intent") {
			return true
		}
	}
	return false
}

func buildChunkSignals(ctx context.Context, deps MaterialSignalBuildDeps, intent *types.MaterialIntent, batch []chunkSignalInput, settings materialSignalSettings) ([]*types.MaterialChunkSignal, map[uuid.UUID]map[string]any) {
	if len(batch) == 0 {
		return nil, nil
	}
	batchJSON, _ := json.Marshal(batch)
	intentJSON, _ := json.Marshal(map[string]any{
		"from_state":            intent.FromState,
		"to_state":              intent.ToState,
		"core_thread":           intent.CoreThread,
		"destination_concepts":  jsonListFromRaw(intent.DestinationConcepts),
		"prerequisite_concepts": jsonListFromRaw(intent.PrerequisiteConcepts),
		"assumed_knowledge":     jsonListFromRaw(intent.AssumedKnowledge),
	})
	p, err := prompts.Build(prompts.PromptMaterialChunkSignal, prompts.Input{
		MaterialIntentJSON: string(intentJSON),
		ChunkBatchJSON:     string(batchJSON),
	})
	if err != nil {
		return fallbackChunkSignals(intent, batch), map[uuid.UUID]map[string]any{}
	}
	obj, err := deps.AI.GenerateJSON(ctx, p.System, p.User, p.SchemaName, p.Schema)
	if err != nil {
		if deps.Log != nil {
			deps.Log.Warn("material_signal_build: chunk signal generation failed (fallback)", "error", err)
		}
		return fallbackChunkSignals(intent, batch), map[uuid.UUID]map[string]any{}
	}
	items := sliceAny(obj["items"])
	if len(items) == 0 {
		return fallbackChunkSignals(intent, batch), map[uuid.UUID]map[string]any{}
	}
	rows := make([]*types.MaterialChunkSignal, 0, len(items))
	metaUpdates := map[uuid.UUID]map[string]any{}
	now := time.Now().UTC()
	for _, raw := range items {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		idStr := strings.TrimSpace(stringFromAny(m["chunk_id"]))
		id, err := uuid.Parse(idStr)
		if err != nil || id == uuid.Nil {
			continue
		}
		role := strings.TrimSpace(stringFromAny(m["role"]))
		trajectory := map[string]any{
			"establishes":   dedupeStrings(stringSliceFromAny(mapFromAny(m["trajectory"])["establishes"])),
			"reinforces":    dedupeStrings(stringSliceFromAny(mapFromAny(m["trajectory"])["reinforces"])),
			"builds_on":     dedupeStrings(stringSliceFromAny(mapFromAny(m["trajectory"])["builds_on"])),
			"points_toward": dedupeStrings(stringSliceFromAny(mapFromAny(m["trajectory"])["points_toward"])),
		}
		row := &types.MaterialChunkSignal{
			ID:                   uuid.New(),
			MaterialChunkID:      id,
			MaterialFileID:       intent.MaterialFileID,
			MaterialSetID:        intent.MaterialSetID,
			Role:                 role,
			SignalStrength:       clamp01(floatFromAny(m["signal_strength"], 0.5)),
			FloorSignal:          clamp01(floatFromAny(m["floor_signal"], 0.2)),
			IntentAlignmentScore: clamp01(floatFromAny(m["intent_alignment_score"], 0.5)),
			NoveltyScore:         clamp01(floatFromAny(m["novelty_score"], 0.4)),
			DensityScore:         clamp01(floatFromAny(m["density_score"], 0.4)),
			ComplexityScore:      clamp01(floatFromAny(m["complexity_score"], 0.4)),
			LoadBearingScore:     clamp01(floatFromAny(m["load_bearing_score"], 0.4)),
			Trajectory:           datatypes.JSON(mustJSON(trajectory)),
			Metadata:             datatypes.JSON(mustJSON(map[string]any{"notes": dedupeStrings(stringSliceFromAny(m["notes"]))})),
			CreatedAt:            now,
			UpdatedAt:            now,
		}
		rows = append(rows, row)
		metaUpdates[id] = map[string]any{
			"signal_role":            row.Role,
			"signal_strength":        row.SignalStrength,
			"signal_floor":           row.FloorSignal,
			"intent_alignment_score": row.IntentAlignmentScore,
			"signal_novelty":         row.NoveltyScore,
			"signal_density":         row.DensityScore,
			"signal_complexity":      row.ComplexityScore,
			"signal_load_bearing":    row.LoadBearingScore,
			"signal_trajectory":      trajectory,
		}
	}
	return rows, metaUpdates
}

func fallbackChunkSignals(intent *types.MaterialIntent, batch []chunkSignalInput) []*types.MaterialChunkSignal {
	now := time.Now().UTC()
	rows := make([]*types.MaterialChunkSignal, 0, len(batch))
	for _, in := range batch {
		id, err := uuid.Parse(strings.TrimSpace(in.ChunkID))
		if err != nil || id == uuid.Nil {
			continue
		}
		alignment := estimateIntentAlignment(intent, in.Excerpt)
		rows = append(rows, &types.MaterialChunkSignal{
			ID:                   uuid.New(),
			MaterialChunkID:      id,
			MaterialFileID:       intent.MaterialFileID,
			MaterialSetID:        intent.MaterialSetID,
			Role:                 "explanation",
			SignalStrength:       0.5,
			FloorSignal:          0.25,
			IntentAlignmentScore: alignment,
			NoveltyScore:         0.4,
			DensityScore:         0.4,
			ComplexityScore:      0.4,
			LoadBearingScore:     0.4,
			Trajectory:           datatypes.JSON(mustJSON(map[string]any{"establishes": []string{}, "reinforces": []string{}, "builds_on": []string{}, "points_toward": []string{}})),
			Metadata:             datatypes.JSON(mustJSON(map[string]any{"notes": []string{"fallback_signal"}})),
			CreatedAt:            now,
			UpdatedAt:            now,
		})
	}
	return rows
}

func estimateIntentAlignment(intent *types.MaterialIntent, excerpt string) float64 {
	if intent == nil {
		return 0.5
	}
	text := strings.ToLower(strings.TrimSpace(excerpt))
	if text == "" {
		return 0.4
	}
	core := strings.ToLower(strings.TrimSpace(intent.CoreThread))
	score := 0.5
	if core != "" && strings.Contains(text, core) {
		score += 0.2
	}
	keys := append(jsonListFromRaw(intent.DestinationConcepts), jsonListFromRaw(intent.PrerequisiteConcepts)...)
	hits := 0
	for _, k := range keys {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "" {
			continue
		}
		if strings.Contains(text, k) {
			hits++
		}
	}
	if hits > 0 {
		score += 0.1 + 0.05*float64(hits)
	}
	return clamp01(score)
}

func buildMaterialContextJSON(f *types.MaterialFile, sig *types.MaterialFileSignature) string {
	obj := map[string]any{
		"file_id":       f.ID.String(),
		"original_name": strings.TrimSpace(f.OriginalName),
		"mime_type":     strings.TrimSpace(f.MimeType),
		"size_bytes":    f.SizeBytes,
	}
	if sig != nil {
		obj["summary_md"] = strings.TrimSpace(sig.SummaryMD)
		obj["topics"] = dedupeStrings(stringSliceFromAny(sig.Topics))
		obj["concept_keys"] = dedupeStrings(stringSliceFromAny(sig.ConceptKeys))
		obj["difficulty"] = strings.TrimSpace(sig.Difficulty)
		obj["domain_tags"] = dedupeStrings(stringSliceFromAny(sig.DomainTags))
	}
	b, _ := json.Marshal(obj)
	return string(b)
}

func buildChunkExcerptLines(chunks []*types.MaterialChunk, maxLines int, maxChars int) string {
	if len(chunks) == 0 {
		return ""
	}
	lines := make([]string, 0, maxLines)
	count := 0
	for _, ch := range chunks {
		if ch == nil || strings.TrimSpace(ch.Text) == "" {
			continue
		}
		line := strings.TrimSpace(ch.Text)
		if len(line) > maxChars {
			line = line[:maxChars] + "..."
		}
		lines = append(lines, line)
		count++
		if count >= maxLines {
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func buildSectionPathByChunk(chunks []*types.MaterialChunk, sections []*types.MaterialFileSection) map[uuid.UUID]string {
	if len(chunks) == 0 {
		return map[uuid.UUID]string{}
	}
	ranges := make([]sectionRange, 0, len(sections))
	for _, s := range sections {
		if s == nil {
			continue
		}
		label := strings.TrimSpace(s.Path)
		if label == "" {
			label = strings.TrimSpace(s.Title)
		}
		if label == "" {
			continue
		}
		depth := 1
		if strings.Contains(label, ">") {
			depth = len(strings.Split(label, ">"))
		}
		ranges = append(ranges, sectionRange{
			StartPage: s.StartPage,
			EndPage:   s.EndPage,
			StartSec:  s.StartSec,
			EndSec:    s.EndSec,
			Label:     label,
			Depth:     depth,
		})
	}
	out := map[uuid.UUID]string{}
	for _, ch := range chunks {
		if ch == nil || ch.ID == uuid.Nil {
			continue
		}
		page := chunkPageFromMeta(ch.Metadata)
		sec := chunkTimeFromMeta(ch.Metadata)
		best := ""
		bestDepth := -1
		for _, r := range ranges {
			if page != nil {
				if !rangeIncludesPage(r, *page) {
					continue
				}
			} else if sec != nil {
				if !rangeIncludesSec(r, *sec) {
					continue
				}
			}
			if r.Depth > bestDepth {
				bestDepth = r.Depth
				best = r.Label
			}
		}
		if best != "" {
			out[ch.ID] = best
		}
	}
	return out
}

type sectionRange struct {
	StartPage *int
	EndPage   *int
	StartSec  *float64
	EndSec    *float64
	Label     string
	Depth     int
}

func rangeIncludesPage(r sectionRange, page int) bool {
	if r.StartPage == nil && r.EndPage == nil {
		return false
	}
	if r.StartPage != nil && page < *r.StartPage {
		return false
	}
	if r.EndPage != nil && page > *r.EndPage {
		return false
	}
	return true
}

func rangeIncludesSec(r sectionRange, sec float64) bool {
	if r.StartSec == nil && r.EndSec == nil {
		return false
	}
	if r.StartSec != nil && sec < *r.StartSec {
		return false
	}
	if r.EndSec != nil && sec > *r.EndSec {
		return false
	}
	return true
}

func chunkPageFromMeta(raw datatypes.JSON) *int {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil || meta == nil {
		return nil
	}
	if v, ok := meta["page"]; ok {
		i := intFromAny(v, -1)
		if i >= 0 {
			return &i
		}
	}
	return nil
}

func chunkTimeFromMeta(raw datatypes.JSON) *float64 {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil || meta == nil {
		return nil
	}
	if v, ok := meta["start_sec"]; ok {
		f := floatFromAny(v, -1)
		if f >= 0 {
			return &f
		}
	}
	return nil
}

func buildChunkSignalBatches(chunks []*types.MaterialChunk, sectionPathByChunk map[uuid.UUID]string, existing map[uuid.UUID]*types.MaterialChunkSignal, settings materialSignalSettings) [][]chunkSignalInput {
	out := make([][]chunkSignalInput, 0)
	if len(chunks) == 0 {
		return out
	}
	maxChunks := settings.MaxChunksPerFile
	batch := make([]chunkSignalInput, 0, settings.ChunkBatchSize)
	count := 0
	for _, ch := range chunks {
		if ch == nil || ch.ID == uuid.Nil {
			continue
		}
		if existing != nil && existing[ch.ID] != nil {
			continue
		}
		if maxChunks > 0 && count >= maxChunks {
			break
		}
		excerpt := strings.TrimSpace(ch.Text)
		if excerpt == "" {
			continue
		}
		if settings.ChunkExcerptChars > 0 && len(excerpt) > settings.ChunkExcerptChars {
			excerpt = excerpt[:settings.ChunkExcerptChars] + "..."
		}
		page := 0
		if p := chunkPageFromMeta(ch.Metadata); p != nil {
			page = *p
		}
		item := chunkSignalInput{
			ChunkID:     ch.ID.String(),
			SectionPath: sectionPathByChunk[ch.ID],
			Page:        page,
			Excerpt:     excerpt,
		}
		batch = append(batch, item)
		if len(batch) >= settings.ChunkBatchSize {
			out = append(out, batch)
			batch = make([]chunkSignalInput, 0, settings.ChunkBatchSize)
		}
		count++
	}
	if len(batch) > 0 {
		out = append(out, batch)
	}
	return out
}

func applyChunkMetadataUpdates(ctx context.Context, db *gorm.DB, updates map[uuid.UUID]map[string]any, concurrency int) error {
	if db == nil || len(updates) == 0 {
		return nil
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	type job struct {
		id   uuid.UUID
		meta map[string]any
	}
	jobs := make(chan job)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	worker := func() {
		defer wg.Done()
		for j := range jobs {
			if j.id == uuid.Nil || j.meta == nil {
				continue
			}
			var existing types.MaterialChunk
			if err := db.WithContext(ctx).Model(&types.MaterialChunk{}).Where("id = ?", j.id).Take(&existing).Error; err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				continue
			}
			var meta map[string]any
			if len(existing.Metadata) > 0 && strings.TrimSpace(string(existing.Metadata)) != "" && strings.TrimSpace(string(existing.Metadata)) != "null" {
				_ = json.Unmarshal(existing.Metadata, &meta)
			}
			if meta == nil {
				meta = map[string]any{}
			}
			changed := false
			for k, v := range j.meta {
				if v == nil {
					continue
				}
				if cur, ok := meta[k]; !ok || fmt.Sprint(cur) != fmt.Sprint(v) {
					meta[k] = v
					changed = true
				}
			}
			if !changed {
				continue
			}
			if err := db.WithContext(ctx).Model(&types.MaterialChunk{}).Where("id = ?", j.id).Update("metadata", datatypes.JSON(mustJSON(meta))).Error; err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				continue
			}
		}
	}
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go worker()
	}
	for id, meta := range updates {
		jobs <- job{id: id, meta: meta}
	}
	close(jobs)
	wg.Wait()
	return firstErr
}

type conceptAgg struct {
	Establishes  map[string]bool
	Reinforces   map[string]bool
	BuildsOn     map[string]bool
	PointsToward map[string]bool
	ScoreSum     float64
	ScoreCount   float64
	FileIDs      map[uuid.UUID]bool
}

type fileConceptStat struct {
	Establishes  map[string]float64
	Reinforces   map[string]float64
	BuildsOn     map[string]float64
	PointsToward map[string]float64
	ScoreSum     float64
}

func buildSetConceptCoverage(rows []*types.MaterialChunkSignal, sigByFile map[uuid.UUID]*types.MaterialFileSignature, materialSetID uuid.UUID, pathID uuid.UUID, conceptByKey map[string]*types.Concept) ([]*types.MaterialSetConceptCoverage, map[string]float64, map[uuid.UUID]*fileConceptStat) {
	agg := map[string]*conceptAgg{}
	fileStats := map[uuid.UUID]*fileConceptStat{}
	for _, r := range rows {
		if r == nil || r.MaterialChunkID == uuid.Nil || strings.TrimSpace(string(r.Trajectory)) == "" {
			continue
		}
		var traj map[string]any
		if err := json.Unmarshal(r.Trajectory, &traj); err != nil {
			continue
		}
		get := func(key string) []string {
			return dedupeStrings(stringSliceFromAny(traj[key]))
		}
		est := get("establishes")
		rein := get("reinforces")
		builds := get("builds_on")
		points := get("points_toward")
		if len(est)+len(rein)+len(builds)+len(points) == 0 {
			continue
		}
		score := clamp01(r.SignalStrength)
		fileStat := fileStats[r.MaterialFileID]
		if fileStat == nil {
			fileStat = &fileConceptStat{
				Establishes:  map[string]float64{},
				Reinforces:   map[string]float64{},
				BuildsOn:     map[string]float64{},
				PointsToward: map[string]float64{},
			}
			fileStats[r.MaterialFileID] = fileStat
		}
		fileStat.ScoreSum += score
		apply := func(keys []string, target map[string]float64, trajectory string) {
			for _, k := range keys {
				k = strings.TrimSpace(strings.ToLower(k))
				if k == "" {
					continue
				}
				if target[k] < score {
					target[k] = score
				}
				a := agg[k]
				if a == nil {
					a = &conceptAgg{
						Establishes:  map[string]bool{},
						Reinforces:   map[string]bool{},
						BuildsOn:     map[string]bool{},
						PointsToward: map[string]bool{},
						FileIDs:      map[uuid.UUID]bool{},
					}
					agg[k] = a
				}
				a.ScoreSum += score
				a.ScoreCount += 1
				a.FileIDs[r.MaterialFileID] = true
				switch trajectory {
				case "establishes":
					a.Establishes[k] = true
				case "reinforces":
					a.Reinforces[k] = true
				case "builds_on":
					a.BuildsOn[k] = true
				case "points_toward":
					a.PointsToward[k] = true
				}
			}
		}
		apply(est, fileStat.Establishes, "establishes")
		apply(rein, fileStat.Reinforces, "reinforces")
		apply(builds, fileStat.BuildsOn, "builds_on")
		apply(points, fileStat.PointsToward, "points_toward")
	}

	// Pull concept keys from signatures if missing.
	for fid, sig := range sigByFile {
		if sig == nil {
			continue
		}
		keys := dedupeStrings(stringSliceFromAny(sig.ConceptKeys))
		if len(keys) == 0 {
			continue
		}
		stat := fileStats[fid]
		if stat == nil {
			stat = &fileConceptStat{
				Establishes:  map[string]float64{},
				Reinforces:   map[string]float64{},
				BuildsOn:     map[string]float64{},
				PointsToward: map[string]float64{},
			}
			fileStats[fid] = stat
		}
		for _, k := range keys {
			k = strings.TrimSpace(strings.ToLower(k))
			if k == "" {
				continue
			}
			if _, ok := agg[k]; !ok {
				agg[k] = &conceptAgg{
					Establishes:  map[string]bool{},
					Reinforces:   map[string]bool{},
					BuildsOn:     map[string]bool{},
					PointsToward: map[string]bool{},
					FileIDs:      map[uuid.UUID]bool{},
					ScoreSum:     0.35,
					ScoreCount:   1,
				}
			}
			stat.PointsToward[k] = math.Max(stat.PointsToward[k], 0.35)
			agg[k].PointsToward[k] = true
			agg[k].FileIDs[fid] = true
		}
	}

	coverage := make([]*types.MaterialSetConceptCoverage, 0, len(agg))
	weights := map[string]float64{}
	now := time.Now().UTC()
	for key, a := range agg {
		if key == "" || a == nil {
			continue
		}
		score := 0.0
		if a.ScoreCount > 0 {
			score = a.ScoreSum / a.ScoreCount
		}
		score = clamp01(score)
		weights[key] = score
		coverageType := "mentions"
		if len(a.Establishes) > 0 {
			coverageType = "introduces"
		} else if len(a.Reinforces) > 0 {
			coverageType = "reinforces"
		} else if len(a.BuildsOn) > 0 {
			coverageType = "assumes"
		}
		depth := "surface"
		if score >= 0.75 {
			depth = "thorough"
		} else if score >= 0.45 {
			depth = "moderate"
		}
		files := make([]string, 0, len(a.FileIDs))
		for fid := range a.FileIDs {
			files = append(files, fid.String())
		}
		sort.Strings(files)
		var canonicalID *uuid.UUID
		if c := conceptByKey[key]; c != nil && c.CanonicalConceptID != nil && *c.CanonicalConceptID != uuid.Nil {
			cid := *c.CanonicalConceptID
			canonicalID = &cid
		} else if c := conceptByKey[key]; c != nil && c.ID != uuid.Nil {
			cid := c.ID
			canonicalID = &cid
		}
		coverage = append(coverage, &types.MaterialSetConceptCoverage{
			ID:                    uuid.New(),
			MaterialSetID:         materialSetID,
			PathID:                &pathID,
			ConceptKey:            key,
			CanonicalConceptID:    canonicalID,
			CoverageType:          coverageType,
			Depth:                 depth,
			Score:                 score,
			SourceMaterialFileIDs: datatypes.JSON(mustJSON(files)),
			Metadata:              datatypes.JSON(mustJSON(map[string]any{"score_count": a.ScoreCount})),
			CreatedAt:             now,
			UpdatedAt:             now,
		})
	}
	return coverage, weights, fileStats
}

func buildMaterialEdges(materialSetID uuid.UUID, fileStats map[uuid.UUID]*fileConceptStat, settings materialSignalSettings) []*types.MaterialEdge {
	if len(fileStats) == 0 {
		return nil
	}
	type fileInfo struct {
		ID  uuid.UUID
		Est map[string]float64
		Rei map[string]float64
		Bld map[string]float64
		Pts map[string]float64
	}
	files := make([]fileInfo, 0, len(fileStats))
	for id, st := range fileStats {
		files = append(files, fileInfo{ID: id, Est: st.Establishes, Rei: st.Reinforces, Bld: st.BuildsOn, Pts: st.PointsToward})
	}
	out := make([]*types.MaterialEdge, 0)
	now := time.Now().UTC()
	for i := range files {
		for j := range files {
			if i == j {
				continue
			}
			a := files[i]
			b := files[j]
			if a.ID == uuid.Nil || b.ID == uuid.Nil {
				continue
			}
			prereq := overlapKeys(a.Est, b.Bld)
			reinf := overlapKeys(a.Est, b.Rei)
			alt := overlapKeys(a.Est, b.Est)
			ext := overlapKeys(a.Pts, b.Est)

			bestType := ""
			bestStrength := 0.0
			bestBridge := []string{}
			scoreOverlap := func(overlap []string, denom int) float64 {
				if denom <= 0 {
					return 0
				}
				return float64(len(overlap)) / float64(denom)
			}
			if len(prereq) > 0 {
				s := scoreOverlap(prereq, len(b.Bld))
				if s > bestStrength {
					bestStrength = s
					bestType = "prerequisite"
					bestBridge = prereq
				}
			}
			if len(reinf) > 0 {
				s := scoreOverlap(reinf, len(b.Rei))
				if s > bestStrength {
					bestStrength = s
					bestType = "reinforces"
					bestBridge = reinf
				}
			}
			if len(ext) > 0 {
				s := scoreOverlap(ext, len(b.Est))
				if s > bestStrength {
					bestStrength = s
					bestType = "extends"
					bestBridge = ext
				}
			}
			if len(alt) > 0 {
				s := scoreOverlap(alt, len(b.Est))
				if s > bestStrength {
					bestStrength = s
					bestType = "alternative"
					bestBridge = alt
				}
			}
			if bestType == "" {
				continue
			}
			if bestStrength < 0.18 {
				continue
			}
			bestBridge = limitStrings(bestBridge, 8)
			out = append(out, &types.MaterialEdge{
				ID:                 uuid.New(),
				MaterialSetID:      materialSetID,
				FromMaterialFileID: a.ID,
				ToMaterialFileID:   b.ID,
				EdgeType:           bestType,
				Strength:           clamp01(bestStrength),
				BridgingConcepts:   datatypes.JSON(mustJSON(bestBridge)),
				Metadata:           datatypes.JSON(mustJSON(map[string]any{"source": "signal_overlap"})),
				CreatedAt:          now,
				UpdatedAt:          now,
			})
		}
	}
	return out
}

func buildChunkLinks(materialSetID uuid.UUID, rows []*types.MaterialChunkSignal, settings materialSignalSettings) []*types.MaterialChunkLink {
	if len(rows) == 0 {
		return nil
	}
	byConcept := map[string][]*types.MaterialChunkSignal{}
	for _, r := range rows {
		if r == nil {
			continue
		}
		var traj map[string]any
		if err := json.Unmarshal(r.Trajectory, &traj); err != nil {
			continue
		}
		keys := append(append(dedupeStrings(stringSliceFromAny(traj["establishes"])), dedupeStrings(stringSliceFromAny(traj["reinforces"]))...),
			dedupeStrings(stringSliceFromAny(traj["builds_on"]))...)
		for _, k := range keys {
			k = strings.TrimSpace(strings.ToLower(k))
			if k == "" {
				continue
			}
			byConcept[k] = append(byConcept[k], r)
		}
	}
	out := make([]*types.MaterialChunkLink, 0)
	now := time.Now().UTC()
	totalLimit := settings.MaxChunkLinks
	if totalLimit <= 0 {
		totalLimit = 1200
	}
	for _, list := range byConcept {
		if len(list) < 2 {
			continue
		}
		sort.Slice(list, func(i, j int) bool {
			return list[i].SignalStrength > list[j].SignalStrength
		})
		if settings.MaxLinksPerConcept > 0 && len(list) > settings.MaxLinksPerConcept {
			list = list[:settings.MaxLinksPerConcept]
		}
		for i := 0; i < len(list)-1; i++ {
			for j := i + 1; j < len(list); j++ {
				if len(out) >= totalLimit {
					return out
				}
				a := list[i]
				b := list[j]
				relation := "reinforces"
				if strings.EqualFold(a.Role, b.Role) {
					relation = "redundant"
				}
				strength := clamp01((a.SignalStrength + b.SignalStrength) / 2)
				out = append(out, &types.MaterialChunkLink{
					ID:            uuid.New(),
					MaterialSetID: materialSetID,
					FromChunkID:   a.MaterialChunkID,
					ToChunkID:     b.MaterialChunkID,
					Relation:      relation,
					Strength:      strength,
					Metadata:      datatypes.JSON(mustJSON(map[string]any{"source": "concept_overlap"})),
					CreatedAt:     now,
					UpdatedAt:     now,
				})
			}
		}
	}
	return out
}

func upsertMaterialSetIntent(ctx context.Context, deps MaterialSignalBuildDeps, materialSetID uuid.UUID, files []*types.MaterialFile, intents map[uuid.UUID]*types.MaterialIntent, coverage []*types.MaterialSetConceptCoverage, edges []*types.MaterialEdge, settings materialSignalSettings) error {
	if deps.DB == nil || deps.AI == nil {
		return nil
	}
	intentList := make([]map[string]any, 0, len(intents))
	for _, f := range files {
		if f == nil {
			continue
		}
		mi := intents[f.ID]
		if mi == nil {
			continue
		}
		intentList = append(intentList, map[string]any{
			"file_id":               f.ID.String(),
			"original_name":         strings.TrimSpace(f.OriginalName),
			"from_state":            mi.FromState,
			"to_state":              mi.ToState,
			"core_thread":           mi.CoreThread,
			"destination_concepts":  jsonListFromRaw(mi.DestinationConcepts),
			"prerequisite_concepts": jsonListFromRaw(mi.PrerequisiteConcepts),
			"assumed_knowledge":     jsonListFromRaw(mi.AssumedKnowledge),
		})
	}
	if len(intentList) == 0 {
		return nil
	}
	intentsJSON, _ := json.Marshal(map[string]any{"files": intentList})

	topConcepts := make([]map[string]any, 0, 24)
	for _, c := range coverage {
		if c == nil {
			continue
		}
		topConcepts = append(topConcepts, map[string]any{
			"concept_key":   c.ConceptKey,
			"coverage_type": c.CoverageType,
			"depth":         c.Depth,
			"score":         c.Score,
		})
	}
	sort.Slice(topConcepts, func(i, j int) bool {
		return floatFromAny(topConcepts[i]["score"], 0) > floatFromAny(topConcepts[j]["score"], 0)
	})
	if len(topConcepts) > 24 {
		topConcepts = topConcepts[:24]
	}
	coverageJSON, _ := json.Marshal(map[string]any{"top_concepts": topConcepts})

	edgeHints := make([]map[string]any, 0, len(edges))
	for _, e := range edges {
		if e == nil {
			continue
		}
		edgeHints = append(edgeHints, map[string]any{
			"from_file_id":      e.FromMaterialFileID.String(),
			"to_file_id":        e.ToMaterialFileID.String(),
			"relation":          e.EdgeType,
			"strength":          e.Strength,
			"bridging_concepts": jsonListFromRaw(e.BridgingConcepts),
		})
	}
	edgesJSON, _ := json.Marshal(map[string]any{"edges": edgeHints})

	ctxJSON, _ := json.Marshal(map[string]any{
		"material_set_id": materialSetID.String(),
		"file_count":      len(files),
	})
	p, err := prompts.Build(prompts.PromptMaterialSetSignal, prompts.Input{
		MaterialContextJSON:     string(ctxJSON),
		MaterialIntentsJSON:     string(intentsJSON),
		MaterialSetCoverageJSON: string(coverageJSON),
		MaterialSetEdgesJSON:    string(edgesJSON),
	})
	if err != nil {
		return err
	}
	obj, err := deps.AI.GenerateJSON(ctx, p.System, p.User, p.SchemaName, p.Schema)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	row := &types.MaterialSetIntent{
		ID:                       uuid.New(),
		MaterialSetID:            materialSetID,
		FromState:                strings.TrimSpace(stringFromAny(obj["from_state"])),
		ToState:                  strings.TrimSpace(stringFromAny(obj["to_state"])),
		CoreThread:               strings.TrimSpace(stringFromAny(obj["core_thread"])),
		SpineMaterialFileIDs:     datatypes.JSON(mustJSON(dedupeStrings(stringSliceFromAny(obj["spine_file_ids"])))),
		SatelliteMaterialFileIDs: datatypes.JSON(mustJSON(dedupeStrings(stringSliceFromAny(obj["satellite_file_ids"])))),
		GapsConceptKeys:          datatypes.JSON(mustJSON(dedupeStrings(stringSliceFromAny(obj["gaps_concept_keys"])))),
		RedundancyNotes:          datatypes.JSON(mustJSON(dedupeStrings(stringSliceFromAny(obj["redundancy_notes"])))),
		ConflictNotes:            datatypes.JSON(mustJSON(dedupeStrings(stringSliceFromAny(obj["conflict_notes"])))),
		Metadata:                 datatypes.JSON(mustJSON(map[string]any{"edge_hints": obj["edge_hints"]})),
		CreatedAt:                now,
		UpdatedAt:                now,
	}
	return deps.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "material_set_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"from_state",
			"to_state",
			"core_thread",
			"spine_material_file_ids",
			"satellite_material_file_ids",
			"gaps_concept_keys",
			"redundancy_notes",
			"conflict_notes",
			"metadata",
			"updated_at",
		}),
	}).Create(row).Error
}

type crossSetOutput struct {
	SetEdgesUpserted       int
	GlobalCoverageUpserted int
	EmergentUpserted       int
	CrossSetByKey          map[string]float64
}

func buildCrossSetSignals(ctx context.Context, deps MaterialSignalBuildDeps, userID uuid.UUID, settings materialSignalSettings) (crossSetOutput, error) {
	out := crossSetOutput{}
	if deps.DB == nil || deps.MaterialSets == nil {
		return out, nil
	}
	sets, err := deps.MaterialSets.GetByUserIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{userID})
	if err != nil {
		return out, err
	}
	sourceSets := make([]*types.MaterialSet, 0)
	for _, s := range sets {
		if s == nil || s.ID == uuid.Nil {
			continue
		}
		if s.SourceMaterialSetID != nil && *s.SourceMaterialSetID != uuid.Nil {
			continue
		}
		sourceSets = append(sourceSets, s)
	}
	if len(sourceSets) < 2 {
		return out, nil
	}

	setIDs := make([]uuid.UUID, 0, len(sourceSets))
	for _, s := range sourceSets {
		setIDs = append(setIDs, s.ID)
	}

	// Load set intents + coverage.
	var intents []*types.MaterialSetIntent
	_ = deps.DB.WithContext(ctx).Model(&types.MaterialSetIntent{}).Where("material_set_id IN ?", setIDs).Find(&intents).Error
	intentBySet := map[uuid.UUID]*types.MaterialSetIntent{}
	for _, it := range intents {
		if it != nil {
			intentBySet[it.MaterialSetID] = it
		}
	}

	var coverage []*types.MaterialSetConceptCoverage
	_ = deps.DB.WithContext(ctx).Model(&types.MaterialSetConceptCoverage{}).Where("material_set_id IN ?", setIDs).Find(&coverage).Error

	coverageBySet := map[uuid.UUID][]*types.MaterialSetConceptCoverage{}
	for _, c := range coverage {
		if c == nil {
			continue
		}
		coverageBySet[c.MaterialSetID] = append(coverageBySet[c.MaterialSetID], c)
	}

	keyToCanonical := map[string]uuid.UUID{}
	for _, c := range coverage {
		if c == nil || c.CanonicalConceptID == nil || *c.CanonicalConceptID == uuid.Nil {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(c.ConceptKey))
		if key == "" {
			continue
		}
		keyToCanonical[key] = *c.CanonicalConceptID
	}

	globalCovAgg := map[uuid.UUID]*globalConceptAgg{}
	for _, c := range coverage {
		if c == nil || c.CanonicalConceptID == nil || *c.CanonicalConceptID == uuid.Nil {
			continue
		}
		id := *c.CanonicalConceptID
		agg := globalCovAgg[id]
		if agg == nil {
			agg = &globalConceptAgg{Sets: map[uuid.UUID]bool{}}
			globalCovAgg[id] = agg
		}
		agg.Sets[c.MaterialSetID] = true
		agg.ScoreSum += c.Score
		agg.ScoreCount += 1
	}

	now := time.Now().UTC()
	globalRows := make([]*types.GlobalConceptCoverage, 0, len(globalCovAgg))
	for id, agg := range globalCovAgg {
		if id == uuid.Nil || agg == nil {
			continue
		}
		setIDs := make([]string, 0, len(agg.Sets))
		for sid := range agg.Sets {
			setIDs = append(setIDs, sid.String())
		}
		sort.Strings(setIDs)
		score := 0.0
		if agg.ScoreCount > 0 {
			score = agg.ScoreSum / agg.ScoreCount
		}
		exposure := clamp01(float64(len(setIDs)) / 5.0)
		globalRows = append(globalRows, &types.GlobalConceptCoverage{
			ID:                uuid.New(),
			UserID:            userID,
			GlobalConceptID:   id,
			MaterialSetIDs:    datatypes.JSON(mustJSON(setIDs)),
			CoverageDepth:     clamp01(score),
			ExposureScore:     exposure,
			CrossSetRelevance: 0,
			Metadata:          datatypes.JSON(mustJSON(map[string]any{"source": "material_signal_build"})),
			CreatedAt:         now,
			UpdatedAt:         now,
		})
	}

	setEdges := buildMaterialSetEdges(userID, sourceSets, coverageBySet)

	if len(globalRows) > 0 {
		gapIDs := map[uuid.UUID]bool{}
		for _, it := range intents {
			if it == nil {
				continue
			}
			for _, k := range jsonListFromRaw(it.GapsConceptKeys) {
				key := strings.TrimSpace(strings.ToLower(k))
				if key == "" {
					continue
				}
				if id, ok := keyToCanonical[key]; ok && id != uuid.Nil {
					gapIDs[id] = true
				}
			}
		}

		bridgeIDs := map[uuid.UUID]bool{}
		var existingEdges []*types.MaterialSetEdge
		_ = deps.DB.WithContext(ctx).Model(&types.MaterialSetEdge{}).Where("user_id = ?", userID).Find(&existingEdges).Error
		for _, e := range append(existingEdges, setEdges...) {
			if e == nil {
				continue
			}
			for _, k := range jsonListFromRaw(e.BridgingConceptIDs) {
				key := strings.TrimSpace(strings.ToLower(k))
				if key == "" {
					continue
				}
				if id, ok := keyToCanonical[key]; ok && id != uuid.Nil {
					bridgeIDs[id] = true
				}
			}
		}

		emergentIDs := map[uuid.UUID]bool{}
		var emergent []*types.EmergentConcept
		_ = deps.DB.WithContext(ctx).Model(&types.EmergentConcept{}).Where("user_id = ?", userID).Find(&emergent).Error
		for _, e := range emergent {
			if e == nil {
				continue
			}
			for _, k := range jsonListFromRaw(e.PrereqConceptIDs) {
				key := strings.TrimSpace(strings.ToLower(k))
				if key == "" {
					continue
				}
				if id, ok := keyToCanonical[key]; ok && id != uuid.Nil {
					emergentIDs[id] = true
				}
			}
		}

		out.CrossSetByKey = map[string]float64{}
		for _, row := range globalRows {
			if row == nil {
				continue
			}
			setCount := len(jsonListFromRaw(row.MaterialSetIDs))
			exposure := clamp01(float64(setCount) / 4.0)
			base := clamp01(0.35*row.CoverageDepth + 0.45*exposure)
			boost := 0.0
			if bridgeIDs[row.GlobalConceptID] {
				boost += 0.2
			}
			if emergentIDs[row.GlobalConceptID] {
				boost += 0.25
			}
			if gapIDs[row.GlobalConceptID] {
				boost += 0.2
			}
			row.CrossSetRelevance = clamp01(base + boost)
			row.Metadata = datatypes.JSON(mustJSON(map[string]any{
				"source":    "material_signal_build",
				"bridge":    bridgeIDs[row.GlobalConceptID],
				"emergent":  emergentIDs[row.GlobalConceptID],
				"gap":       gapIDs[row.GlobalConceptID],
				"set_count": setCount,
			}))
		}

		if err := deps.DB.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "global_concept_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"material_set_ids",
				"coverage_depth",
				"exposure_score",
				"cross_set_relevance",
				"metadata",
				"updated_at",
			}),
		}).CreateInBatches(&globalRows, 200).Error; err != nil {
			return out, err
		}
		out.GlobalCoverageUpserted = len(globalRows)
		scoreByID := map[uuid.UUID]float64{}
		for _, row := range globalRows {
			if row != nil && row.GlobalConceptID != uuid.Nil {
				scoreByID[row.GlobalConceptID] = row.CrossSetRelevance
			}
		}
		for key, id := range keyToCanonical {
			if id == uuid.Nil {
				continue
			}
			if v, ok := scoreByID[id]; ok {
				out.CrossSetByKey[key] = v
			}
		}
	}

	if len(setEdges) > 0 {
		if err := deps.DB.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "from_material_set_id"}, {Name: "to_material_set_id"}, {Name: "relation"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"strength",
				"bridging_concept_ids",
				"metadata",
				"updated_at",
			}),
		}).CreateInBatches(&setEdges, 200).Error; err != nil {
			return out, err
		}
		out.SetEdgesUpserted = len(setEdges)
	}

	if deps.AI != nil {
		userSetsJSON := buildUserSetsJSON(sourceSets, intentBySet, coverageBySet)
		if strings.TrimSpace(userSetsJSON) != "" {
			p, err := prompts.Build(prompts.PromptCrossSetSignal, prompts.Input{
				UserSetsJSON: userSetsJSON,
			})
			if err == nil {
				if obj, err := deps.AI.GenerateJSON(ctx, p.System, p.User, p.SchemaName, p.Schema); err == nil {
					emergentRows := parseEmergentConcepts(obj, userID)
					if len(emergentRows) > 0 {
						if err := deps.DB.WithContext(ctx).Clauses(clause.OnConflict{
							Columns: []clause.Column{{Name: "user_id"}, {Name: "key"}},
							DoUpdates: clause.AssignmentColumns([]string{
								"name",
								"summary",
								"source_material_set_ids",
								"prereq_concept_ids",
								"metadata",
								"updated_at",
							}),
						}).CreateInBatches(&emergentRows, 200).Error; err == nil {
							out.EmergentUpserted = len(emergentRows)
						}
					}
					if edgeHints := parseMaterialSetEdgesFromLLM(obj, userID); len(edgeHints) > 0 {
						_ = deps.DB.WithContext(ctx).Clauses(clause.OnConflict{
							Columns: []clause.Column{{Name: "user_id"}, {Name: "from_material_set_id"}, {Name: "to_material_set_id"}, {Name: "relation"}},
							DoUpdates: clause.AssignmentColumns([]string{
								"strength",
								"bridging_concept_ids",
								"metadata",
								"updated_at",
							}),
						}).CreateInBatches(&edgeHints, 200).Error
						out.SetEdgesUpserted += len(edgeHints)
					}
				}
			}
		}
	}
	return out, nil
}

type globalConceptAgg struct {
	Sets       map[uuid.UUID]bool
	ScoreSum   float64
	ScoreCount float64
}

func buildMaterialSetEdges(userID uuid.UUID, sets []*types.MaterialSet, coverageBySet map[uuid.UUID][]*types.MaterialSetConceptCoverage) []*types.MaterialSetEdge {
	out := make([]*types.MaterialSetEdge, 0)
	if len(sets) < 2 {
		return out
	}
	bySetConceptType := func(setID uuid.UUID) (map[string]bool, map[string]bool, map[string]bool) {
		introduces := map[string]bool{}
		assumes := map[string]bool{}
		reinforces := map[string]bool{}
		for _, c := range coverageBySet[setID] {
			if c == nil {
				continue
			}
			key := strings.TrimSpace(strings.ToLower(c.ConceptKey))
			if key == "" {
				continue
			}
			switch strings.ToLower(c.CoverageType) {
			case "introduces":
				introduces[key] = true
			case "assumes":
				assumes[key] = true
			case "reinforces":
				reinforces[key] = true
			}
		}
		return introduces, assumes, reinforces
	}
	now := time.Now().UTC()
	for i := range sets {
		for j := range sets {
			if i == j {
				continue
			}
			a := sets[i]
			b := sets[j]
			if a == nil || b == nil {
				continue
			}
			aIntro, _, aReinf := bySetConceptType(a.ID)
			_, bAssume, bReinf := bySetConceptType(b.ID)
			bridge := overlapKeyList(aIntro, bAssume)
			strength := 0.0
			if len(bAssume) > 0 {
				strength = float64(len(bridge)) / float64(len(bAssume))
			}
			relation := ""
			if strength >= 0.3 {
				relation = "prerequisite"
			} else {
				overlap := overlapKeyList(aReinf, bReinf)
				if len(overlap) > 0 {
					relation = "parallel"
					bridge = overlap
					if len(bReinf) > 0 {
						strength = float64(len(overlap)) / float64(len(bReinf))
					}
				}
			}
			if relation == "" || strength < 0.2 {
				continue
			}
			out = append(out, &types.MaterialSetEdge{
				ID:                 uuid.New(),
				UserID:             userID,
				FromMaterialSetID:  a.ID,
				ToMaterialSetID:    b.ID,
				Relation:           relation,
				Strength:           clamp01(strength),
				BridgingConceptIDs: datatypes.JSON(mustJSON(limitStrings(bridge, 10))),
				Metadata:           datatypes.JSON(mustJSON(map[string]any{"source": "coverage_overlap"})),
				CreatedAt:          now,
				UpdatedAt:          now,
			})
		}
	}
	return out
}

func buildUserSetsJSON(sets []*types.MaterialSet, intents map[uuid.UUID]*types.MaterialSetIntent, coverageBySet map[uuid.UUID][]*types.MaterialSetConceptCoverage) string {
	payload := make([]map[string]any, 0, len(sets))
	for _, s := range sets {
		if s == nil {
			continue
		}
		it := intents[s.ID]
		intentFrom := ""
		intentTo := ""
		intentThread := ""
		if it != nil {
			intentFrom = strings.TrimSpace(it.FromState)
			intentTo = strings.TrimSpace(it.ToState)
			intentThread = strings.TrimSpace(it.CoreThread)
		}
		topConcepts := make([]map[string]any, 0, 10)
		for _, c := range coverageBySet[s.ID] {
			if c == nil {
				continue
			}
			topConcepts = append(topConcepts, map[string]any{
				"concept_key":   c.ConceptKey,
				"coverage_type": c.CoverageType,
				"score":         c.Score,
			})
		}
		sort.Slice(topConcepts, func(i, j int) bool {
			return floatFromAny(topConcepts[i]["score"], 0) > floatFromAny(topConcepts[j]["score"], 0)
		})
		if len(topConcepts) > 12 {
			topConcepts = topConcepts[:12]
		}
		payload = append(payload, map[string]any{
			"material_set_id": s.ID.String(),
			"title":           strings.TrimSpace(s.Title),
			"intent": map[string]any{
				"from_state":  intentFrom,
				"to_state":    intentTo,
				"core_thread": intentThread,
			},
			"top_concepts": topConcepts,
		})
	}
	b, _ := json.Marshal(map[string]any{"sets": payload})
	return string(b)
}

func parseEmergentConcepts(obj map[string]any, userID uuid.UUID) []*types.EmergentConcept {
	raw := sliceAny(obj["emergent_concepts"])
	if len(raw) == 0 {
		return nil
	}
	now := time.Now().UTC()
	out := make([]*types.EmergentConcept, 0, len(raw))
	for _, it := range raw {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(stringFromAny(m["key"])))
		if key == "" {
			continue
		}
		out = append(out, &types.EmergentConcept{
			ID:                   uuid.New(),
			UserID:               userID,
			Key:                  key,
			Name:                 strings.TrimSpace(stringFromAny(m["name"])),
			Summary:              strings.TrimSpace(stringFromAny(m["summary"])),
			SourceMaterialSetIDs: datatypes.JSON(mustJSON(dedupeStrings(stringSliceFromAny(m["source_set_ids"])))),
			PrereqConceptIDs:     datatypes.JSON(mustJSON(dedupeStrings(stringSliceFromAny(m["prereq_concept_keys"])))),
			Metadata:             datatypes.JSON(mustJSON(map[string]any{"source": "cross_set_signal"})),
			CreatedAt:            now,
			UpdatedAt:            now,
		})
	}
	return out
}

func parseMaterialSetEdgesFromLLM(obj map[string]any, userID uuid.UUID) []*types.MaterialSetEdge {
	raw := sliceAny(obj["set_edges"])
	if len(raw) == 0 {
		return nil
	}
	now := time.Now().UTC()
	out := make([]*types.MaterialSetEdge, 0, len(raw))
	for _, it := range raw {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		fromID, _ := uuid.Parse(strings.TrimSpace(stringFromAny(m["from_set_id"])))
		toID, _ := uuid.Parse(strings.TrimSpace(stringFromAny(m["to_set_id"])))
		if fromID == uuid.Nil || toID == uuid.Nil {
			continue
		}
		out = append(out, &types.MaterialSetEdge{
			ID:                 uuid.New(),
			UserID:             userID,
			FromMaterialSetID:  fromID,
			ToMaterialSetID:    toID,
			Relation:           strings.TrimSpace(stringFromAny(m["relation"])),
			Strength:           clamp01(floatFromAny(m["strength"], 0.4)),
			BridgingConceptIDs: datatypes.JSON(mustJSON(dedupeStrings(stringSliceFromAny(m["bridging_concepts"])))),
			Metadata:           datatypes.JSON(mustJSON(map[string]any{"source": "cross_set_signal"})),
			CreatedAt:          now,
			UpdatedAt:          now,
		})
	}
	return out
}

func applyConceptSignalWeights(ctx context.Context, db *gorm.DB, conceptByKey map[string]*types.Concept, weights map[string]float64) error {
	if db == nil || len(weights) == 0 {
		return nil
	}
	for key, w := range weights {
		if key == "" {
			continue
		}
		c := conceptByKey[strings.TrimSpace(strings.ToLower(key))]
		if c == nil || c.ID == uuid.Nil {
			continue
		}
		meta := map[string]any{}
		if len(c.Metadata) > 0 && strings.TrimSpace(string(c.Metadata)) != "" && strings.TrimSpace(string(c.Metadata)) != "null" {
			_ = json.Unmarshal(c.Metadata, &meta)
		}
		if meta == nil {
			meta = map[string]any{}
		}
		meta["signal_weight"] = clamp01(w)
		if err := db.WithContext(ctx).Model(&types.Concept{}).Where("id = ?", c.ID).Updates(map[string]any{
			"metadata":   datatypes.JSON(mustJSON(meta)),
			"updated_at": time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func dedupeMaterialIntentRows(rows []*types.MaterialIntent) []*types.MaterialIntent {
	if len(rows) == 0 {
		return rows
	}
	byID := map[uuid.UUID]*types.MaterialIntent{}
	for _, r := range rows {
		if r == nil || r.MaterialFileID == uuid.Nil {
			continue
		}
		byID[r.MaterialFileID] = r
	}
	out := make([]*types.MaterialIntent, 0, len(byID))
	for _, r := range byID {
		out = append(out, r)
	}
	return out
}

func dedupeMaterialChunkSignalRows(rows []*types.MaterialChunkSignal) []*types.MaterialChunkSignal {
	if len(rows) == 0 {
		return rows
	}
	byID := map[uuid.UUID]*types.MaterialChunkSignal{}
	for _, r := range rows {
		if r == nil || r.MaterialChunkID == uuid.Nil {
			continue
		}
		ex := byID[r.MaterialChunkID]
		if ex == nil || r.SignalStrength > ex.SignalStrength {
			byID[r.MaterialChunkID] = r
		}
	}
	out := make([]*types.MaterialChunkSignal, 0, len(byID))
	for _, r := range byID {
		out = append(out, r)
	}
	return out
}

func dedupeMaterialSetCoverageRows(rows []*types.MaterialSetConceptCoverage) []*types.MaterialSetConceptCoverage {
	if len(rows) == 0 {
		return rows
	}
	byKey := map[string]*types.MaterialSetConceptCoverage{}
	for _, r := range rows {
		if r == nil {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(r.ConceptKey))
		if key == "" {
			continue
		}
		ex := byKey[key]
		if ex == nil || r.Score > ex.Score {
			byKey[key] = r
		}
	}
	out := make([]*types.MaterialSetConceptCoverage, 0, len(byKey))
	for _, r := range byKey {
		out = append(out, r)
	}
	return out
}

func dedupeMaterialEdgeRows(rows []*types.MaterialEdge) []*types.MaterialEdge {
	if len(rows) == 0 {
		return rows
	}
	type key struct {
		from uuid.UUID
		to   uuid.UUID
		typ  string
	}
	byKey := map[key]*types.MaterialEdge{}
	for _, r := range rows {
		if r == nil || r.FromMaterialFileID == uuid.Nil || r.ToMaterialFileID == uuid.Nil {
			continue
		}
		k := key{from: r.FromMaterialFileID, to: r.ToMaterialFileID, typ: strings.ToLower(strings.TrimSpace(r.EdgeType))}
		ex := byKey[k]
		if ex == nil || r.Strength > ex.Strength {
			byKey[k] = r
		}
	}
	out := make([]*types.MaterialEdge, 0, len(byKey))
	for _, r := range byKey {
		out = append(out, r)
	}
	return out
}

func dedupeChunkLinkRows(rows []*types.MaterialChunkLink) []*types.MaterialChunkLink {
	if len(rows) == 0 {
		return rows
	}
	type key struct {
		from uuid.UUID
		to   uuid.UUID
	}
	byKey := map[key]*types.MaterialChunkLink{}
	for _, r := range rows {
		if r == nil || r.FromChunkID == uuid.Nil || r.ToChunkID == uuid.Nil {
			continue
		}
		k := key{from: r.FromChunkID, to: r.ToChunkID}
		ex := byKey[k]
		if ex == nil || r.Strength > ex.Strength {
			byKey[k] = r
		}
	}
	out := make([]*types.MaterialChunkLink, 0, len(byKey))
	for _, r := range byKey {
		out = append(out, r)
	}
	return out
}

type setPositionContext struct {
	SpineFiles     map[uuid.UUID]bool
	SatelliteFiles map[uuid.UUID]bool
	CoreThread     string
	GapKeys        map[string]bool
	RedundantText  string
}

func computeSetPositionScores(intent *types.MaterialSetIntent, rows []*types.MaterialChunkSignal) map[uuid.UUID]float64 {
	if intent == nil || len(rows) == 0 {
		return nil
	}
	ctx := buildSetPositionContext(intent)
	out := map[uuid.UUID]float64{}
	for _, r := range rows {
		if r == nil || r.MaterialChunkID == uuid.Nil {
			continue
		}
		keys := conceptKeysFromTrajectory(r.Trajectory)
		out[r.MaterialChunkID] = computeSetPositionScoreForChunk(r.MaterialFileID, keys, ctx)
	}
	return out
}

func buildSetPositionContext(intent *types.MaterialSetIntent) setPositionContext {
	ctx := setPositionContext{
		SpineFiles:     map[uuid.UUID]bool{},
		SatelliteFiles: map[uuid.UUID]bool{},
		CoreThread:     strings.ToLower(strings.TrimSpace(intent.CoreThread)),
		GapKeys:        map[string]bool{},
		RedundantText:  "",
	}
	for _, s := range jsonListFromRaw(intent.SpineMaterialFileIDs) {
		if id, err := uuid.Parse(strings.TrimSpace(s)); err == nil && id != uuid.Nil {
			ctx.SpineFiles[id] = true
		}
	}
	for _, s := range jsonListFromRaw(intent.SatelliteMaterialFileIDs) {
		if id, err := uuid.Parse(strings.TrimSpace(s)); err == nil && id != uuid.Nil {
			ctx.SatelliteFiles[id] = true
		}
	}
	for _, k := range jsonListFromRaw(intent.GapsConceptKeys) {
		key := strings.TrimSpace(strings.ToLower(k))
		if key != "" {
			ctx.GapKeys[key] = true
		}
	}
	ctx.RedundantText = strings.ToLower(strings.Join(jsonListFromRaw(intent.RedundancyNotes), " "))
	return ctx
}

func computeSetPositionScoreForChunk(fileID uuid.UUID, conceptKeys []string, ctx setPositionContext) float64 {
	base := 0.8
	if fileID != uuid.Nil && ctx.SpineFiles[fileID] {
		base = 1.0
	} else if fileID != uuid.Nil && ctx.SatelliteFiles[fileID] {
		base = 0.6
	}
	if len(conceptKeys) > 0 {
		if ctx.CoreThread != "" {
			for _, k := range conceptKeys {
				if k != "" && strings.Contains(ctx.CoreThread, k) {
					base += 0.2
					break
				}
			}
		}
		for _, k := range conceptKeys {
			if ctx.GapKeys[k] {
				base += 0.3
				break
			}
		}
		if ctx.RedundantText != "" {
			for _, k := range conceptKeys {
				if k != "" && strings.Contains(ctx.RedundantText, k) {
					base -= 0.2
					break
				}
			}
		}
	}
	return clamp01(base)
}

func upsertChunkSignalScores(ctx context.Context, db *gorm.DB, setPos map[uuid.UUID]float64, alignment map[uuid.UUID]float64) error {
	if db == nil || len(setPos) == 0 {
		return nil
	}
	rows := make([]*types.MaterialChunkSignal, 0, len(setPos))
	now := time.Now().UTC()
	for id, score := range setPos {
		if id == uuid.Nil {
			continue
		}
		row := &types.MaterialChunkSignal{
			MaterialChunkID:  id,
			SetPositionScore: clamp01(score),
			UpdatedAt:        now,
		}
		if alignment != nil {
			if v, ok := alignment[id]; ok {
				row.IntentAlignmentScore = clamp01(v)
			}
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil
	}
	rows = dedupeMaterialChunkSignalRows(rows)
	cols := []string{"set_position_score", "updated_at"}
	if alignment != nil {
		cols = append(cols, "intent_alignment_score")
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "material_chunk_id"}},
		DoUpdates: clause.AssignmentColumns(cols),
	}).CreateInBatches(&rows, 200).Error
}

func upsertChunkSignalCompoundWeights(ctx context.Context, db *gorm.DB, rows []*types.MaterialChunkSignal, setIntent *types.MaterialSetIntent, crossSetByKey map[string]float64) error {
	if db == nil || len(rows) == 0 {
		return nil
	}
	ctxPos := setPositionContext{}
	if setIntent != nil {
		ctxPos = buildSetPositionContext(setIntent)
	}
	updates := make([]*types.MaterialChunkSignal, 0, len(rows))
	now := time.Now().UTC()
	for _, r := range rows {
		if r == nil || r.MaterialChunkID == uuid.Nil {
			continue
		}
		alignment := r.IntentAlignmentScore
		if alignment <= 0 {
			alignment = 0.5
		}
		setPos := r.SetPositionScore
		if setPos <= 0 {
			keys := conceptKeysFromTrajectory(r.Trajectory)
			setPos = computeSetPositionScoreForChunk(r.MaterialFileID, keys, ctxPos)
		}
		crossSet := 0.5
		if len(crossSetByKey) > 0 {
			keys := conceptKeysFromTrajectory(r.Trajectory)
			maxVal := 0.0
			for _, k := range keys {
				if v, ok := crossSetByKey[k]; ok && v > maxVal {
					maxVal = v
				}
			}
			if maxVal > 0 {
				crossSet = maxVal
			}
		}
		compound := clamp01(r.SignalStrength) * clamp01(alignment) * clamp01(setPos) * clamp01(crossSet)
		updates = append(updates, &types.MaterialChunkSignal{
			MaterialChunkID:      r.MaterialChunkID,
			IntentAlignmentScore: clamp01(alignment),
			SetPositionScore:     clamp01(setPos),
			CompoundWeight:       clamp01(compound),
			UpdatedAt:            now,
		})
	}
	if len(updates) == 0 {
		return nil
	}
	updates = dedupeMaterialChunkSignalRows(updates)
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "material_chunk_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"intent_alignment_score",
			"set_position_score",
			"compound_weight",
			"updated_at",
		}),
	}).CreateInBatches(&updates, 200).Error
}

func overlapKeys(a map[string]float64, b map[string]float64) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	out := make([]string, 0)
	for k := range a {
		if b[k] > 0 {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func overlapKeyList(a map[string]bool, b map[string]bool) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	out := make([]string, 0)
	for k := range a {
		if b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func limitStrings(in []string, max int) []string {
	if max <= 0 || len(in) <= max {
		return in
	}
	return in[:max]
}

func jsonListFromRaw(raw datatypes.JSON) []string {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err == nil {
		return dedupeStrings(out)
	}
	var tmp []any
	if err := json.Unmarshal(raw, &tmp); err != nil {
		return []string{}
	}
	out = make([]string, 0, len(tmp))
	for _, v := range tmp {
		s := strings.TrimSpace(fmt.Sprint(v))
		if s != "" {
			out = append(out, s)
		}
	}
	return dedupeStrings(out)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

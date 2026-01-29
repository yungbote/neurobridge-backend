package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/data/materialsetctx"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/prompts"
	"golang.org/x/sync/errgroup"
)

type conceptCoverage struct {
	Confidence    float64
	Notes         string
	MissingTopics []string
}

func parseConceptCoverage(obj map[string]any) conceptCoverage {
	out := conceptCoverage{}
	if obj == nil {
		return out
	}
	raw, _ := obj["coverage"].(map[string]any)
	if raw == nil {
		return out
	}
	out.Confidence = floatFromAny(raw["confidence"], 0)
	out.Notes = strings.TrimSpace(stringFromAny(raw["notes"]))
	out.MissingTopics = dedupeStrings(stringSliceFromAny(raw["missing_topics_suspected"]))
	return out
}

type conceptCoverageInput struct {
	PathID        uuid.UUID
	MaterialSetID uuid.UUID
	IntentMD      string

	Chunks    []*types.MaterialChunk
	ChunkByID map[uuid.UUID]*types.MaterialChunk
	ChunkEmbs []chunkEmbedding

	AllowedChunkIDs map[string]bool
	InitialChunkIDs []uuid.UUID

	InitialCoverage conceptCoverage
	Concepts        []conceptInvItem

	// Optional file allowlist from path intake.
	MaterialFileFilter map[uuid.UUID]bool

	// Optional overrides for coverage tuning.
	Passes           int
	MaxConcepts      int
	ExtraPerFile     int
	ExtraMaxChars    int
	ExtraMaxLines    int
	ExtraMaxTotal    int
	MaxMissingTopics int
	TopicTopK        int
	TargetedOnly     bool

	AdaptiveEnabled bool
	Signals         AdaptiveSignals
	Stage           string

	Progress      func(pct int, msg string)
	ProgressStart int
	ProgressEnd   int
}

func conceptGraphCoveragePasses() int {
	switch qualityMode() {
	case "premium", "openai", "high":
		return 3
	default:
		return 1
	}
}

func envIntAllowZeroWithSet(key string, def int) (int, bool) {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return def, false
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, false
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return def, true
	}
	return val, true
}

type conceptCoverageResult struct {
	Concepts       []conceptInvItem
	AdaptiveParams map[string]any
}

func completeConceptCoverage(ctx context.Context, deps ConceptGraphBuildDeps, in conceptCoverageInput) conceptCoverageResult {
	result := conceptCoverageResult{Concepts: in.Concepts, AdaptiveParams: map[string]any{}}
	if ctx == nil {
		ctx = context.Background()
	}
	if deps.AI == nil || len(in.Concepts) == 0 || len(in.Chunks) == 0 {
		return result
	}

	topicEmbedCache := map[string][]float32{}

	adaptiveEnabled := in.AdaptiveEnabled
	signals := in.Signals
	if adaptiveEnabled && signals.MaterialSetID == uuid.Nil {
		signals = loadAdaptiveSignals(ctx, deps.DB, in.MaterialSetID, in.PathID)
	}

	progress := in.Progress
	if progress == nil {
		progress = func(int, string) {}
	}
	progressStart := in.ProgressStart
	progressEnd := in.ProgressEnd
	if progressStart < 0 {
		progressStart = 0
	}
	if progressEnd < progressStart {
		progressEnd = progressStart
	}

	passes := in.Passes
	passesCeiling, passesCeilingSet := envIntAllowZeroWithSet("CONCEPT_GRAPH_COVERAGE_PASSES", -1)
	if passes <= 0 {
		if adaptiveEnabled {
			ceiling := passesCeiling
			if !passesCeilingSet || ceiling < 0 {
				ceiling = conceptGraphCoveragePasses()
			}
			passes = adaptiveFromRatio(signals.PageCount, 1.0/50.0, 1, ceiling)
		} else {
			if passesCeilingSet && passesCeiling > 0 {
				passes = passesCeiling
			} else {
				passes = conceptGraphCoveragePasses()
			}
		}
	}
	if passes <= 0 {
		return result
	}

	maxConcepts := in.MaxConcepts
	maxConceptsCeiling := envIntAllowZero("CONCEPT_GRAPH_MAX_CONCEPTS", 180)
	if maxConceptsCeiling < 0 {
		maxConceptsCeiling = 0
	}
	if maxConcepts <= 0 {
		if adaptiveEnabled {
			maxConcepts = clampIntCeiling(int(math.Round(float64(signals.PageCount)*0.4)), 40, maxConceptsCeiling)
		} else if maxConceptsCeiling > 0 {
			maxConcepts = maxConceptsCeiling
		} else {
			fallback := int(math.Round(float64(len(in.Chunks)) * 0.2))
			if fallback < 180 {
				fallback = 180
			}
			maxConcepts = fallback
		}
	}
	result.AdaptiveParams["CONCEPT_GRAPH_MAX_CONCEPTS"] = map[string]any{"actual": maxConcepts, "ceiling": maxConceptsCeiling}
	result.AdaptiveParams["CONCEPT_GRAPH_COVERAGE_PASSES"] = map[string]any{"actual": passes, "ceiling": passesCeiling}

	extraPerFile := in.ExtraPerFile
	if extraPerFile <= 0 {
		ceiling := envIntAllowZero("CONCEPT_GRAPH_COVERAGE_EXCERPTS_PER_FILE", 6)
		if adaptiveEnabled {
			extraPerFile = clampIntCeiling(int(math.Round(signals.AvgPagesPerFile/15.0)), 2, ceiling)
		} else {
			extraPerFile = ceiling
		}
	}
	result.AdaptiveParams["CONCEPT_GRAPH_COVERAGE_EXCERPTS_PER_FILE"] = map[string]any{
		"actual":  extraPerFile,
		"ceiling": envIntAllowZero("CONCEPT_GRAPH_COVERAGE_EXCERPTS_PER_FILE", 6),
	}
	extraMaxChars := in.ExtraMaxChars
	extraMaxCharsCeiling := envIntAllowZero("CONCEPT_GRAPH_COVERAGE_EXCERPT_MAX_CHARS", 700)
	if extraMaxChars <= 0 {
		extraMaxChars = extraMaxCharsCeiling
	}
	if extraMaxChars <= 0 {
		extraMaxChars = 700
		extraMaxCharsCeiling = extraMaxChars
	}
	if adaptiveEnabled {
		extraMaxChars = clampIntCeiling(adjustExcerptCharsByContentType(extraMaxChars, signals.ContentType), 200, extraMaxCharsCeiling)
	}
	result.AdaptiveParams["CONCEPT_GRAPH_COVERAGE_EXCERPT_MAX_CHARS"] = map[string]any{"actual": extraMaxChars, "ceiling": extraMaxCharsCeiling}
	extraMaxLines := in.ExtraMaxLines
	extraMaxLinesCeiling := envIntAllowZero("CONCEPT_GRAPH_COVERAGE_EXCERPT_MAX_LINES", 0)
	if extraMaxLines <= 0 {
		extraMaxLines = extraMaxLinesCeiling
	}
	if adaptiveEnabled && extraMaxLines > 0 {
		extraMaxLines = clampIntCeiling(adjustExcerptLinesByContentType(extraMaxLines, signals.ContentType), 8, extraMaxLinesCeiling)
	}
	result.AdaptiveParams["CONCEPT_GRAPH_COVERAGE_EXCERPT_MAX_LINES"] = map[string]any{"actual": extraMaxLines, "ceiling": extraMaxLinesCeiling}
	extraMaxTotal := in.ExtraMaxTotal
	extraMaxTotalCeiling, extraMaxTotalSet := envIntAllowZeroWithSet("CONCEPT_GRAPH_COVERAGE_EXCERPT_MAX_TOTAL_CHARS", 0)
	if extraMaxTotalCeiling < 0 {
		extraMaxTotalCeiling = 0
	}
	if !extraMaxTotalSet {
		if in.TargetedOnly {
			extraMaxTotalCeiling = 20000
		} else {
			extraMaxTotalCeiling = 45000
		}
	}
	if extraMaxTotal <= 0 {
		if adaptiveEnabled {
			if extraMaxTotalCeiling != 0 || !extraMaxTotalSet {
				extraMaxTotal = clampIntCeiling(int(math.Round(float64(signals.PageCount)*250)), 8000, extraMaxTotalCeiling)
			}
		} else {
			extraMaxTotal = extraMaxTotalCeiling
		}
	}
	if extraMaxTotal <= 0 && !extraMaxTotalSet {
		if in.TargetedOnly {
			extraMaxTotal = 20000
		} else {
			extraMaxTotal = 45000
		}
	}
	result.AdaptiveParams["CONCEPT_GRAPH_COVERAGE_EXCERPT_MAX_TOTAL_CHARS"] = map[string]any{
		"actual":  extraMaxTotal,
		"ceiling": extraMaxTotalCeiling,
	}

	maxTopics := in.MaxMissingTopics
	if maxTopics <= 0 {
		ceiling := envIntAllowZero("CONCEPT_GRAPH_COVERAGE_MAX_MISSING_TOPICS", 8)
		if adaptiveEnabled && signals.ConceptCount > 0 {
			maxTopics = adaptiveFromRatio(signals.ConceptCount, 0.05, 6, ceiling)
		} else {
			maxTopics = ceiling
		}
	}
	if adaptiveEnabled && signals.ConceptCount > 120 && maxTopics < 8 {
		maxTopics = 8
	}
	if adaptiveEnabled && signals.ConceptCount > 200 && maxTopics < 10 {
		maxTopics = 10
	}
	result.AdaptiveParams["CONCEPT_GRAPH_COVERAGE_MAX_MISSING_TOPICS"] = map[string]any{
		"actual":  maxTopics,
		"ceiling": envIntAllowZero("CONCEPT_GRAPH_COVERAGE_MAX_MISSING_TOPICS", 8),
	}
	topicTopK := in.TopicTopK
	if topicTopK <= 0 {
		ceiling := envIntAllowZero("CONCEPT_GRAPH_COVERAGE_TOPIC_TOPK", 6)
		if adaptiveEnabled && signals.ConceptCount > 0 {
			topicTopK = adaptiveFromRatio(signals.ConceptCount, 0.03, 4, ceiling)
		} else {
			topicTopK = ceiling
		}
	}
	if topicTopK <= 0 {
		topicTopK = 6
	}
	result.AdaptiveParams["CONCEPT_GRAPH_COVERAGE_TOPIC_TOPK"] = map[string]any{
		"actual":  topicTopK,
		"ceiling": envIntAllowZero("CONCEPT_GRAPH_COVERAGE_TOPIC_TOPK", 6),
	}

	seenChunkIDs := map[uuid.UUID]bool{}
	for _, id := range in.InitialChunkIDs {
		if id != uuid.Nil {
			seenChunkIDs[id] = true
		}
	}

	knownKeys := map[string]bool{}
	for _, c := range in.Concepts {
		if strings.TrimSpace(c.Key) != "" {
			knownKeys[strings.TrimSpace(c.Key)] = true
		}
	}

	missingTopics := in.InitialCoverage.MissingTopics
	concepts := in.Concepts

	batchSize := maxTopics
	if batchSize <= 0 {
		batchSize = 8
	}
	maxTotalTopics := batchSize * passes
	if maxTotalTopics <= 0 {
		maxTotalTopics = batchSize
	}

	maxRounds := passes
	maxRoundsCeiling := envIntAllowZero("CONCEPT_GRAPH_COVERAGE_MAX_ROUNDS", 3)
	if maxRoundsCeiling > 0 && maxRounds > maxRoundsCeiling {
		maxRounds = maxRoundsCeiling
	}
	if maxRounds < 1 {
		maxRounds = 1
	}

	for round := 1; round <= maxRounds; round++ {
		roundStart := progressStart
		roundEnd := progressEnd
		if progressEnd > progressStart {
			span := progressEnd - progressStart
			roundStart = progressStart + int(math.Round(float64(round-1)/float64(maxRounds)*float64(span)))
			roundEnd = progressStart + int(math.Round(float64(round)/float64(maxRounds)*float64(span)))
			if roundEnd < roundStart {
				roundEnd = roundStart
			}
		}
		progress(roundStart, fmt.Sprintf("Coverage pass %d/%d", round, maxRounds))

		if len(knownKeys) >= maxConcepts {
			result.Concepts = concepts
			return result
		}

		remaining := make([]*types.MaterialChunk, 0, len(in.Chunks))
		for _, ch := range in.Chunks {
			if ch == nil || ch.ID == uuid.Nil {
				continue
			}
			if seenChunkIDs[ch.ID] {
				continue
			}
			if isUnextractableChunk(ch) {
				continue
			}
			if strings.TrimSpace(ch.Text) == "" {
				continue
			}
			remaining = append(remaining, ch)
		}
		if len(remaining) == 0 {
			break
		}

		topics := dedupeStrings(missingTopics)
		if len(topics) > maxTotalTopics {
			topics = topics[:maxTotalTopics]
		}
		topicBatches := splitStringBatches(topics, batchSize)
		if len(topicBatches) == 0 {
			topicBatches = [][]string{{}}
		}

		_, stratIDs := stratifiedChunkExcerptsWithLimitsAndIDs(remaining, extraPerFile, extraMaxChars, extraMaxLines, extraMaxTotal)
		stratChunks := make([][]uuid.UUID, len(topicBatches))
		for i, id := range stratIDs {
			stratChunks[i%len(topicBatches)] = append(stratChunks[i%len(topicBatches)], id)
		}

		type coverageTask struct {
			MissingTopics []string
			CandidateIDs  []uuid.UUID
			Excerpts      string
		}
		tasks := make([]coverageTask, 0, len(topicBatches))

		for i, batch := range topicBatches {
			targetIDs := coverageTargetChunkIDs(ctx, deps, in.MaterialSetID, in.MaterialFileFilter, batch, seenChunkIDs, in.ChunkEmbs, maxTopics, topicTopK, topicEmbedCache)
			candidates := targetIDs
			if !in.TargetedOnly || len(candidates) == 0 {
				candidates = append(candidates, stratChunks[i]...)
			}
			deltaExcerpts, usedIDs := renderChunkExcerptsByIDsOrdered(in.ChunkByID, candidates, extraMaxChars, extraMaxTotal)
			if strings.TrimSpace(deltaExcerpts) == "" {
				continue
			}
			for _, id := range usedIDs {
				if id != uuid.Nil {
					seenChunkIDs[id] = true
				}
			}
			tasks = append(tasks, coverageTask{
				MissingTopics: batch,
				CandidateIDs:  candidates,
				Excerpts:      deltaExcerpts,
			})
		}
		if len(tasks) == 0 {
			progress(roundEnd, fmt.Sprintf("Coverage pass %d/%d", round, maxRounds))
			break
		}

		conceptsJSON := conceptsJSONForDelta(concepts)
		var (
			mu             sync.Mutex
			newConceptsAll []conceptInvItem
			nextTopics     []string
		)
		var tasksDone int32

		tg, tctx := errgroup.WithContext(ctx)
		concCeiling := envIntAllowZero("CONCEPT_GRAPH_COVERAGE_CONCURRENCY", 16)
		if concCeiling <= 0 {
			concCeiling = 4
		}
		conc := len(tasks)
		if conc > concCeiling {
			conc = concCeiling
		}
		if conc < 1 {
			conc = 1
		}
		result.AdaptiveParams["CONCEPT_GRAPH_COVERAGE_CONCURRENCY"] = map[string]any{
			"actual":  conc,
			"ceiling": concCeiling,
		}
		tg.SetLimit(conc)

		for _, task := range tasks {
			task := task
			round := round
			tg.Go(func() error {
				defer func() {
					if len(tasks) > 0 {
						done := int(atomic.AddInt32(&tasksDone, 1))
						pct := roundStart
						if roundEnd > roundStart {
							pct = roundStart + int(math.Round(float64(done)/float64(len(tasks))*float64(roundEnd-roundStart)))
						}
						progress(pct, fmt.Sprintf("Coverage pass %d/%d (%d/%d)", round, maxRounds, done, len(tasks)))
					}
				}()
				if err := tctx.Err(); err != nil {
					return err
				}
				p, err := prompts.Build(prompts.PromptConceptInventoryDelta, prompts.Input{
					PathIntentMD: in.IntentMD,
					ConceptsJSON: conceptsJSON,
					Excerpts:     task.Excerpts,
				})
				if err != nil {
					if deps.Log != nil {
						deps.Log.Warn("concept_graph_build: coverage delta prompt build failed (continuing)", "error", err, "path_id", in.PathID.String())
					}
					return nil
				}

				timer := llmTimer(deps.Log, "concept_inventory_delta", map[string]any{
					"stage":         "concept_graph_build",
					"path_id":       in.PathID.String(),
					"round":         round,
					"topic_count":   len(task.MissingTopics),
					"excerpt_chars": len(task.Excerpts),
				})
				obj, err := deps.AI.GenerateJSON(tctx, p.System, p.User, p.SchemaName, p.Schema)
				timer(err)
				if err != nil && isContextLengthExceeded(err) && extraMaxTotal > 12000 {
					maxTotal := maxInt(12000, extraMaxTotal/2)
					shorter, _ := renderChunkExcerptsByIDsOrdered(in.ChunkByID, task.CandidateIDs, extraMaxChars, maxTotal)
					if strings.TrimSpace(shorter) != "" {
						p2, berr := prompts.Build(prompts.PromptConceptInventoryDelta, prompts.Input{
							PathIntentMD: in.IntentMD,
							ConceptsJSON: conceptsJSON,
							Excerpts:     shorter,
						})
						if berr == nil {
							timer = llmTimer(deps.Log, "concept_inventory_delta", map[string]any{
								"stage":         "concept_graph_build",
								"path_id":       in.PathID.String(),
								"round":         round,
								"topic_count":   len(task.MissingTopics),
								"excerpt_chars": len(shorter),
								"retry":         "shorter",
							})
							obj, err = deps.AI.GenerateJSON(tctx, p2.System, p2.User, p2.SchemaName, p2.Schema)
							timer(err)
						}
					}
				}
				if err != nil {
					if deps.Log != nil {
						deps.Log.Warn("concept_graph_build: coverage delta generation failed (continuing)", "error", err, "path_id", in.PathID.String())
					}
					return nil
				}

				newConcepts, cov, perr := parseConceptInventoryDelta(obj)
				if perr != nil {
					if deps.Log != nil {
						deps.Log.Warn("concept_graph_build: coverage delta parse failed (continuing)", "error", perr, "path_id", in.PathID.String())
					}
					return nil
				}
				mu.Lock()
				if len(newConcepts) > 0 {
					newConceptsAll = append(newConceptsAll, newConcepts...)
				}
				if len(cov.MissingTopics) > 0 {
					nextTopics = append(nextTopics, cov.MissingTopics...)
				}
				mu.Unlock()
				return nil
			})
		}

		if err := tg.Wait(); err != nil && tctx.Err() != nil {
			result.Concepts = concepts
			return result
		}
		progress(roundEnd, fmt.Sprintf("Coverage pass %d/%d", round, maxRounds))
		if len(newConceptsAll) == 0 {
			break
		}

		merged, _ := normalizeConceptInventory(append(concepts, newConceptsAll...), in.AllowedChunkIDs)
		merged, _ = dedupeConceptInventoryByKey(merged)

		added := 0
		for _, c := range merged {
			k := strings.TrimSpace(c.Key)
			if k == "" || knownKeys[k] {
				continue
			}
			knownKeys[k] = true
			added++
		}
		if added == 0 {
			break
		}
		concepts = merged
		if deps.Log != nil {
			deps.Log.Info("concept_graph_build: coverage round added concepts", "path_id", in.PathID.String(), "round", round, "added", added, "total", len(knownKeys))
		}

		missingTopics = dedupeStrings(nextTopics)
		if len(missingTopics) == 0 {
			break
		}
	}

	result.Concepts = concepts
	if deps.Log != nil && adaptiveEnabled && len(result.AdaptiveParams) > 0 {
		stage := strings.TrimSpace(in.Stage)
		if stage == "" {
			stage = "concept_graph_coverage"
		}
		deps.Log.Info(stage+": adaptive params", "adaptive", adaptiveStageMeta(stage, adaptiveEnabled, signals, result.AdaptiveParams))
	}
	return result
}

func conceptsJSONForDelta(concepts []conceptInvItem) string {
	type row struct {
		Key       string `json:"key"`
		Name      string `json:"name"`
		ParentKey string `json:"parent_key,omitempty"`
		Summary   string `json:"summary,omitempty"`
	}
	arr := make([]row, 0, len(concepts))
	for _, c := range concepts {
		if strings.TrimSpace(c.Key) == "" || strings.TrimSpace(c.Name) == "" {
			continue
		}
		arr = append(arr, row{
			Key:       strings.TrimSpace(c.Key),
			Name:      strings.TrimSpace(c.Name),
			ParentKey: strings.TrimSpace(c.ParentKey),
			Summary:   shorten(strings.TrimSpace(c.Summary), 260),
		})
	}
	sort.Slice(arr, func(i, j int) bool { return arr[i].Key < arr[j].Key })
	b, err := json.Marshal(map[string]any{"concepts": arr})
	if err != nil {
		return `{"concepts":[]}`
	}
	return string(b)
}

func parseConceptInventoryDelta(obj map[string]any) ([]conceptInvItem, conceptCoverage, error) {
	cov := parseConceptCoverage(obj)
	raw, ok := obj["new_concepts"]
	if !ok || raw == nil {
		return nil, cov, fmt.Errorf("concept_inventory_delta: missing new_concepts")
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, cov, fmt.Errorf("concept_inventory_delta: new_concepts not array")
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
	return out, cov, nil
}

func splitStringBatches(in []string, size int) [][]string {
	if len(in) == 0 {
		return nil
	}
	if size <= 0 {
		return [][]string{in}
	}
	out := make([][]string, 0, (len(in)+size-1)/size)
	for start := 0; start < len(in); start += size {
		end := start + size
		if end > len(in) {
			end = len(in)
		}
		out = append(out, in[start:end])
	}
	return out
}

func renderChunkExcerptsByIDsOrdered(chunkByID map[uuid.UUID]*types.MaterialChunk, ids []uuid.UUID, maxChars int, maxTotalChars int) (string, []uuid.UUID) {
	if maxChars <= 0 {
		maxChars = 700
	}
	var (
		b    strings.Builder
		out  []uuid.UUID
		seen = map[uuid.UUID]bool{}
	)
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		ch := chunkByID[id]
		if ch == nil {
			continue
		}
		if isUnextractableChunk(ch) {
			continue
		}
		txt := shorten(strings.TrimSpace(ch.Text), maxChars)
		if txt == "" {
			continue
		}
		line := fmt.Sprintf("[chunk_id=%s] %s\n", id.String(), txt)
		if maxTotalChars > 0 && b.Len()+len(line) > maxTotalChars {
			break
		}
		b.WriteString(line)
		out = append(out, id)
	}
	return strings.TrimSpace(b.String()), out
}

func coverageTargetChunkIDs(
	ctx context.Context,
	deps ConceptGraphBuildDeps,
	materialSetID uuid.UUID,
	allowFiles map[uuid.UUID]bool,
	missingTopics []string,
	seenChunkIDs map[uuid.UUID]bool,
	chunkEmbs []chunkEmbedding,
	maxTopics int,
	topK int,
	topicEmbedCache map[string][]float32,
) []uuid.UUID {
	if deps.AI == nil || materialSetID == uuid.Nil || maxTopics <= 0 || topK <= 0 {
		return nil
	}
	topics := dedupeStrings(missingTopics)
	if len(topics) == 0 {
		return nil
	}
	if len(topics) > maxTopics {
		topics = topics[:maxTopics]
	}

	if topicEmbedCache == nil {
		topicEmbedCache = map[string][]float32{}
	}
	embs := make([][]float32, len(topics))
	missing := make([]string, 0)
	missingIdx := make([]int, 0)
	for i, t := range topics {
		key := strings.TrimSpace(t)
		if key == "" {
			continue
		}
		if v := topicEmbedCache[key]; len(v) > 0 {
			embs[i] = v
			continue
		}
		missing = append(missing, key)
		missingIdx = append(missingIdx, i)
	}
	if len(missing) > 0 {
		timer := llmTimer(deps.Log, "topic_embeddings", map[string]any{
			"stage":        "concept_graph_build",
			"material_set": materialSetID.String(),
			"topic_count":  len(missing),
		})
		newEmbs, err := deps.AI.Embed(ctx, missing)
		timer(err)
		if err != nil || len(newEmbs) != len(missing) {
			return nil
		}
		for i, emb := range newEmbs {
			idx := missingIdx[i]
			embs[idx] = emb
			if len(emb) > 0 {
				topicEmbedCache[missing[i]] = emb
			}
		}
	}
	for _, emb := range embs {
		if len(emb) == 0 {
			return nil
		}
	}

	out := make([]uuid.UUID, 0)
	seenOut := map[uuid.UUID]bool{}

	// Prefer Pinecone for semantic chunk recall when available.
	if deps.Vec != nil {
		// Derived material sets share the chunk namespace with their source upload batch.
		sourceSetID := materialSetID
		if deps.DB != nil {
			if sc, err := materialsetctx.Resolve(ctx, deps.DB, materialSetID); err == nil && sc.SourceMaterialSetID != uuid.Nil {
				sourceSetID = sc.SourceMaterialSetID
			}
		}
		ns := index.ChunksNamespace(sourceSetID)
		filter := pineconeChunkFilterWithAllowlist(allowFiles)
		for i := range embs {
			if len(embs[i]) == 0 {
				continue
			}
			qctx, cancel := context.WithTimeout(ctx, 4*time.Second)
			ids, qerr := deps.Vec.QueryIDs(qctx, ns, embs[i], topK, filter)
			cancel()
			if qerr != nil {
				continue
			}
			for _, s := range ids {
				id, err := uuid.Parse(strings.TrimSpace(s))
				if err != nil || id == uuid.Nil || seenChunkIDs[id] || seenOut[id] {
					continue
				}
				seenOut[id] = true
				out = append(out, id)
			}
		}
	}

	// Local embedding fallback.
	if len(out) == 0 && len(chunkEmbs) > 0 {
		for i := range embs {
			if len(embs[i]) == 0 {
				continue
			}
			ids := topKChunkIDsByCosine(embs[i], chunkEmbs, topK)
			for _, id := range ids {
				if id == uuid.Nil || seenChunkIDs[id] || seenOut[id] {
					continue
				}
				seenOut[id] = true
				out = append(out, id)
			}
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

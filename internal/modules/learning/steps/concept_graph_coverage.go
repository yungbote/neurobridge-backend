package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/data/materialsetctx"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/prompts"
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
}

func conceptGraphCoveragePasses() int {
	if v := envIntAllowZero("CONCEPT_GRAPH_COVERAGE_PASSES", -1); v >= 0 {
		return v
	}
	switch qualityMode() {
	case "premium", "openai", "high":
		return 3
	default:
		return 1
	}
}

func completeConceptCoverage(ctx context.Context, deps ConceptGraphBuildDeps, in conceptCoverageInput) []conceptInvItem {
	if ctx == nil {
		ctx = context.Background()
	}
	if deps.AI == nil || len(in.Concepts) == 0 || len(in.Chunks) == 0 {
		return in.Concepts
	}

	passes := conceptGraphCoveragePasses()
	if passes <= 0 {
		return in.Concepts
	}

	maxConcepts := envIntAllowZero("CONCEPT_GRAPH_MAX_CONCEPTS", 180)
	if maxConcepts <= 0 {
		maxConcepts = 180
	}

	extraPerFile := envIntAllowZero("CONCEPT_GRAPH_COVERAGE_EXCERPTS_PER_FILE", 6)
	extraMaxChars := envIntAllowZero("CONCEPT_GRAPH_COVERAGE_EXCERPT_MAX_CHARS", 700)
	extraMaxLines := envIntAllowZero("CONCEPT_GRAPH_COVERAGE_EXCERPT_MAX_LINES", 0)
	extraMaxTotal := envIntAllowZero("CONCEPT_GRAPH_COVERAGE_EXCERPT_MAX_TOTAL_CHARS", 45000)
	if extraMaxTotal <= 0 {
		extraMaxTotal = 45000
	}

	maxTopics := envIntAllowZero("CONCEPT_GRAPH_COVERAGE_MAX_MISSING_TOPICS", 8)
	topicTopK := envIntAllowZero("CONCEPT_GRAPH_COVERAGE_TOPIC_TOPK", 6)

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

	for pass := 1; pass <= passes; pass++ {
		if len(knownKeys) >= maxConcepts {
			return concepts
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

		targetIDs := coverageTargetChunkIDs(ctx, deps, in.MaterialSetID, in.MaterialFileFilter, missingTopics, seenChunkIDs, in.ChunkEmbs, maxTopics, topicTopK)
		deltaExcerpts, stratIDs := stratifiedChunkExcerptsWithLimitsAndIDs(remaining, extraPerFile, extraMaxChars, extraMaxLines, extraMaxTotal)
		usedIDs := stratIDs
		if len(targetIDs) > 0 {
			candidates := append(targetIDs, stratIDs...)
			deltaExcerpts, usedIDs = renderChunkExcerptsByIDsOrdered(in.ChunkByID, candidates, extraMaxChars, extraMaxTotal)
		}
		if strings.TrimSpace(deltaExcerpts) == "" {
			break
		}

		// Mark chunks as seen so each pass samples new evidence.
		for _, id := range usedIDs {
			if id != uuid.Nil {
				seenChunkIDs[id] = true
			}
		}

		conceptsJSON := conceptsJSONForDelta(concepts)
		p, err := prompts.Build(prompts.PromptConceptInventoryDelta, prompts.Input{
			PathIntentMD: in.IntentMD,
			ConceptsJSON: conceptsJSON,
			Excerpts:     deltaExcerpts,
		})
		if err != nil {
			if deps.Log != nil {
				deps.Log.Warn("concept_graph_build: coverage delta prompt build failed (continuing)", "error", err, "path_id", in.PathID.String())
			}
			break
		}

		obj, err := deps.AI.GenerateJSON(ctx, p.System, p.User, p.SchemaName, p.Schema)
		if err != nil && isContextLengthExceeded(err) && extraMaxTotal > 12000 {
			extraMaxTotal = maxInt(12000, extraMaxTotal/2)
			deltaExcerpts, _ = renderChunkExcerptsByIDsOrdered(in.ChunkByID, append(targetIDs, stratIDs...), extraMaxChars, extraMaxTotal)
			if strings.TrimSpace(deltaExcerpts) != "" {
				p2, berr := prompts.Build(prompts.PromptConceptInventoryDelta, prompts.Input{
					PathIntentMD: in.IntentMD,
					ConceptsJSON: conceptsJSON,
					Excerpts:     deltaExcerpts,
				})
				if berr == nil {
					obj, err = deps.AI.GenerateJSON(ctx, p2.System, p2.User, p2.SchemaName, p2.Schema)
				}
			}
		}
		if err != nil {
			if deps.Log != nil {
				deps.Log.Warn("concept_graph_build: coverage delta generation failed (continuing)", "error", err, "path_id", in.PathID.String())
			}
			break
		}

		newConcepts, cov, perr := parseConceptInventoryDelta(obj)
		if perr != nil {
			if deps.Log != nil {
				deps.Log.Warn("concept_graph_build: coverage delta parse failed (continuing)", "error", perr, "path_id", in.PathID.String())
			}
			break
		}
		if len(cov.MissingTopics) > 0 {
			missingTopics = cov.MissingTopics
		}
		if len(newConcepts) == 0 {
			break
		}

		merged, _ := normalizeConceptInventory(append(concepts, newConcepts...), in.AllowedChunkIDs)
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
			deps.Log.Info("concept_graph_build: coverage pass added concepts", "path_id", in.PathID.String(), "pass", pass, "added", added, "total", len(knownKeys))
		}
	}

	return concepts
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

	embs, err := deps.AI.Embed(ctx, topics)
	if err != nil || len(embs) != len(topics) {
		return nil
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

package steps

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/prompts"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type styleManifest struct {
	SchemaVersion    int      `json:"schema_version"`
	Tone             string   `json:"tone"`
	Register         string   `json:"register"`
	Verbosity        string   `json:"verbosity"`
	MetaphorsAllowed bool     `json:"metaphors_allowed"`
	PreferredPhrases []string `json:"preferred_phrases"`
	BannedPhrases    []string `json:"banned_phrases"`
	DoList           []string `json:"do_list"`
	DontList         []string `json:"dont_list"`
	SentenceLength   string   `json:"sentence_length"`
	VoiceNotes       string   `json:"voice_notes"`
}

type pathNarrativePlan struct {
	SchemaVersion         int      `json:"schema_version"`
	ArcSummary            string   `json:"arc_summary"`
	ContinuityRules       []string `json:"continuity_rules"`
	RecurringTerms        []string `json:"recurring_terms"`
	PreferredTransitions  []string `json:"preferred_transitions"`
	ForbiddenPhrases      []string `json:"forbidden_phrases"`
	BackReferenceRules    []string `json:"back_reference_rules"`
	ForwardReferenceRules []string `json:"forward_reference_rules"`
	ToneNotes             string   `json:"tone_notes"`
}

type nodeNarrativePlan struct {
	SchemaVersion  int      `json:"schema_version"`
	OpeningIntent  string   `json:"opening_intent"`
	ClosingIntent  string   `json:"closing_intent"`
	BackReferences []string `json:"back_references"`
	ForwardLink    string   `json:"forward_link"`
	AnchorTerms    []string `json:"anchor_terms"`
	AvoidPhrases   []string `json:"avoid_phrases"`
}

type mediaRankPlan struct {
	SchemaVersion int                  `json:"schema_version"`
	Selections    []mediaRankSelection `json:"selections"`
}

type mediaRankSelection struct {
	SectionHeading string   `json:"section_heading"`
	Purpose        string   `json:"purpose"`
	AssetURL       string   `json:"asset_url"`
	AssetKind      string   `json:"asset_kind"`
	Rationale      string   `json:"rationale"`
	ChunkIDs       []string `json:"chunk_ids"`
}

func metaJSONString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func parseMetaJSON(raw datatypes.JSON) map[string]any {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || string(raw) == "null" {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func hashJSON(v any) string {
	b, err := content.CanonicalizeJSON(v)
	if err != nil || len(b) == 0 {
		raw, _ := json.Marshal(v)
		return content.HashBytes(raw)
	}
	return content.HashBytes(b)
}

func pathStructureSummary(nodes []*types.PathNode) []map[string]any {
	childCount := map[uuid.UUID]int{}
	for _, n := range nodes {
		if n == nil || n.ID == uuid.Nil {
			continue
		}
		if n.ParentNodeID != nil && *n.ParentNodeID != uuid.Nil {
			childCount[*n.ParentNodeID]++
		}
	}
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		if n == nil || n.ID == uuid.Nil {
			continue
		}
		meta := parseMetaJSON(n.Metadata)
		kind := strings.TrimSpace(stringFromAny(meta["node_kind"]))
		if kind == "" {
			if childCount[n.ID] > 0 {
				kind = "module"
			} else {
				kind = "lesson"
			}
		}
		out = append(out, map[string]any{
			"index":           n.Index,
			"title":           strings.TrimSpace(n.Title),
			"goal":            strings.TrimSpace(stringFromAny(meta["goal"])),
			"kind":            kind,
			"concept_keys":    dedupeStrings(stringSliceFromAny(meta["concept_keys"])),
			"prereq_concepts": dedupeStrings(stringSliceFromAny(meta["prereq_concept_keys"])),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return intFromAny(out[i]["index"], 0) < intFromAny(out[j]["index"], 0)
	})
	return out
}

func ensureStyleManifest(ctx context.Context, deps NodeDocBuildDeps, pathID uuid.UUID, pathMeta map[string]any, pathIntentMD, pathStyleJSON, patternHierarchyJSON, pathStructureJSON string) (string, map[string]any) {
	if deps.AI == nil || !envBool("STYLE_MANIFEST_ENABLED", true) {
		return "", nil
	}
	inputHash := hashJSON(map[string]any{
		"intent":    strings.TrimSpace(pathIntentMD),
		"style":     strings.TrimSpace(pathStyleJSON),
		"patterns":  strings.TrimSpace(patternHierarchyJSON),
		"structure": strings.TrimSpace(pathStructureJSON),
	})

	if pathMeta != nil {
		if h := strings.TrimSpace(stringFromAny(pathMeta["style_manifest_hash"])); h != "" && h == inputHash {
			if v := pathMeta["style_manifest"]; v != nil {
				if js := metaJSONString(v); js != "" {
					return js, nil
				}
			}
		}
	}

	prompt, err := prompts.Build(prompts.PromptStyleManifest, prompts.Input{
		PathIntentMD:         pathIntentMD,
		PathStyleJSON:        pathStyleJSON,
		PatternHierarchyJSON: patternHierarchyJSON,
		PathStructureJSON:    pathStructureJSON,
	})
	if err != nil {
		return "", nil
	}
	obj, err := deps.AI.GenerateJSON(ctx, prompt.System, prompt.User, prompt.SchemaName, prompt.Schema)
	if err != nil {
		return "", nil
	}
	raw, _ := json.Marshal(obj)
	if len(raw) == 0 {
		return "", nil
	}
	update := map[string]any{
		"style_manifest":      obj,
		"style_manifest_hash": inputHash,
	}
	return string(raw), update
}

func ensurePathNarrativePlan(ctx context.Context, deps NodeDocBuildDeps, pathID uuid.UUID, pathMeta map[string]any, pathIntentMD, patternHierarchyJSON, pathStructureJSON, styleManifestJSON string) (string, map[string]any) {
	if deps.AI == nil || !envBool("PATH_NARRATIVE_ENABLED", true) {
		return "", nil
	}
	inputHash := hashJSON(map[string]any{
		"intent":    strings.TrimSpace(pathIntentMD),
		"patterns":  strings.TrimSpace(patternHierarchyJSON),
		"structure": strings.TrimSpace(pathStructureJSON),
		"style":     strings.TrimSpace(styleManifestJSON),
	})

	if pathMeta != nil {
		if h := strings.TrimSpace(stringFromAny(pathMeta["path_narrative_hash"])); h != "" && h == inputHash {
			if v := pathMeta["path_narrative_plan"]; v != nil {
				if js := metaJSONString(v); js != "" {
					return js, nil
				}
			}
		}
	}

	prompt, err := prompts.Build(prompts.PromptPathNarrativePlan, prompts.Input{
		PathIntentMD:         pathIntentMD,
		PathStructureJSON:    pathStructureJSON,
		PatternHierarchyJSON: patternHierarchyJSON,
		StyleManifestJSON:    styleManifestJSON,
	})
	if err != nil {
		return "", nil
	}
	obj, err := deps.AI.GenerateJSON(ctx, prompt.System, prompt.User, prompt.SchemaName, prompt.Schema)
	if err != nil {
		return "", nil
	}
	raw, _ := json.Marshal(obj)
	if len(raw) == 0 {
		return "", nil
	}
	update := map[string]any{
		"path_narrative_plan": obj,
		"path_narrative_hash": inputHash,
	}
	return string(raw), update
}

func ensureNodeNarrativePlan(
	ctx context.Context,
	deps NodeDocBuildDeps,
	node *types.PathNode,
	nodeMeta map[string]any,
	pathNarrativeJSON string,
	styleManifestJSON string,
	outlineJSON string,
	conceptCSV string,
	prevTitle string,
	nextTitle string,
	moduleTitle string,
) (string, map[string]any) {
	if deps.AI == nil || node == nil || node.ID == uuid.Nil || !envBool("NODE_NARRATIVE_ENABLED", true) {
		return "", nil
	}
	inputHash := hashJSON(map[string]any{
		"title":     strings.TrimSpace(node.Title),
		"goal":      strings.TrimSpace(stringFromAny(nodeMeta["goal"])),
		"concepts":  strings.TrimSpace(conceptCSV),
		"prev":      strings.TrimSpace(prevTitle),
		"next":      strings.TrimSpace(nextTitle),
		"module":    strings.TrimSpace(moduleTitle),
		"outline":   strings.TrimSpace(outlineJSON),
		"path_plan": strings.TrimSpace(pathNarrativeJSON),
		"style":     strings.TrimSpace(styleManifestJSON),
	})

	if nodeMeta != nil {
		if h := strings.TrimSpace(stringFromAny(nodeMeta["node_narrative_hash"])); h != "" && h == inputHash {
			if v := nodeMeta["node_narrative_plan"]; v != nil {
				if js := metaJSONString(v); js != "" {
					return js, nil
				}
			}
		}
	}

	prompt, err := prompts.Build(prompts.PromptNodeNarrativePlan, prompts.Input{
		NodeTitle:         strings.TrimSpace(node.Title),
		NodeGoal:          strings.TrimSpace(stringFromAny(nodeMeta["goal"])),
		ConceptKeysCSV:    strings.TrimSpace(conceptCSV),
		OutlineHintJSON:   outlineJSON,
		PathNarrativeJSON: pathNarrativeJSON,
		StyleManifestJSON: styleManifestJSON,
	})
	if err != nil {
		return "", nil
	}
	obj, err := deps.AI.GenerateJSON(ctx, prompt.System, prompt.User, prompt.SchemaName, prompt.Schema)
	if err != nil {
		return "", nil
	}
	raw, _ := json.Marshal(obj)
	if len(raw) == 0 {
		return "", nil
	}
	update := map[string]any{
		"node_narrative_plan":       obj,
		"node_narrative_hash":       inputHash,
		"node_narrative_updated_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	return string(raw), update
}

func ensureMediaRank(
	ctx context.Context,
	deps NodeDocBuildDeps,
	node *types.PathNode,
	nodeMeta map[string]any,
	outlineJSON string,
	assetsJSON string,
	nodeNarrativeJSON string,
	styleManifestJSON string,
) (string, map[string]any) {
	if deps.AI == nil || node == nil || node.ID == uuid.Nil || !envBool("NODE_MEDIA_RANK_ENABLED", true) {
		return "", nil
	}
	if strings.TrimSpace(assetsJSON) == "" || strings.TrimSpace(outlineJSON) == "" {
		return "", nil
	}
	inputHash := hashJSON(map[string]any{
		"outline":   strings.TrimSpace(outlineJSON),
		"assets":    strings.TrimSpace(assetsJSON),
		"narrative": strings.TrimSpace(nodeNarrativeJSON),
		"style":     strings.TrimSpace(styleManifestJSON),
	})
	if nodeMeta != nil {
		if h := strings.TrimSpace(stringFromAny(nodeMeta["media_rank_hash"])); h != "" && h == inputHash {
			if v := nodeMeta["media_rank_plan"]; v != nil {
				if js := metaJSONString(v); js != "" {
					return js, nil
				}
			}
		}
	}
	prompt, err := prompts.Build(prompts.PromptMediaRank, prompts.Input{
		OutlineHintJSON:   outlineJSON,
		AssetsJSON:        assetsJSON,
		NodeNarrativeJSON: nodeNarrativeJSON,
		StyleManifestJSON: styleManifestJSON,
	})
	if err != nil {
		return "", nil
	}
	obj, err := deps.AI.GenerateJSON(ctx, prompt.System, prompt.User, prompt.SchemaName, prompt.Schema)
	if err != nil {
		return "", nil
	}
	raw, _ := json.Marshal(obj)
	if len(raw) == 0 {
		return "", nil
	}
	update := map[string]any{
		"media_rank_plan":       obj,
		"media_rank_hash":       inputHash,
		"media_rank_updated_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	return string(raw), update
}

func updatePathMeta(ctx context.Context, deps NodeDocBuildDeps, pathID uuid.UUID, base map[string]any, updates map[string]any) {
	if deps.Path == nil || deps.DB == nil || len(updates) == 0 {
		return
	}
	if base == nil {
		base = map[string]any{}
	}
	for k, v := range updates {
		base[k] = v
	}
	base["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	_ = deps.Path.UpdateFields(dbctx.Context{Ctx: ctx}, pathID, map[string]interface{}{
		"metadata": datatypes.JSON(mustJSON(base)),
	})
}

func updateNodeMeta(ctx context.Context, deps NodeDocBuildDeps, nodeID uuid.UUID, base map[string]any, updates map[string]any) {
	if deps.PathNodes == nil || deps.DB == nil || nodeID == uuid.Nil || len(updates) == 0 {
		return
	}
	if base == nil {
		base = map[string]any{}
	}
	for k, v := range updates {
		base[k] = v
	}
	base["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	_ = deps.PathNodes.UpdateFields(dbctx.Context{Ctx: ctx}, nodeID, map[string]interface{}{
		"metadata": datatypes.JSON(mustJSON(base)),
	})
}

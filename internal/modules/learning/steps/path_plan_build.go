package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/prompts"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"golang.org/x/sync/errgroup"
)

type PathPlanBuildDeps struct {
	DB           *gorm.DB
	Log          *logger.Logger
	Path         repos.PathRepo
	PathNodes    repos.PathNodeRepo
	Concepts     repos.ConceptRepo
	Edges        repos.ConceptEdgeRepo
	Summaries    repos.MaterialSetSummaryRepo
	UserProfile  repos.UserProfileVectorRepo
	ConceptState repos.UserConceptStateRepo
	Graph        *neo4jdb.Client
	AI           openai.Client
	Bootstrap    services.LearningBuildBootstrapService
}

type PathPlanBuildInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
}

type PathPlanBuildOutput struct {
	PathID uuid.UUID `json:"path_id"`
	Nodes  int       `json:"nodes"`
}

func PathPlanBuild(ctx context.Context, deps PathPlanBuildDeps, in PathPlanBuildInput) (PathPlanBuildOutput, error) {
	out := PathPlanBuildOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Path == nil || deps.PathNodes == nil || deps.Concepts == nil || deps.Edges == nil || deps.Summaries == nil || deps.UserProfile == nil || deps.AI == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("path_plan_build: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("path_plan_build: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("path_plan_build: missing material_set_id")
	}

	pathID, err := resolvePathID(ctx, deps.Bootstrap, in.OwnerUserID, in.MaterialSetID, in.PathID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	// Idempotency: if nodes already exist, don't rebuild structure (preserve stable IDs/ranks).
	existingNodes, err := deps.PathNodes.GetByPathIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{pathID})
	if err != nil {
		return out, err
	}
	if len(existingNodes) > 0 {
		if deps.Graph != nil {
			if err := syncPathStructureToNeo4j(ctx, deps, pathID); err != nil {
				deps.Log.Warn("neo4j path structure sync failed (continuing)", "error", err, "path_id", pathID.String())
			}
		}
		out.Nodes = len(existingNodes)
		return out, nil
	}

	var (
		up          *types.UserProfileVector
		summaryText string
		concepts    []*types.Concept
		pathRow     *types.Path
	)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var err error
		up, err = deps.UserProfile.GetByUserID(dbctx.Context{Ctx: gctx}, in.OwnerUserID)
		return err
	})
	g.Go(func() error {
		if rows, err := deps.Summaries.GetByMaterialSetIDs(dbctx.Context{Ctx: gctx}, []uuid.UUID{in.MaterialSetID}); err == nil && len(rows) > 0 && rows[0] != nil {
			summaryText = strings.TrimSpace(rows[0].SummaryMD)
		}
		return nil
	})
	g.Go(func() error {
		var err error
		concepts, err = deps.Concepts.GetByScope(dbctx.Context{Ctx: gctx}, "path", &pathID)
		return err
	})
	g.Go(func() error {
		if deps.Path == nil {
			return nil
		}
		row, err := deps.Path.GetByID(dbctx.Context{Ctx: gctx}, pathID)
		if err != nil {
			if deps.Log != nil {
				deps.Log.Warn("path_plan_build: failed to load path metadata (continuing)", "error", err, "path_id", pathID.String())
			}
			return nil
		}
		pathRow = row
		return nil
	})
	if err := g.Wait(); err != nil {
		return out, err
	}
	if up == nil || strings.TrimSpace(up.ProfileDoc) == "" {
		return out, fmt.Errorf("path_plan_build: missing user_profile_doc (run user_profile_refresh first)")
	}

	curriculumSpecJSON := ""
	materialPathsJSON := ""
	// Optionally prepend user intent/intake context (written by the path_intake stage).
	if pathRow != nil && len(pathRow.Metadata) > 0 && string(pathRow.Metadata) != "null" {
		var meta map[string]any
		if uerr := json.Unmarshal(pathRow.Metadata, &meta); uerr == nil && meta != nil {
			intakeMD := strings.TrimSpace(stringFromAny(meta["intake_md"]))
			if intakeMD != "" {
				if summaryText != "" {
					summaryText = strings.TrimSpace(intakeMD + "\n\n" + summaryText)
				} else {
					summaryText = intakeMD
				}
			}
			curriculumSpecJSON = CurriculumSpecBriefJSONFromPathMeta(meta, 6)
			materialPathsJSON = IntakePathsBriefJSONFromPathMeta(meta, 4)
		}
	}

	if len(concepts) == 0 {
		return out, fmt.Errorf("path_plan_build: no concepts for path (run concept_graph_build first)")
	}

	edges, err := deps.Edges.GetByConceptIDs(dbctx.Context{Ctx: ctx}, conceptIDs(concepts))
	if err != nil {
		return out, err
	}

	// ---- User knowledge context (cross-path mastery transfer) ----
	userKnowledgeJSON := ""
	canonicalIDByKey := map[string]uuid.UUID{}
	allConceptKeys := make([]string, 0, len(concepts))
	canonicalIDs := make([]uuid.UUID, 0, len(concepts))
	for _, c := range concepts {
		if c == nil || c.ID == uuid.Nil {
			continue
		}
		k := strings.TrimSpace(strings.ToLower(c.Key))
		if k == "" {
			continue
		}
		id := c.ID
		if c.CanonicalConceptID != nil && *c.CanonicalConceptID != uuid.Nil {
			id = *c.CanonicalConceptID
		}
		if id == uuid.Nil {
			continue
		}
		canonicalIDByKey[k] = id
		allConceptKeys = append(allConceptKeys, c.Key)
		canonicalIDs = append(canonicalIDs, id)
	}
	canonicalIDs = dedupeUUIDs(canonicalIDs)
	stateByConceptID := map[uuid.UUID]*types.UserConceptState{}
	if deps.ConceptState != nil && len(canonicalIDs) > 0 {
		if rows, err := deps.ConceptState.ListByUserAndConceptIDs(dbctx.Context{Ctx: ctx}, in.OwnerUserID, canonicalIDs); err == nil {
			for _, st := range rows {
				if st == nil || st.ConceptID == uuid.Nil {
					continue
				}
				stateByConceptID[st.ConceptID] = st
			}
		}
	}
	if len(allConceptKeys) > 0 {
		userKnowledgeJSON = BuildUserKnowledgeContextV1(allConceptKeys, canonicalIDByKey, stateByConceptID, time.Now().UTC()).JSON()
	}
	if strings.TrimSpace(userKnowledgeJSON) == "" {
		userKnowledgeJSON = "(none)"
	}

	// ConceptsJSON + EdgesJSON for prompt input.
	type cjson struct {
		Key     string `json:"key"`
		Name    string `json:"name"`
		Summary string `json:"summary"`
	}
	carr := make([]cjson, 0, len(concepts))
	idToKey := map[uuid.UUID]string{}
	for _, c := range concepts {
		if c == nil {
			continue
		}
		idToKey[c.ID] = c.Key
		carr = append(carr, cjson{Key: c.Key, Name: c.Name, Summary: c.Summary})
	}
	sort.Slice(carr, func(i, j int) bool { return carr[i].Key < carr[j].Key })
	conceptsJSON, _ := json.Marshal(map[string]any{"concepts": carr})

	type ejson struct {
		FromKey  string  `json:"from_key"`
		ToKey    string  `json:"to_key"`
		EdgeType string  `json:"edge_type"`
		Strength float64 `json:"strength"`
	}
	earr := make([]ejson, 0, len(edges))
	for _, e := range edges {
		if e == nil {
			continue
		}
		earr = append(earr, ejson{
			FromKey:  idToKey[e.FromConceptID],
			ToKey:    idToKey[e.ToConceptID],
			EdgeType: e.EdgeType,
			Strength: e.Strength,
		})
	}
	edgesJSON, _ := json.Marshal(map[string]any{"edges": earr})

	signalCtx := loadMaterialSetSignalContext(ctx, deps.DB, in.MaterialSetID, 30)

	// ---- Prompt: Path charter ----
	charterPrompt, err := prompts.Build(prompts.PromptPathCharter, prompts.Input{
		UserProfileDoc:          up.ProfileDoc,
		BundleExcerpt:           summaryText,
		UserKnowledgeJSON:       userKnowledgeJSON,
		MaterialSetIntentJSON:   signalCtx.IntentJSON,
		MaterialSetCoverageJSON: signalCtx.CoverageJSON,
		MaterialSetEdgesJSON:    signalCtx.EdgesJSON,
	})
	if err != nil {
		return out, err
	}
	charterObj, err := deps.AI.GenerateJSON(ctx, charterPrompt.System, charterPrompt.User, charterPrompt.SchemaName, charterPrompt.Schema)
	if err != nil {
		return out, err
	}
	charterJSON, _ := json.Marshal(charterObj)

	// ---- Prompt: Path structure ----
	structPrompt, err := prompts.Build(prompts.PromptPathStructure, prompts.Input{
		PathCharterJSON:         string(charterJSON),
		BundleExcerpt:           summaryText,
		CurriculumSpecJSON:      curriculumSpecJSON,
		MaterialPathsJSON:       materialPathsJSON,
		ConceptsJSON:            string(conceptsJSON),
		EdgesJSON:               string(edgesJSON),
		UserKnowledgeJSON:       userKnowledgeJSON,
		MaterialSetIntentJSON:   signalCtx.IntentJSON,
		MaterialSetCoverageJSON: signalCtx.CoverageJSON,
		MaterialSetEdgesJSON:    signalCtx.EdgesJSON,
	})
	if err != nil {
		return out, err
	}
	structObj, err := deps.AI.GenerateJSON(ctx, structPrompt.System, structPrompt.User, structPrompt.SchemaName, structPrompt.Schema)
	if err != nil {
		return out, err
	}
	structDraft := structObj
	didRefine := false

	// Optional refinement pass for higher-quality structure.
	if shouldRefinePathStructure(structObj) {
		if refined, ok := refinePathStructure(ctx, deps, string(charterJSON), string(conceptsJSON), string(edgesJSON), userKnowledgeJSON, structDraft); ok {
			structObj = refined
			didRefine = true
		}
	}

	title := strings.TrimSpace(stringFromAny(structObj["title"]))
	desc := strings.TrimSpace(stringFromAny(structObj["description"]))
	nodesOut := parsePathStructureNodes(structObj)
	if len(nodesOut) == 0 {
		return out, fmt.Errorf("path_plan_build: 0 nodes returned")
	}
	if len(signalCtx.WeightsByKey) > 0 {
		for i := range nodesOut {
			nodesOut[i].ConceptKeys = sortConceptKeysByWeight(nodesOut[i].ConceptKeys, signalCtx.WeightsByKey)
			nodesOut[i].PrereqConceptKeys = sortConceptKeysByWeight(nodesOut[i].PrereqConceptKeys, signalCtx.WeightsByKey)
		}
	}

	// ---- Teaching pattern hierarchy (path/module/lesson) ----
	var (
		patternHierarchy    teachingPatternHierarchy
		patternHierarchyRaw map[string]any
		patternSignalsJSON  string
	)
	if deps.AI != nil {
		structJSON, _ := json.Marshal(structObj)
		signals := patternSignalsForPath(nodesOut, edges)
		if b, err := json.Marshal(signals); err == nil && len(b) > 0 {
			patternSignalsJSON = string(b)
		}
		patternPrompt, perr := prompts.Build(prompts.PromptTeachingPatternHierarchy, prompts.Input{
			UserProfileDoc:          up.ProfileDoc,
			BundleExcerpt:           summaryText,
			PathCharterJSON:         string(charterJSON),
			PathStructureJSON:       string(structJSON),
			ConceptsJSON:            string(conceptsJSON),
			EdgesJSON:               string(edgesJSON),
			UserKnowledgeJSON:       userKnowledgeJSON,
			PatternSignalsJSON:      patternSignalsJSON,
			MaterialSetIntentJSON:   signalCtx.IntentJSON,
			MaterialSetCoverageJSON: signalCtx.CoverageJSON,
			MaterialSetEdgesJSON:    signalCtx.EdgesJSON,
		})
		if perr == nil {
			obj, gerr := deps.AI.GenerateJSON(ctx, patternPrompt.System, patternPrompt.User, patternPrompt.SchemaName, patternPrompt.Schema)
			if gerr == nil {
				patternHierarchyRaw = obj
				patternHierarchy = parseTeachingPatternHierarchy(obj)
			} else if deps.Log != nil {
				deps.Log.Warn("path_plan_build: pattern hierarchy generation failed (continuing)", "error", gerr)
			}
		} else if deps.Log != nil {
			deps.Log.Warn("path_plan_build: pattern hierarchy prompt build failed (continuing)", "error", perr)
		}
	}
	patternHierarchy = normalizeTeachingPatternHierarchy(patternHierarchy, nodesOut, edges, up.ProfileDoc)
	modulePatternsByIndex := map[int]teachingModulePattern{}
	lessonPatternsByIndex := map[int]teachingLessonPattern{}
	for _, m := range patternHierarchy.Modules {
		modulePatternsByIndex[m.ModuleIndex] = m
	}
	for _, l := range patternHierarchy.Lessons {
		lessonPatternsByIndex[l.LessonIndex] = l
	}
	moduleIndices := map[int]bool{}
	parentByIndex := map[int]*int{}
	for _, n := range nodesOut {
		if strings.EqualFold(strings.TrimSpace(n.NodeKind), "module") {
			moduleIndices[n.Index] = true
		}
		parentByIndex[n.Index] = n.ParentIndex
	}
	lessonOrder := lessonOrderIndices(nodesOut)
	firstLesson := 0
	lastLesson := 0
	if len(lessonOrder) > 0 {
		firstLesson = lessonOrder[0]
		lastLesson = lessonOrder[len(lessonOrder)-1]
	}
	moduleLessonOrder := lessonsByModule(nodesOut, moduleIndices)

	now := time.Now().UTC()

	nodesInserted := 0
	if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		if in.PathID == uuid.Nil {
			if _, err := deps.Bootstrap.EnsurePath(dbc, in.OwnerUserID, in.MaterialSetID); err != nil {
				return err
			}
		}

		// Update path title/description + metadata.
		pRow, err := deps.Path.GetByID(dbc, pathID)
		if err != nil {
			return err
		}
		meta := map[string]any{}
		if pRow != nil && len(pRow.Metadata) > 0 && string(pRow.Metadata) != "null" {
			_ = json.Unmarshal(pRow.Metadata, &meta)
		}
		meta["charter"] = charterObj
		// Save both the raw draft and refined output when refinement is enabled (debuggability).
		if didRefine && structDraft != nil {
			meta["structure_draft"] = structDraft
		}
		meta["structure"] = structObj
		if patternHierarchy.SchemaVersion > 0 {
			meta["pattern_hierarchy"] = patternHierarchy
			if len(patternSignalsJSON) > 0 {
				var sig any
				if json.Unmarshal([]byte(patternSignalsJSON), &sig) == nil {
					meta["pattern_signals"] = sig
				}
			}
			if patternHierarchyRaw != nil {
				meta["pattern_hierarchy_raw"] = patternHierarchyRaw
			}
		}
		meta["updated_at"] = now.Format(time.RFC3339Nano)
		if err := deps.Path.UpdateFields(dbc, pathID, map[string]interface{}{
			"title":       stringsOr(title, "Learning Path"),
			"description": desc,
			"metadata":    datatypes.JSON(mustJSON(meta)),
		}); err != nil {
			return err
		}

		idByIndex := map[int]uuid.UUID{}
		for _, n := range nodesOut {
			if n.Index <= 0 {
				continue
			}
			if _, exists := idByIndex[n.Index]; exists {
				continue
			}
			idByIndex[n.Index] = uuid.New()
		}

		nodeRows := make([]*types.PathNode, 0, len(nodesOut))
		for _, n := range nodesOut {
			if n.Index <= 0 {
				continue
			}
			orderedKeys := n.ConceptKeys
			orderedPrereq := n.PrereqConceptKeys
			if len(signalCtx.WeightsByKey) > 0 {
				orderedKeys = sortConceptKeysByWeight(orderedKeys, signalCtx.WeightsByKey)
				orderedPrereq = sortConceptKeysByWeight(orderedPrereq, signalCtx.WeightsByKey)
			}
			nodeMeta := map[string]any{
				"goal":                n.Goal,
				"concept_keys":        orderedKeys,
				"prereq_concept_keys": orderedPrereq,
				"difficulty":          n.Difficulty,
				"activity_slots":      n.ActivitySlots,
				"node_kind":           n.NodeKind,
				"doc_template":        n.DocTemplate,
				"parent_index":        n.ParentIndex,
			}
			if len(signalCtx.WeightsByKey) > 0 {
				if weights := conceptWeightsForKeys(orderedKeys, signalCtx.WeightsByKey); len(weights) > 0 {
					nodeMeta["concept_weights"] = weights
				}
			}
			if patternHierarchy.SchemaVersion > 0 {
				patternMeta := map[string]any{
					"path": patternHierarchy.Path,
				}
				if strings.EqualFold(strings.TrimSpace(n.NodeKind), "module") {
					if mod, ok := modulePatternsByIndex[n.Index]; ok {
						patternMeta["module"] = mod
						patternMeta["module_index"] = n.Index
					}
				} else {
					moduleIdx := parentModuleIndexForNode(n.Index, parentByIndex, moduleIndices)
					if mod, ok := modulePatternsByIndex[moduleIdx]; ok {
						patternMeta["module"] = mod
						patternMeta["module_index"] = moduleIdx
					}
					if lesson, ok := lessonPatternsByIndex[n.Index]; ok {
						patternMeta["lesson"] = lesson
						patternMeta["lesson_index"] = n.Index
					}
					pos := map[string]bool{
						"is_first_in_path":   n.Index == firstLesson,
						"is_last_in_path":    n.Index == lastLesson,
						"is_first_in_module": false,
						"is_last_in_module":  false,
					}
					if kids := moduleLessonOrder[moduleIdx]; len(kids) > 0 {
						pos["is_first_in_module"] = kids[0] == n.Index
						pos["is_last_in_module"] = kids[len(kids)-1] == n.Index
					}
					patternMeta["position"] = pos
				}
				nodeMeta["patterns"] = patternMeta
			}

			var parentNodeID *uuid.UUID
			if n.ParentIndex != nil && *n.ParentIndex > 0 {
				if pid, ok := idByIndex[*n.ParentIndex]; ok && pid != uuid.Nil {
					parentCopy := pid
					parentNodeID = &parentCopy
				}
			}

			row := &types.PathNode{
				ID:           idByIndex[n.Index],
				PathID:       pathID,
				Index:        n.Index,
				Title:        n.Title,
				ParentNodeID: parentNodeID,
				Gating:       datatypes.JSON([]byte(`{}`)),
				Metadata:     datatypes.JSON(mustJSON(nodeMeta)),
				CreatedAt:    now,
				UpdatedAt:    now,
			}
			nodeRows = append(nodeRows, row)
		}

		if len(nodeRows) > 0 {
			if err := dbc.Tx.WithContext(dbc.Ctx).
				Clauses(clause.OnConflict{
					Columns: []clause.Column{{Name: "path_id"}, {Name: "index"}},
					DoUpdates: clause.AssignmentColumns([]string{
						"title",
						"parent_node_id",
						"gating",
						"metadata",
						"content_json",
						"updated_at",
					}),
				}).
				Create(&nodeRows).Error; err != nil {
				return err
			}
			nodesInserted = len(nodeRows)
		}

		return nil
	}); err != nil {
		return out, err
	}

	out.Nodes = nodesInserted
	if deps.Graph != nil {
		if err := syncPathStructureToNeo4j(ctx, deps, pathID); err != nil {
			deps.Log.Warn("neo4j path structure sync failed (continuing)", "error", err, "path_id", pathID.String())
		}
	}

	return out, nil
}

func qualityMode() string {
	s := strings.ToLower(strings.TrimSpace(os.Getenv("LEARNING_QUALITY_MODE")))
	if s == "" {
		return "standard"
	}
	return s
}

func shouldRefinePathStructure(structObj map[string]any) bool {
	mode := qualityMode()
	if mode == "premium" || mode == "openai" || mode == "high" {
		return true
	}
	// If the model admits uncovered concepts, always try a refine pass.
	uncovered := parsePathStructureUncovered(structObj)
	return len(uncovered) > 0
}

func parsePathStructureUncovered(obj map[string]any) []string {
	if obj == nil {
		return nil
	}
	cc, _ := obj["coverage_check"].(map[string]any)
	if cc == nil {
		return nil
	}
	return dedupeStrings(stringSliceFromAny(cc["uncovered_concept_keys"]))
}

func refinePathStructure(
	ctx context.Context,
	deps PathPlanBuildDeps,
	pathCharterJSON string,
	conceptsJSON string,
	edgesJSON string,
	userKnowledgeJSON string,
	draft map[string]any,
) (map[string]any, bool) {
	if deps.AI == nil {
		return nil, false
	}
	draftJSON, _ := json.Marshal(draft)
	uncovered := parsePathStructureUncovered(draft)

	system := strings.TrimSpace(`
You refine an existing learning path structure to make it feel premium and coherent.
Improve sequencing, grouping, and coverage. Keep titles crisp and non-generic.
Return JSON only that matches the schema.`)

	var user strings.Builder
	user.WriteString("PATH_CHARTER_JSON:\n")
	user.WriteString(pathCharterJSON)
	user.WriteString("\n\nCONCEPTS_JSON:\n")
	user.WriteString(conceptsJSON)
	user.WriteString("\n\nEDGES_JSON:\n")
	user.WriteString(edgesJSON)
	if strings.TrimSpace(userKnowledgeJSON) != "" && strings.TrimSpace(userKnowledgeJSON) != "(none)" {
		user.WriteString("\n\nUSER_KNOWLEDGE_JSON:\n")
		user.WriteString(userKnowledgeJSON)
	}
	user.WriteString("\n\nDRAFT_PATH_STRUCTURE_JSON:\n")
	user.WriteString(string(draftJSON))
	user.WriteString("\n")
	if len(uncovered) > 0 {
		user.WriteString("\nUNCOVERED_CONCEPT_KEYS (must cover these):\n")
		user.WriteString(strings.Join(uncovered, ", "))
		user.WriteString("\n")
	}

	user.WriteString(`

Refinement rubric:
- Cover all concepts (uncovered_concept_keys should be empty).
- Use meaningful modules when the topic is broad; avoid a flat list when hierarchy helps.
- Ensure prerequisites flow naturally (no big jumps).
- Include a capstone when it meaningfully integrates multiple concepts.
- Include review nodes only when they genuinely help retention (don’t spam).
- Keep depth <= 3, indices contiguous from 1.`)

	schema := prompts.PathStructureSchema()
	obj, err := deps.AI.GenerateJSON(ctx, system, user.String(), "path_structure_refine", schema)
	if err != nil {
		return nil, false
	}
	if len(parsePathStructureNodes(obj)) == 0 {
		return nil, false
	}
	return obj, true
}

type pathStructureNode struct {
	Index             int              `json:"index"`
	ParentIndex       *int             `json:"parent_index"`
	NodeKind          string           `json:"node_kind"`
	DocTemplate       string           `json:"doc_template"`
	Title             string           `json:"title"`
	Goal              string           `json:"goal"`
	ConceptKeys       []string         `json:"concept_keys"`
	PrereqConceptKeys []string         `json:"prereq_concept_keys"`
	Difficulty        string           `json:"difficulty"`
	ActivitySlots     []map[string]any `json:"activity_slots"`
}

func parsePathStructureNodes(obj map[string]any) []pathStructureNode {
	raw, ok := obj["nodes"]
	if !ok || raw == nil {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]pathStructureNode, 0, len(arr))
	for _, x := range arr {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		index := intFromAny(m["index"], 0)
		if index <= 0 {
			continue
		}
		title := strings.TrimSpace(stringFromAny(m["title"]))
		if title == "" {
			continue
		}
		var parentIndex *int
		if v, ok := m["parent_index"]; ok && v != nil {
			if pi := intFromAny(v, 0); pi > 0 {
				parentIndex = &pi
			}
		}
		nodeKind := strings.ToLower(strings.TrimSpace(stringFromAny(m["node_kind"])))
		if nodeKind == "" {
			nodeKind = "lesson"
		}
		docTemplate := strings.ToLower(strings.TrimSpace(stringFromAny(m["doc_template"])))
		if docTemplate == "" {
			docTemplate = "concept"
		}
		slots := []map[string]any{}
		if a, ok := m["activity_slots"].([]any); ok {
			for _, y := range a {
				if mm, ok := y.(map[string]any); ok {
					slots = append(slots, mm)
				}
			}
		}
		out = append(out, pathStructureNode{
			Index:             index,
			ParentIndex:       parentIndex,
			NodeKind:          nodeKind,
			DocTemplate:       docTemplate,
			Title:             title,
			Goal:              strings.TrimSpace(stringFromAny(m["goal"])),
			ConceptKeys:       dedupeStrings(stringSliceFromAny(m["concept_keys"])),
			PrereqConceptKeys: dedupeStrings(stringSliceFromAny(m["prereq_concept_keys"])),
			Difficulty:        strings.TrimSpace(stringFromAny(m["difficulty"])),
			ActivitySlots:     slots,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return normalizePathStructureNodes(out)
}

func normalizePathStructureNodes(in []pathStructureNode) []pathStructureNode {
	if len(in) == 0 {
		return nil
	}

	// Ensure stable ordering and unique indices (keep first occurrence).
	sort.Slice(in, func(i, j int) bool { return in[i].Index < in[j].Index })
	byIndex := map[int]pathStructureNode{}
	ordered := make([]int, 0, len(in))
	for _, n := range in {
		if n.Index <= 0 || strings.TrimSpace(n.Title) == "" {
			continue
		}
		if _, exists := byIndex[n.Index]; exists {
			continue
		}
		byIndex[n.Index] = n
		ordered = append(ordered, n.Index)
	}
	if len(ordered) == 0 {
		return nil
	}

	hasIndex := func(idx int) bool {
		_, ok := byIndex[idx]
		return ok
	}

	// Clean parent refs (must exist, must be < index, no self refs).
	for _, idx := range ordered {
		n := byIndex[idx]
		if n.ParentIndex == nil {
			continue
		}
		pi := *n.ParentIndex
		if pi <= 0 || pi == idx || pi >= idx || !hasIndex(pi) {
			n.ParentIndex = nil
			byIndex[idx] = n
		}
	}

	// Break cycles + enforce depth <= 3 (module -> lesson -> leaf).
	parentOf := func(idx int) *int {
		n := byIndex[idx]
		return n.ParentIndex
	}
	for _, idx := range ordered {
		n := byIndex[idx]
		seen := map[int]bool{idx: true}
		depth := 0
		cur := idx
		for {
			p := parentOf(cur)
			if p == nil || *p <= 0 {
				break
			}
			if seen[*p] {
				// Cycle detected; detach this node.
				n.ParentIndex = nil
				byIndex[idx] = n
				break
			}
			seen[*p] = true
			depth++
			if depth >= 3 {
				// Too deep; detach this node to keep UI + generation sane.
				n.ParentIndex = nil
				byIndex[idx] = n
				break
			}
			cur = *p
		}
	}

	out := make([]pathStructureNode, 0, len(ordered))
	for _, idx := range ordered {
		out = append(out, byIndex[idx])
	}
	return out
}

func stringsOr(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func conceptIDs(concepts []*types.Concept) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(concepts))
	for _, c := range concepts {
		if c != nil && c.ID != uuid.Nil {
			out = append(out, c.ID)
		}
	}
	return out
}

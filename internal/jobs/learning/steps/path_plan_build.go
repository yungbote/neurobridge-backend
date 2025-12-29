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
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/learning/prompts"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type PathPlanBuildDeps struct {
	DB          *gorm.DB
	Log         *logger.Logger
	Path        repos.PathRepo
	PathNodes   repos.PathNodeRepo
	Concepts    repos.ConceptRepo
	Edges       repos.ConceptEdgeRepo
	Summaries   repos.MaterialSetSummaryRepo
	UserProfile repos.UserProfileVectorRepo
	AI          openai.Client
	Bootstrap   services.LearningBuildBootstrapService
}

type PathPlanBuildInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
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

	pathID, err := deps.Bootstrap.EnsurePath(dbctx.Context{Ctx: ctx}, in.OwnerUserID, in.MaterialSetID)
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
		out.Nodes = len(existingNodes)
		return out, nil
	}

	up, err := deps.UserProfile.GetByUserID(dbctx.Context{Ctx: ctx}, in.OwnerUserID)
	if err != nil || up == nil || strings.TrimSpace(up.ProfileDoc) == "" {
		return out, fmt.Errorf("path_plan_build: missing user_profile_doc (run user_profile_refresh first)")
	}

	var summaryText string
	if rows, err := deps.Summaries.GetByMaterialSetIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{in.MaterialSetID}); err == nil && len(rows) > 0 && rows[0] != nil {
		summaryText = strings.TrimSpace(rows[0].SummaryMD)
	}

	concepts, err := deps.Concepts.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID)
	if err != nil {
		return out, err
	}
	if len(concepts) == 0 {
		return out, fmt.Errorf("path_plan_build: no concepts for path (run concept_graph_build first)")
	}

	edges, err := deps.Edges.GetByConceptIDs(dbctx.Context{Ctx: ctx}, conceptIDs(concepts))
	if err != nil {
		return out, err
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

	// ---- Prompt: Path charter ----
	charterPrompt, err := prompts.Build(prompts.PromptPathCharter, prompts.Input{
		UserProfileDoc: up.ProfileDoc,
		BundleExcerpt:  summaryText,
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
		PathCharterJSON: string(charterJSON),
		ConceptsJSON:    string(conceptsJSON),
		EdgesJSON:       string(edgesJSON),
	})
	if err != nil {
		return out, err
	}
	structObj, err := deps.AI.GenerateJSON(ctx, structPrompt.System, structPrompt.User, structPrompt.SchemaName, structPrompt.Schema)
	if err != nil {
		return out, err
	}

	title := strings.TrimSpace(stringFromAny(structObj["title"]))
	desc := strings.TrimSpace(stringFromAny(structObj["description"]))
	nodesOut := parsePathStructureNodes(structObj)
	if len(nodesOut) == 0 {
		return out, fmt.Errorf("path_plan_build: 0 nodes returned")
	}

	now := time.Now().UTC()

	if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		if _, err := deps.Bootstrap.EnsurePath(dbc, in.OwnerUserID, in.MaterialSetID); err != nil {
			return err
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
		meta["structure"] = structObj
		meta["updated_at"] = now.Format(time.RFC3339Nano)
		if err := deps.Path.UpdateFields(dbc, pathID, map[string]interface{}{
			"title":       stringsOr(title, "Learning Path"),
			"description": desc,
			"metadata":    datatypes.JSON(mustJSON(meta)),
		}); err != nil {
			return err
		}

		for _, n := range nodesOut {
			nodeMeta := map[string]any{
				"goal":                n.Goal,
				"concept_keys":        n.ConceptKeys,
				"prereq_concept_keys": n.PrereqConceptKeys,
				"difficulty":          n.Difficulty,
				"activity_slots":      n.ActivitySlots,
			}
			row := &types.PathNode{
				ID:           uuid.New(),
				PathID:       pathID,
				Index:        n.Index,
				Title:        n.Title,
				ParentNodeID: nil,
				Gating:       datatypes.JSON([]byte(`{}`)),
				Metadata:     datatypes.JSON(mustJSON(nodeMeta)),
				CreatedAt:    now,
				UpdatedAt:    now,
			}
			if err := deps.PathNodes.Upsert(dbc, row); err != nil {
				return err
			}
			out.Nodes++
		}

		return nil
	}); err != nil {
		return out, err
	}

	return out, nil
}

type pathStructureNode struct {
	Index             int              `json:"index"`
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
		title := strings.TrimSpace(stringFromAny(m["title"]))
		if title == "" {
			continue
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
			Index:             intFromAny(m["index"], 0),
			Title:             title,
			Goal:              strings.TrimSpace(stringFromAny(m["goal"])),
			ConceptKeys:       dedupeStrings(stringSliceFromAny(m["concept_keys"])),
			PrereqConceptKeys: dedupeStrings(stringSliceFromAny(m["prereq_concept_keys"])),
			Difficulty:        strings.TrimSpace(stringFromAny(m["difficulty"])),
			ActivitySlots:     slots,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
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

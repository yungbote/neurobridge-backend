package steps

import (
	"context"
	"encoding/json"
	"fmt"
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

type CoverageCoherenceAuditDeps struct {
	DB         *gorm.DB
	Log        *logger.Logger
	Path       repos.PathRepo
	PathNodes  repos.PathNodeRepo
	Concepts   repos.ConceptRepo
	Activities repos.ActivityRepo
	Variants   repos.ActivityVariantRepo
	AI         openai.Client
	Bootstrap  services.LearningBuildBootstrapService
}

type CoverageCoherenceAuditInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
}

type CoverageCoherenceAuditOutput struct {
	AuditWritten bool `json:"audit_written"`
}

func CoverageCoherenceAudit(ctx context.Context, deps CoverageCoherenceAuditDeps, in CoverageCoherenceAuditInput) (CoverageCoherenceAuditOutput, error) {
	out := CoverageCoherenceAuditOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Path == nil || deps.PathNodes == nil || deps.Concepts == nil || deps.Activities == nil || deps.Variants == nil || deps.AI == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("coverage_coherence_audit: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("coverage_coherence_audit: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("coverage_coherence_audit: missing material_set_id")
	}

	pathID, err := deps.Bootstrap.EnsurePath(dbctx.Context{Ctx: ctx}, in.OwnerUserID, in.MaterialSetID)
	if err != nil {
		return out, err
	}

	concepts, err := deps.Concepts.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID)
	if err != nil {
		return out, err
	}
	nodes, err := deps.PathNodes.GetByPathIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{pathID})
	if err != nil {
		return out, err
	}
	acts, err := deps.Activities.ListByOwner(dbctx.Context{Ctx: ctx}, "path", &pathID)
	if err != nil {
		return out, err
	}
	activityIDs := make([]uuid.UUID, 0, len(acts))
	for _, a := range acts {
		if a != nil && a.ID != uuid.Nil {
			activityIDs = append(activityIDs, a.ID)
		}
	}
	variants, _ := deps.Variants.GetByActivityIDs(dbctx.Context{Ctx: ctx}, activityIDs)

	conceptsJSON, _ := json.Marshal(map[string]any{"concepts": compactConceptsForAudit(concepts)})
	nodesJSON, _ := json.Marshal(map[string]any{"nodes": compactPathNodesForAudit(nodes)})
	actsJSON, _ := json.Marshal(map[string]any{"activities": compactActivitiesForAudit(acts)})
	variantsJSON, _ := json.Marshal(map[string]any{"variants": compactVariantsForAudit(variants)})

	curriculumSpecJSON := ""
	if deps.Path != nil {
		if pr, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, pathID); err == nil && pr != nil && len(pr.Metadata) > 0 && strings.TrimSpace(string(pr.Metadata)) != "" && string(pr.Metadata) != "null" {
			var meta map[string]any
			if json.Unmarshal(pr.Metadata, &meta) == nil && meta != nil {
				curriculumSpecJSON = CurriculumSpecBriefJSONFromPathMeta(meta, 6)
			}
		}
	}

	p, err := prompts.Build(prompts.PromptCoverageAndCoheranceAudit, prompts.Input{
		CurriculumSpecJSON: curriculumSpecJSON,
		ConceptsJSON:       string(conceptsJSON),
		PathNodesJSON:      string(nodesJSON),
		NodePlansJSON:      "[]",
		ChainPlansJSON:     "[]",
		ActivitiesJSON:     string(actsJSON),
		VariantsJSON:       string(variantsJSON),
	})
	if err != nil {
		return out, err
	}
	obj, err := deps.AI.GenerateJSON(ctx, p.System, p.User, p.SchemaName, p.Schema)
	if err != nil && isContextLengthExceeded(err) {
		p2, pErr := prompts.Build(prompts.PromptCoverageAndCoheranceAudit, prompts.Input{
			CurriculumSpecJSON: curriculumSpecJSON,
			ConceptsJSON:       string(conceptsJSON),
			PathNodesJSON:      string(nodesJSON),
			NodePlansJSON:      "[]",
			ChainPlansJSON:     "[]",
			ActivitiesJSON:     `{"activities":[],"omitted":true}`,
			VariantsJSON:       `{"variants":[],"omitted":true}`,
		})
		if pErr == nil {
			obj, err = deps.AI.GenerateJSON(ctx, p2.System, p2.User, p2.SchemaName, p2.Schema)
		}
	}
	if err != nil {
		return out, err
	}

	now := time.Now().UTC()
	if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		pr, err := deps.Path.GetByID(dbc, pathID)
		if err != nil {
			return err
		}
		meta := map[string]any{}
		if pr != nil && len(pr.Metadata) > 0 && strings.TrimSpace(string(pr.Metadata)) != "" && string(pr.Metadata) != "null" {
			_ = json.Unmarshal(pr.Metadata, &meta)
		}
		meta["audit"] = obj
		meta["audit_updated_at"] = now.Format(time.RFC3339Nano)
		return deps.Path.UpdateFields(dbc, pathID, map[string]interface{}{"metadata": datatypes.JSON(mustJSON(meta))})
	}); err != nil {
		return out, err
	}

	out.AuditWritten = true
	return out, nil
}

func isContextLengthExceeded(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "context_length_exceeded") || strings.Contains(s, "exceeds the context window")
}

func compactConceptsForAudit(in []*types.Concept) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, c := range in {
		if c == nil || c.ID == uuid.Nil {
			continue
		}
		m := map[string]any{
			"id":         c.ID.String(),
			"key":        strings.TrimSpace(c.Key),
			"name":       strings.TrimSpace(c.Name),
			"summary":    shorten(c.Summary, 400),
			"depth":      c.Depth,
			"sort_index": c.SortIndex,
		}
		if c.ParentID != nil && *c.ParentID != uuid.Nil {
			m["parent_id"] = c.ParentID.String()
		}
		out = append(out, m)
	}
	return out
}

func compactPathNodesForAudit(in []*types.PathNode) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, n := range in {
		if n == nil || n.ID == uuid.Nil {
			continue
		}
		m := map[string]any{
			"id":             n.ID.String(),
			"index":          n.Index,
			"title":          strings.TrimSpace(n.Title),
			"parent_node_id": "",
		}
		if n.ParentNodeID != nil && *n.ParentNodeID != uuid.Nil {
			m["parent_node_id"] = n.ParentNodeID.String()
		} else {
			delete(m, "parent_node_id")
		}

		meta := map[string]any{}
		if len(n.Metadata) > 0 && strings.TrimSpace(string(n.Metadata)) != "" && string(n.Metadata) != "null" {
			_ = json.Unmarshal(n.Metadata, &meta)
		}
		if goal := strings.TrimSpace(stringFromAny(meta["goal"])); goal != "" {
			m["goal"] = shorten(goal, 600)
		}
		if keys := dedupeStrings(stringSliceFromAny(meta["concept_keys"])); len(keys) > 0 {
			m["concept_keys"] = keys
		}
		if rawSlots, ok := meta["activity_slots"].([]any); ok && len(rawSlots) > 0 {
			m["activity_slots"] = rawSlots
		}
		if diff := strings.TrimSpace(stringFromAny(meta["difficulty"])); diff != "" {
			m["difficulty"] = diff
		}

		out = append(out, m)
	}
	return out
}

func compactActivitiesForAudit(in []*types.Activity) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, a := range in {
		if a == nil || a.ID == uuid.Nil {
			continue
		}
		m := map[string]any{
			"id":                a.ID.String(),
			"kind":              strings.TrimSpace(a.Kind),
			"title":             shorten(strings.TrimSpace(a.Title), 200),
			"estimated_minutes": a.EstimatedMinutes,
			"status":            strings.TrimSpace(a.Status),
		}

		meta := map[string]any{}
		if len(a.Metadata) > 0 && strings.TrimSpace(string(a.Metadata)) != "" && string(a.Metadata) != "null" {
			_ = json.Unmarshal(a.Metadata, &meta)
		}
		if v := strings.TrimSpace(stringFromAny(meta["path_node_id"])); v != "" {
			m["path_node_id"] = v
		}
		if slot := intFromAny(meta["slot"], -1); slot >= 0 {
			m["slot"] = slot
		}

		out = append(out, m)
	}
	return out
}

func compactVariantsForAudit(in []*types.ActivityVariant) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, v := range in {
		if v == nil || v.ID == uuid.Nil {
			continue
		}
		m := map[string]any{
			"id":          v.ID.String(),
			"activity_id": v.ActivityID.String(),
			"variant":     strings.TrimSpace(v.Variant),
		}
		out = append(out, m)
	}
	return out
}

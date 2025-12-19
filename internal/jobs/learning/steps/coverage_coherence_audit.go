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
	"github.com/yungbote/neurobridge-backend/internal/learning/prompts"
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

	pathID, err := deps.Bootstrap.EnsurePath(ctx, nil, in.OwnerUserID, in.MaterialSetID)
	if err != nil {
		return out, err
	}

	concepts, err := deps.Concepts.GetByScope(ctx, nil, "path", &pathID)
	if err != nil {
		return out, err
	}
	nodes, err := deps.PathNodes.GetByPathIDs(ctx, nil, []uuid.UUID{pathID})
	if err != nil {
		return out, err
	}
	acts, err := deps.Activities.ListByOwner(ctx, nil, "path", &pathID)
	if err != nil {
		return out, err
	}
	activityIDs := make([]uuid.UUID, 0, len(acts))
	for _, a := range acts {
		if a != nil && a.ID != uuid.Nil {
			activityIDs = append(activityIDs, a.ID)
		}
	}
	variants, _ := deps.Variants.GetByActivityIDs(ctx, nil, activityIDs)

	conceptsJSON, _ := json.Marshal(map[string]any{"concepts": concepts})
	nodesJSON, _ := json.Marshal(map[string]any{"nodes": nodes})
	actsJSON, _ := json.Marshal(map[string]any{"activities": acts})
	variantsJSON, _ := json.Marshal(map[string]any{"variants": variants})

	p, err := prompts.Build(prompts.PromptCoverageAndCoheranceAudit, prompts.Input{
		ConceptsJSON:   string(conceptsJSON),
		PathNodesJSON:  string(nodesJSON),
		NodePlansJSON:  "[]",
		ChainPlansJSON: "[]",
		ActivitiesJSON: string(actsJSON),
		VariantsJSON:   string(variantsJSON),
	})
	if err != nil {
		return out, err
	}
	obj, err := deps.AI.GenerateJSON(ctx, p.System, p.User, p.SchemaName, p.Schema)
	if err != nil {
		return out, err
	}

	now := time.Now().UTC()
	if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		pr, err := deps.Path.GetByID(ctx, tx, pathID)
		if err != nil {
			return err
		}
		meta := map[string]any{}
		if pr != nil && len(pr.Metadata) > 0 && strings.TrimSpace(string(pr.Metadata)) != "" && string(pr.Metadata) != "null" {
			_ = json.Unmarshal(pr.Metadata, &meta)
		}
		meta["audit"] = obj
		meta["audit_updated_at"] = now.Format(time.RFC3339Nano)
		return deps.Path.UpdateFields(ctx, tx, pathID, map[string]interface{}{"metadata": datatypes.JSON(mustJSON(meta))})
	}); err != nil {
		return out, err
	}

	out.AuditWritten = true
	return out, nil
}

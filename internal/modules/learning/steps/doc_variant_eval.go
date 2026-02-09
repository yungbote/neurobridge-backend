package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	docgen "github.com/yungbote/neurobridge-backend/internal/modules/learning/docgen"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type DocVariantEvalDeps struct {
	DB           *gorm.DB
	Log          *logger.Logger
	Exposures    repos.DocVariantExposureRepo
	Outcomes     repos.DocVariantOutcomeRepo
	NodeRuns     repos.NodeRunRepo
	ConceptState repos.UserConceptStateRepo
	Bootstrap    services.LearningBuildBootstrapService
}

type DocVariantEvalInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	PathID        uuid.UUID
}

type DocVariantEvalOutput struct {
	PathID          uuid.UUID `json:"path_id"`
	Considered      int       `json:"considered"`
	OutcomesCreated int       `json:"outcomes_created"`
	OutcomesSkipped int       `json:"outcomes_skipped"`
}

type docVariantBaseline struct {
	ConceptID            string  `json:"concept_id,omitempty"`
	ConceptKey           string  `json:"concept_key,omitempty"`
	Mastery              float64 `json:"mastery,omitempty"`
	Confidence           float64 `json:"confidence,omitempty"`
	EpistemicUncertainty float64 `json:"epistemic_uncertainty,omitempty"`
	AleatoricUncertainty float64 `json:"aleatoric_uncertainty,omitempty"`
}

func DocVariantEval(ctx context.Context, deps DocVariantEvalDeps, in DocVariantEvalInput) (DocVariantEvalOutput, error) {
	out := DocVariantEvalOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Exposures == nil || deps.Outcomes == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("doc_variant_eval: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("doc_variant_eval: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("doc_variant_eval: missing material_set_id")
	}

	pathID := in.PathID
	if pathID != uuid.Nil {
		resolved, err := resolvePathID(ctx, deps.Bootstrap, in.OwnerUserID, in.MaterialSetID, pathID)
		if err != nil {
			return out, err
		}
		pathID = resolved
		out.PathID = pathID
	}

	cutoff := time.Now().UTC().Add(-time.Duration(docgen.DocVariantEvalMinAgeMinutes()) * time.Minute)
	limit := docgen.DocVariantEvalLimit()

	var pathPtr *uuid.UUID
	if pathID != uuid.Nil {
		pathPtr = &pathID
	}

	exposures, err := deps.Exposures.ListUnevaluatedByUser(dbctx.Context{Ctx: ctx}, in.OwnerUserID, pathPtr, cutoff, limit)
	if err != nil {
		return out, err
	}

	for _, exp := range exposures {
		if exp == nil || exp.ID == uuid.Nil {
			continue
		}
		out.Considered++

		metrics, ok := buildDocVariantOutcomeMetrics(ctx, deps, exp)
		if !ok {
			out.OutcomesSkipped++
			continue
		}

		metricsJSON, _ := json.Marshal(metrics)
		outcome := &types.DocVariantOutcome{
			ID:            uuid.New(),
			ExposureID:    &exp.ID,
			UserID:        exp.UserID,
			PathID:        exp.PathID,
			PathNodeID:    exp.PathNodeID,
			VariantID:     exp.VariantID,
			PolicyVersion: strings.TrimSpace(exp.PolicyVersion),
			SchemaVersion: 1,
			OutcomeKind:   "eval_v1",
			MetricsJSON:   datatypes.JSON(metricsJSON),
			CreatedAt:     time.Now().UTC(),
		}
		if err := deps.Outcomes.Create(dbctx.Context{Ctx: ctx}, outcome); err != nil {
			out.OutcomesSkipped++
			if deps.Log != nil {
				deps.Log.Warn("doc_variant_eval outcome create failed", "error", err, "exposure_id", exp.ID.String())
			}
			continue
		}
		out.OutcomesCreated++
	}

	return out, nil
}

func buildDocVariantOutcomeMetrics(ctx context.Context, deps DocVariantEvalDeps, exp *types.DocVariantExposure) (map[string]any, bool) {
	if exp == nil {
		return nil, false
	}
	metrics := map[string]any{
		"exposure_id":         exp.ID.String(),
		"exposure_kind":       strings.TrimSpace(exp.ExposureKind),
		"variant_kind":        strings.TrimSpace(exp.VariantKind),
		"policy_version":      strings.TrimSpace(exp.PolicyVersion),
		"schema_version":      exp.SchemaVersion,
		"exposure_age_sec":    time.Since(exp.CreatedAt).Seconds(),
		"content_hash":        strings.TrimSpace(exp.ContentHash),
		"concepts_total":      0,
		"concepts_with_state": 0,
	}

	baseline := []docVariantBaseline{}
	if len(exp.BaselineJSON) > 0 && string(exp.BaselineJSON) != "null" {
		_ = json.Unmarshal(exp.BaselineJSON, &baseline)
	}

	conceptIDs := make([]uuid.UUID, 0, len(baseline))
	for _, b := range baseline {
		if b.ConceptID == "" {
			continue
		}
		if id, err := uuid.Parse(strings.TrimSpace(b.ConceptID)); err == nil && id != uuid.Nil {
			conceptIDs = append(conceptIDs, id)
		}
	}
	metrics["concepts_total"] = len(conceptIDs)

	current := map[uuid.UUID]*types.UserConceptState{}
	if deps.ConceptState != nil && len(conceptIDs) > 0 && exp.UserID != uuid.Nil {
		if rows, err := deps.ConceptState.ListByUserAndConceptIDs(dbctx.Context{Ctx: ctx}, exp.UserID, conceptIDs); err == nil {
			for _, row := range rows {
				if row != nil && row.ConceptID != uuid.Nil {
					current[row.ConceptID] = row
				}
			}
		}
	}

	var (
		baselineMasterySum float64
		currentMasterySum  float64
		deltaMasterySum    float64
		deltaConfSum       float64
		deltaUncSum        float64
		paired             int
	)

	for _, b := range baseline {
		id, err := uuid.Parse(strings.TrimSpace(b.ConceptID))
		if err != nil || id == uuid.Nil {
			continue
		}
		baseMastery := clamp01VariantEval(b.Mastery)
		baseConf := clamp01VariantEval(b.Confidence)
		baseUnc := math.Max(clamp01VariantEval(b.EpistemicUncertainty), clamp01VariantEval(b.AleatoricUncertainty))
		baselineMasterySum += baseMastery

		st := current[id]
		if st == nil {
			continue
		}
		curMastery := clamp01VariantEval(st.Mastery)
		curConf := clamp01VariantEval(st.Confidence)
		curUnc := math.Max(clamp01VariantEval(st.EpistemicUncertainty), clamp01VariantEval(st.AleatoricUncertainty))

		currentMasterySum += curMastery
		deltaMasterySum += curMastery - baseMastery
		deltaConfSum += curConf - baseConf
		deltaUncSum += curUnc - baseUnc
		paired++
	}

	if len(baseline) > 0 {
		metrics["baseline_mastery_mean"] = baselineMasterySum / float64(len(baseline))
	}
	if paired > 0 {
		metrics["concepts_with_state"] = paired
		metrics["current_mastery_mean"] = currentMasterySum / float64(paired)
		metrics["mastery_delta_mean"] = deltaMasterySum / float64(paired)
		metrics["confidence_delta_mean"] = deltaConfSum / float64(paired)
		metrics["uncertainty_delta_mean"] = deltaUncSum / float64(paired)
	}

	if deps.NodeRuns != nil && exp.UserID != uuid.Nil && exp.PathNodeID != uuid.Nil {
		if nr, err := deps.NodeRuns.GetByUserAndNodeID(dbctx.Context{Ctx: ctx}, exp.UserID, exp.PathNodeID); err == nil && nr != nil {
			metrics["node_state"] = strings.TrimSpace(string(nr.State))
			metrics["node_completed"] = nr.CompletedAt != nil || strings.EqualFold(string(nr.State), "completed")
			metrics["node_attempts"] = nr.AttemptCount
			metrics["node_last_score"] = nr.LastScore
			if nr.StartedAt != nil && nr.CompletedAt != nil {
				metrics["time_to_complete_sec"] = nr.CompletedAt.Sub(*nr.StartedAt).Seconds()
			}
			if nr.LastSeenAt != nil {
				metrics["node_last_seen_at"] = nr.LastSeenAt.UTC().Format(time.RFC3339)
			}
		}
	}

	return metrics, true
}

func clamp01VariantEval(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

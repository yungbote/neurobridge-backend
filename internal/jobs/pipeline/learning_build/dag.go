package learning_build

import (
	"github.com/google/uuid"

	orchestrator "github.com/yungbote/neurobridge-backend/internal/jobs/orchestrator"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
)

var stageDeps = map[string][]string{
	"embed_chunks":           {"ingest_chunks"},
	"material_set_summarize": {"ingest_chunks"},
	"concept_graph_build":    {"ingest_chunks"},
	"concept_cluster_build":  {"concept_graph_build"},
	"chain_signature_build":  {"concept_cluster_build"},

	"user_profile_refresh":   {"ingest_chunks"},
	"teaching_patterns_seed": {"user_profile_refresh"},

	"path_plan_build": {"concept_graph_build", "material_set_summarize", "user_profile_refresh"},

	"node_figures_plan_build": {"path_plan_build", "embed_chunks"},
	"node_figures_render":     {"node_figures_plan_build"},
	"node_videos_plan_build":  {"path_plan_build", "embed_chunks"},
	"node_videos_render":      {"node_videos_plan_build"},

	"node_doc_build": {"path_plan_build", "embed_chunks", "node_figures_render", "node_videos_render"},

	"realize_activities":       {"path_plan_build", "embed_chunks", "user_profile_refresh", "concept_graph_build"},
	"coverage_coherence_audit": {"realize_activities"},
	"progression_compact":      {"user_profile_refresh"},
	"variant_stats_refresh":    {"user_profile_refresh"},
	"priors_refresh":           {"realize_activities", "variant_stats_refresh", "chain_signature_build"},
	"completed_unit_refresh":   {"realize_activities", "progression_compact", "chain_signature_build"},
}

func buildChildStages(setID, sagaID uuid.UUID) []orchestrator.Stage {
	stages := make([]orchestrator.Stage, 0, len(stageOrder))
	for _, name := range stageOrder {
		stage := orchestrator.Stage{
			Name:         name,
			Deps:         stageDeps[name],
			Mode:         orchestrator.ModeChild,
			ChildJobType: name,
			ChildEntity: func(ctx *jobrt.Context) (string, *uuid.UUID) {
				return "material_set", &setID
			},
			ChildPayload: func(ctx *jobrt.Context, st *orchestrator.OrchestratorState) (map[string]any, error) {
				return map[string]any{
					"material_set_id": setID.String(),
					"saga_id":         sagaID.String(),
				}, nil
			},
		}
		stages = append(stages, stage)
	}
	return stages
}

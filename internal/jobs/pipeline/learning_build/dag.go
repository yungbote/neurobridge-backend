package learning_build

import (
	"fmt"
	"github.com/google/uuid"
	"strings"

	orchestrator "github.com/yungbote/neurobridge-backend/internal/jobs/orchestrator"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
)

var stageDeps = map[string][]string{
	"ingest_chunks": {"web_resources_seed"},

	// Gate expensive downstream work on intake so we don't burn compute while waiting for user answers.
	"embed_chunks":           {"path_intake"},
	"material_set_summarize": {"ingest_chunks"},
	// Intake should happen before concept graph so we can incorporate user intent and reduce noise.
	// Depend only on ingestion so intake can still proceed even if summarization fails.
	"path_intake": {"ingest_chunks"},

	"concept_graph_build":    {"path_intake"},
	"concept_cluster_build":  {"concept_graph_build"},
	"chain_signature_build":  {"concept_cluster_build"},

	"user_profile_refresh":   {"path_intake"},
	"teaching_patterns_seed": {"user_profile_refresh"},

	"path_plan_build":    {"concept_graph_build", "material_set_summarize", "user_profile_refresh", "path_intake"},
	"path_cover_render":  {"path_plan_build"},

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

func buildChildStages(setID, sagaID, pathID, threadID uuid.UUID) []orchestrator.Stage {
	stages := make([]orchestrator.Stage, 0, len(stageOrder))
	for _, name := range stageOrder {
		name := name
		stage := orchestrator.Stage{
			Name:         name,
			Deps:         stageDeps[name],
			Mode:         orchestrator.ModeChild,
			ChildJobType: name,
			ChildEntity: func(ctx *jobrt.Context) (string, *uuid.UUID) {
				return "material_set", &setID
			},
			ChildPayload: func(ctx *jobrt.Context, st *orchestrator.OrchestratorState) (map[string]any, error) {
				out := map[string]any{
					"material_set_id": setID.String(),
					"saga_id":         sagaID.String(),
				}
				if pathID != uuid.Nil {
					out["path_id"] = pathID.String()
				}
				if threadID != uuid.Nil {
					out["thread_id"] = threadID.String()
				}
				// Prompt-only builds can pass the prompt through for seeding; avoid duplicating it on every child job.
				if name == "web_resources_seed" && ctx != nil {
					if v, ok := ctx.Payload()["prompt"]; ok && v != nil && strings.TrimSpace(fmt.Sprint(v)) != "" {
						out["prompt"] = strings.TrimSpace(fmt.Sprint(v))
					}
				}
				return out, nil
			},
		}
		stages = append(stages, stage)
	}
	return stages
}

package learning_build

import (
	"fmt"
	"github.com/google/uuid"
	"strings"

	orchestrator "github.com/yungbote/neurobridge-backend/internal/jobs/orchestrator"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
)

func buildChildStages(setID, sagaID, pathID, threadID uuid.UUID) []orchestrator.Stage {
	order := pipelineStageOrder(nil)
	specs := map[string]yamlStageSpec{}
	if rt := currentPipelineRuntime(nil); rt != nil {
		specs = rt.Stages
	}
	return buildChildStagesForNames(order, setID, sagaID, pathID, threadID, specs)
}

func buildChildStagesForNames(stageNames []string, setID, sagaID, pathID, threadID uuid.UUID, specs map[string]yamlStageSpec) []orchestrator.Stage {
	waitpointDeps := map[string]bool{}
	for _, name := range stageNames {
		spec := yamlStageSpec{}
		if specs != nil {
			if s, ok := specs[name]; ok {
				spec = s
			}
		}
		if isWaitpointStage(name, spec) {
			deps := stageDepsForSpec(name, spec)
			for _, dep := range deps {
				if strings.TrimSpace(dep) != "" {
					waitpointDeps[dep] = true
				}
			}
		}
	}

	stages := make([]orchestrator.Stage, 0, len(stageNames))
	for _, name := range stageNames {
		name := name
		spec := yamlStageSpec{}
		if specs != nil {
			if s, ok := specs[name]; ok {
				spec = s
			}
		}
		jobType := strings.TrimSpace(spec.JobType)
		if jobType == "" {
			jobType = name
		}
		if isWaitpointStage(name, spec) && jobType == name {
			jobType = "waitpoint_stage"
		}
		deps := stageDepsForSpec(name, spec)
		stage := orchestrator.Stage{
			Name:         name,
			Deps:         deps,
			Mode:         orchestrator.ModeChild,
			ChildJobType: jobType,
			ChildEntity: func(ctx *jobrt.Context) (string, *uuid.UUID) {
				return "material_set", &setID
			},
			ChildPayload: func(ctx *jobrt.Context, st *orchestrator.OrchestratorState) (map[string]any, error) {
				out := map[string]any{
					"material_set_id": setID.String(),
					"saga_id":         sagaID.String(),
				}
				cfg := copyStageConfig(spec.Config)
				if waitpointDeps[name] {
					if cfg == nil {
						cfg = map[string]any{}
					}
					cfg["waitpoint_external"] = true
				}
				if len(cfg) > 0 {
					out["stage_config"] = cfg
				}
				if pathID != uuid.Nil {
					out["path_id"] = pathID.String()
				}
				if threadID != uuid.Nil {
					out["thread_id"] = threadID.String()
				}
				if isWaitpointStage(name, spec) {
					out["stage_name"] = name
					sourceStage := ""
					if cfg != nil {
						sourceStage = strings.TrimSpace(stringFromAny(cfg["source_stage"]))
					}
					if sourceStage == "" && len(deps) > 0 {
						sourceStage = strings.TrimSpace(deps[0])
					}
					if sourceStage != "" {
						out["source_stage"] = sourceStage
						if st != nil && st.Stages != nil {
							if ss, ok := st.Stages[sourceStage]; ok && ss != nil {
								if m := mapFromAny(ss.ChildResult); len(m) > 0 {
									out["source_outputs"] = m
								} else if len(ss.Outputs) > 0 {
									out["source_outputs"] = ss.Outputs
								}
							}
						}
					}
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

func stageDepsForSpec(name string, spec yamlStageSpec) []string {
	deps := pipelineStageDeps(nil, name)
	if spec.Name != "" && len(spec.DependsOn) > 0 {
		deps = spec.DependsOn
	}
	return deps
}

func isWaitpointStage(name string, spec yamlStageSpec) bool {
	if strings.EqualFold(strings.TrimSpace(spec.Type), "waitpoint") {
		return true
	}
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(name)), "_waitpoint")
}

func copyStageConfig(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func stringFromAny(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return ""
	}
}

func mapFromAny(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

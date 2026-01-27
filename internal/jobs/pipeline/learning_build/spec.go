package learning_build

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

const learningBuildPipelineEnv = "LEARNING_BUILD_PIPELINE_YAML"

//go:embed learning_build.yaml
var learningBuildSpecFS embed.FS

// fallback stage graph used when YAML is missing or invalid
var fallbackStageOrder = []string{
	"web_resources_seed",
	"ingest_chunks",
	"file_signature_build",
	"path_intake",
	"path_grouping_refine",
	"path_structure_dispatch",
	"embed_chunks",
	"material_set_summarize",
	"user_profile_refresh",
	"teaching_patterns_seed",
	"concept_graph_build",
	"material_signal_build",
	"path_structure_refine",
	"material_kg_build",
	"concept_cluster_build",
	"chain_signature_build",
	"path_plan_build",
	"path_cover_render",
	"node_figures_plan_build",
	"node_figures_render",
	"node_videos_plan_build",
	"node_videos_render",
	"node_doc_build",
	"realize_activities",
	"coverage_coherence_audit",
	"progression_compact",
	"variant_stats_refresh",
	"priors_refresh",
	"completed_unit_refresh",
}

var fallbackDispatchStageOrder = []string{
	"web_resources_seed",
	"ingest_chunks",
	"file_signature_build",
	"path_intake",
	"path_grouping_refine",
	"path_structure_dispatch",
}

var fallbackStageDeps = map[string][]string{
	"ingest_chunks":        {"web_resources_seed"},
	"file_signature_build": {"ingest_chunks"},

	"embed_chunks":           {"path_structure_dispatch"},
	"material_set_summarize": {"ingest_chunks"},
	"path_intake":            {"file_signature_build"},

	"path_grouping_refine":    {"path_intake"},
	"path_structure_dispatch": {"path_grouping_refine"},

	"concept_graph_build":   {"path_structure_dispatch"},
	"material_signal_build": {"concept_graph_build"},
	"path_structure_refine": {"concept_graph_build"},
	"material_kg_build":     {"concept_graph_build", "embed_chunks"},
	"concept_cluster_build": {"concept_graph_build"},
	"chain_signature_build": {"concept_cluster_build"},

	"user_profile_refresh":   {"path_intake"},
	"teaching_patterns_seed": {"user_profile_refresh"},

	"path_plan_build":   {"concept_graph_build", "material_signal_build", "material_set_summarize", "user_profile_refresh", "path_intake"},
	"path_cover_render": {"path_plan_build"},

	"node_figures_plan_build": {"path_plan_build", "embed_chunks", "material_kg_build"},
	"node_figures_render":     {"node_figures_plan_build"},
	"node_videos_plan_build":  {"path_plan_build", "embed_chunks", "material_kg_build"},
	"node_videos_render":      {"node_videos_plan_build"},

	"node_doc_build": {"path_plan_build", "embed_chunks", "node_figures_render", "node_videos_render", "material_kg_build"},

	"realize_activities":       {"path_plan_build", "embed_chunks", "user_profile_refresh", "concept_graph_build", "material_kg_build"},
	"coverage_coherence_audit": {"realize_activities"},
	"progression_compact":      {"user_profile_refresh"},
	"variant_stats_refresh":    {"user_profile_refresh"},
	"priors_refresh":           {"realize_activities", "variant_stats_refresh", "chain_signature_build"},
	"completed_unit_refresh":   {"realize_activities", "progression_compact", "chain_signature_build"},
}

type yamlPipelineSpec struct {
	Pipeline string                 `yaml:"pipeline"`
	Version  int                    `yaml:"version"`
	Stages   []yamlStageSpec        `yaml:"stages"`
	Variants map[string]yamlVariant `yaml:"variants"`
}

type yamlStageSpec struct {
	Name      string                 `yaml:"name"`
	Type      string                 `yaml:"type"`
	JobType   string                 `yaml:"job_type"`
	DependsOn []string               `yaml:"depends_on"`
	Enabled   *bool                  `yaml:"enabled"`
	Config    map[string]any         `yaml:"config"`
	Meta      map[string]interface{} `yaml:"meta"`
}

type yamlVariant struct {
	Stages []string `yaml:"stages"`
}

type pipelineRuntime struct {
	StageOrder    []string
	DispatchOrder []string
	Stages        map[string]yamlStageSpec
}

var runtimeOnce sync.Once
var runtimeCache *pipelineRuntime
var runtimeErr error

func currentPipelineRuntime(log *logger.Logger) *pipelineRuntime {
	runtimeOnce.Do(func() {
		runtimeCache, runtimeErr = loadPipelineRuntime()
	})
	if runtimeErr != nil {
		if log != nil {
			log.Warn("learning_build: pipeline spec load failed; using fallback", "error", runtimeErr)
		}
		return nil
	}
	return runtimeCache
}

func pipelineStageOrder(log *logger.Logger) []string {
	if rt := currentPipelineRuntime(log); rt != nil && len(rt.StageOrder) > 0 {
		return rt.StageOrder
	}
	return fallbackStageOrder
}

func pipelineDispatchOrder(log *logger.Logger) []string {
	if rt := currentPipelineRuntime(log); rt != nil && len(rt.DispatchOrder) > 0 {
		return rt.DispatchOrder
	}
	return fallbackDispatchStageOrder
}

func pipelineStageSpec(log *logger.Logger, name string) (yamlStageSpec, bool) {
	if rt := currentPipelineRuntime(log); rt != nil {
		if spec, ok := rt.Stages[name]; ok {
			return spec, true
		}
	}
	return yamlStageSpec{}, false
}

func pipelineStageDeps(log *logger.Logger, name string) []string {
	if spec, ok := pipelineStageSpec(log, name); ok {
		return spec.DependsOn
	}
	if deps, ok := fallbackStageDeps[name]; ok {
		return deps
	}
	return nil
}

func loadPipelineRuntime() (*pipelineRuntime, error) {
	data, err := readLearningBuildSpec()
	if err != nil {
		return nil, err
	}

	var spec yamlPipelineSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, err
	}
	if err := validatePipelineSpec(&spec); err != nil {
		return nil, err
	}

	order := make([]string, 0, len(spec.Stages))
	stages := make(map[string]yamlStageSpec, len(spec.Stages))
	for _, stage := range spec.Stages {
		if stage.Name == "" {
			continue
		}
		if stage.Enabled != nil && !*stage.Enabled {
			continue
		}
		if stage.JobType == "" {
			stage.JobType = stage.Name
		}
		stage.DependsOn = dedupeStrings(stage.DependsOn)
		order = append(order, stage.Name)
		stages[stage.Name] = stage
	}

	dispatch := []string{}
	if v, ok := spec.Variants["dispatch_only"]; ok {
		for _, name := range v.Stages {
			if _, ok := stages[name]; ok {
				dispatch = append(dispatch, name)
			}
		}
	}

	return &pipelineRuntime{
		StageOrder:    order,
		DispatchOrder: dispatch,
		Stages:        stages,
	}, nil
}

func readLearningBuildSpec() ([]byte, error) {
	if path := strings.TrimSpace(os.Getenv(learningBuildPipelineEnv)); path != "" {
		return os.ReadFile(path)
	}
	return learningBuildSpecFS.ReadFile("learning_build.yaml")
}

func validatePipelineSpec(spec *yamlPipelineSpec) error {
	if spec == nil {
		return errors.New("missing spec")
	}
	if strings.TrimSpace(spec.Pipeline) != "learning_build" {
		return fmt.Errorf("unexpected pipeline: %s", spec.Pipeline)
	}
	if len(spec.Stages) == 0 {
		return errors.New("no stages defined")
	}

	enabled := map[string]bool{}
	order := make([]string, 0, len(spec.Stages))
	for _, stage := range spec.Stages {
		name := strings.TrimSpace(stage.Name)
		if name == "" {
			return errors.New("stage name is required")
		}
		if _, exists := enabled[name]; exists {
			return fmt.Errorf("duplicate stage name: %s", name)
		}
		if stage.Enabled != nil && !*stage.Enabled {
			continue
		}
		enabled[name] = true
		order = append(order, name)
		if stage.JobType != "" && strings.TrimSpace(stage.JobType) == "" {
			return fmt.Errorf("stage %s: job_type is empty", name)
		}
	}

	orderIndex := map[string]int{}
	for i, name := range order {
		orderIndex[name] = i
	}

	for _, stage := range spec.Stages {
		name := strings.TrimSpace(stage.Name)
		if name == "" {
			continue
		}
		if stage.Enabled != nil && !*stage.Enabled {
			continue
		}
		for _, dep := range stage.DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				continue
			}
			if !enabled[dep] {
				return fmt.Errorf("stage %s: unknown dependency %s", name, dep)
			}
			if orderIndex[dep] > orderIndex[name] {
				return fmt.Errorf("stage %s: dependency %s appears after stage in order", name, dep)
			}
		}
	}

	if len(spec.Variants) > 0 {
		for key, variant := range spec.Variants {
			if strings.TrimSpace(key) == "" {
				return errors.New("variant name is required")
			}
			seen := map[string]bool{}
			for _, name := range variant.Stages {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				if !enabled[name] {
					return fmt.Errorf("variant %s: unknown stage %s", key, name)
				}
				if seen[name] {
					return fmt.Errorf("variant %s: duplicate stage %s", key, name)
				}
				seen[name] = true
			}
		}
	}

	return nil
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

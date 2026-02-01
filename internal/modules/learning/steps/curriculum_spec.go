package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
)

type CurriculumSpecV1 struct {
	SchemaVersion  int                       `json:"schema_version"`
	Goal           string                    `json:"goal"`
	Domain         string                    `json:"domain"`
	CoverageTarget string                    `json:"coverage_target"` // materials_only|survey|mastery
	Sections       []CurriculumSpecSectionV1 `json:"sections"`
}

type CurriculumSpecSectionV1 struct {
	Key          string                 `json:"key"`
	Title        string                 `json:"title"`
	Description  string                 `json:"description"`
	Competencies []CurriculumSpecItemV1 `json:"competencies"`
}

type CurriculumSpecItemV1 struct {
	Key        string   `json:"key"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Must       bool     `json:"must"`
	PrereqKeys []string `json:"prereq_keys"`
	Tags       []string `json:"tags"`
}

func (s CurriculumSpecV1) AllCompetencyKeys() []string {
	keys := make([]string, 0, 64)
	seen := map[string]bool{}
	for _, sec := range s.Sections {
		for _, c := range sec.Competencies {
			k := strings.TrimSpace(c.Key)
			if k == "" || seen[k] {
				continue
			}
			seen[k] = true
			keys = append(keys, k)
		}
	}
	return keys
}

func CurriculumSpecBriefJSONFromPathMeta(meta map[string]any, maxCompetenciesPerSection int) string {
	if meta == nil {
		return ""
	}
	ws := mapFromAny(meta["web_resources_seed"])
	if ws == nil {
		return ""
	}
	raw := ws["spec"]
	if raw == nil {
		return ""
	}
	var spec CurriculumSpecV1
	b, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	if json.Unmarshal(b, &spec) != nil || len(spec.Sections) == 0 {
		return ""
	}
	if maxCompetenciesPerSection <= 0 {
		maxCompetenciesPerSection = 6
	}

	briefSections := make([]map[string]any, 0, len(spec.Sections))
	for _, s := range spec.Sections {
		if strings.TrimSpace(s.Key) == "" {
			continue
		}
		top := make([]map[string]any, 0, min(maxCompetenciesPerSection, len(s.Competencies)))
		for i, c := range s.Competencies {
			if i >= maxCompetenciesPerSection {
				break
			}
			if strings.TrimSpace(c.Key) == "" {
				continue
			}
			top = append(top, map[string]any{
				"key":   strings.TrimSpace(c.Key),
				"title": strings.TrimSpace(c.Title),
				"must":  c.Must,
			})
		}
		briefSections = append(briefSections, map[string]any{
			"key":          strings.TrimSpace(s.Key),
			"title":        strings.TrimSpace(s.Title),
			"description":  strings.TrimSpace(s.Description),
			"competencies": top,
		})
	}
	if len(briefSections) == 0 {
		return ""
	}

	brief := map[string]any{
		"schema_version":  spec.SchemaVersion,
		"goal":            strings.TrimSpace(spec.Goal),
		"domain":          strings.TrimSpace(spec.Domain),
		"coverage_target": strings.TrimSpace(spec.CoverageTarget),
		"sections":        briefSections,
	}
	bb, err := json.Marshal(brief)
	if err != nil {
		return ""
	}
	return string(bb)
}

func InferCoverageTargetFromPrompt(prompt string) string {
	p := strings.ToLower(strings.TrimSpace(prompt))
	if p == "" {
		return "materials_only"
	}
	// Heuristic: treat "mastery" language as a strong signal for a comprehensive spec.
	masteryHints := []string{
		"master", "mastery", "from the ground up", "from scratch", "complete", "comprehensive",
		"everything", "no gaps", "beginner to advanced", "beginner-to-advanced", "zero to hero",
	}
	for _, h := range masteryHints {
		if strings.Contains(p, h) {
			return "mastery"
		}
	}
	return "survey"
}

func BuildCurriculumSpecV1(ctx context.Context, ai openai.Client, prompt string) (CurriculumSpecV1, error) {
	out := CurriculumSpecV1{}
	if ai == nil {
		return out, fmt.Errorf("BuildCurriculumSpecV1: ai required")
	}
	goal := strings.TrimSpace(prompt)
	if goal == "" {
		return out, fmt.Errorf("BuildCurriculumSpecV1: prompt required")
	}

	// Default target based on heuristics; the model can override if needed.
	defaultTarget := InferCoverageTargetFromPrompt(goal)

	system := strings.TrimSpace(`
ROLE: Curriculum architect.
TASK: Convert a learning goal into a competency map a generator can satisfy.
OUTPUT: Return ONLY JSON matching the schema (no extra keys).
RULES:
- Keep keys stable snake_case.
- The competency map must be comprehensive for the requested coverage_target.
- If coverage_target is "mastery", include all major prerequisites, core concepts, practice/project skills, and professional workflows.
- Prefer fewer, clearer competencies over many micro-topics; this is a coverage checklist, not a lesson plan.
- Avoid paywalled or proprietary assumptions.
`)

	user := fmt.Sprintf(`LEARNING_GOAL:
%s

DEFAULT_COVERAGE_TARGET: %s

Guidance:
- If the goal explicitly asks for complete mastery / from the ground up, set coverage_target="mastery".
- If the goal is broad but not explicitly mastery, set coverage_target="survey".
- If the goal is to only learn the provided materials, set coverage_target="materials_only".`,
		goal,
		defaultTarget,
	)

	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"schema_version": map[string]any{"type": "integer", "enum": []any{1}},
			"goal":           map[string]any{"type": "string"},
			"domain":         map[string]any{"type": "string"},
			"coverage_target": map[string]any{
				"type": "string",
				"enum": []any{"materials_only", "survey", "mastery"},
			},
			"sections": map[string]any{
				"type":     "array",
				"minItems": 1,
				"maxItems": 20,
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"key":         map[string]any{"type": "string"},
						"title":       map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"},
						"competencies": map[string]any{
							"type":     "array",
							"minItems": 1,
							"maxItems": 40,
							"items": map[string]any{
								"type":                 "object",
								"additionalProperties": false,
								"properties": map[string]any{
									"key":         map[string]any{"type": "string"},
									"title":       map[string]any{"type": "string"},
									"summary":     map[string]any{"type": "string"},
									"must":        map[string]any{"type": "boolean"},
									"prereq_keys": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
									"tags":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
								},
								"required": []string{"key", "title", "summary", "must", "prereq_keys", "tags"},
							},
						},
					},
					"required": []string{"key", "title", "description", "competencies"},
				},
			},
		},
		"required": []string{"schema_version", "goal", "domain", "coverage_target", "sections"},
	}

	obj, err := ai.GenerateJSON(ctx, system, user, "curriculum_spec_v1", schema)
	if err != nil {
		return out, err
	}
	raw, _ := json.Marshal(obj)
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	out.Goal = strings.TrimSpace(out.Goal)
	out.Domain = strings.TrimSpace(out.Domain)
	out.CoverageTarget = strings.TrimSpace(out.CoverageTarget)
	out.Sections = normalizeCurriculumSections(out.Sections)
	if len(out.Sections) == 0 {
		return out, fmt.Errorf("BuildCurriculumSpecV1: spec returned 0 valid sections")
	}
	if out.SchemaVersion == 0 {
		out.SchemaVersion = 1
	}
	if strings.TrimSpace(out.CoverageTarget) == "" {
		out.CoverageTarget = defaultTarget
	}
	if strings.TrimSpace(out.Goal) == "" {
		out.Goal = goal
	}
	return out, nil
}

var reSnake = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func normalizeCurriculumSections(in []CurriculumSpecSectionV1) []CurriculumSpecSectionV1 {
	out := make([]CurriculumSpecSectionV1, 0, len(in))
	seenSec := map[string]bool{}
	seenItem := map[string]bool{}
	for _, sec := range in {
		sec.Key = strings.TrimSpace(sec.Key)
		sec.Title = strings.TrimSpace(sec.Title)
		sec.Description = strings.TrimSpace(sec.Description)
		if sec.Key == "" || !reSnake.MatchString(sec.Key) || seenSec[sec.Key] {
			continue
		}
		seenSec[sec.Key] = true
		items := make([]CurriculumSpecItemV1, 0, len(sec.Competencies))
		for _, it := range sec.Competencies {
			it.Key = strings.TrimSpace(it.Key)
			it.Title = strings.TrimSpace(it.Title)
			it.Summary = strings.TrimSpace(it.Summary)
			it.PrereqKeys = dedupeStrings(it.PrereqKeys)
			it.Tags = dedupeStrings(it.Tags)
			if it.Key == "" || !reSnake.MatchString(it.Key) || seenItem[it.Key] {
				continue
			}
			seenItem[it.Key] = true
			if it.Title == "" {
				it.Title = it.Key
			}
			items = append(items, it)
		}
		if len(items) == 0 {
			continue
		}
		sec.Competencies = items
		if sec.Title == "" {
			sec.Title = sec.Key
		}
		out = append(out, sec)
	}
	return out
}

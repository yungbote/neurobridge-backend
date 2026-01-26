package steps

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

type teachingPatternHierarchy struct {
	SchemaVersion int                     `json:"schema_version"`
	Path          teachingPathPattern     `json:"path"`
	Modules       []teachingModulePattern `json:"modules"`
	Lessons       []teachingLessonPattern `json:"lessons"`
}

type teachingPathPattern struct {
	Sequencing    string `json:"sequencing"`
	Pedagogy      string `json:"pedagogy"`
	Mastery       string `json:"mastery"`
	Reinforcement string `json:"reinforcement"`
	Rationale     string `json:"rationale,omitempty"`
}

type teachingModulePattern struct {
	ModuleIndex int    `json:"module_index"`
	Sequencing  string `json:"sequencing"`
	Pedagogy    string `json:"pedagogy"`
	Assessment  string `json:"assessment"`
	ContentMix  string `json:"content_mix"`
	Rationale   string `json:"rationale,omitempty"`
}

type teachingLessonPattern struct {
	LessonIndex int    `json:"lesson_index"`
	Opening     string `json:"opening"`
	Core        string `json:"core"`
	Example     string `json:"example"`
	Visual      string `json:"visual"`
	Practice    string `json:"practice"`
	Closing     string `json:"closing"`
	Depth       string `json:"depth"`
	Engagement  string `json:"engagement"`
	Rationale   string `json:"rationale,omitempty"`
}

type patternSignals struct {
	ConceptCount     int            `json:"concept_count"`
	PrereqEdgeCount  int            `json:"prereq_edge_count"`
	MaxPrereqDepth   int            `json:"max_prereq_depth"`
	DocTemplateCount map[string]int `json:"doc_template_count"`
	NodeKindCount    map[string]int `json:"node_kind_count"`
}

var (
	pathSequencingPatterns = map[string]bool{
		"linear":        true,
		"spiral":        true,
		"modular":       true,
		"branching":     true,
		"layered":       true,
		"thematic":      true,
		"chronological": true,
		"whole_to_part": true,
		"part_to_whole": true,
		"concentric":    true,
		"comparative":   true,
		"problem_arc":   true,
	}
	pathPedagogyPatterns = map[string]bool{
		"direct_instruction": true,
		"project_based":      true,
		"problem_based":      true,
		"case_based":         true,
		"inquiry_based":      true,
		"discovery":          true,
		"narrative":          true,
		"apprenticeship":     true,
		"simulation":         true,
		"socratic":           true,
		"challenge_ladder":   true,
		"competency":         true,
	}
	pathMasteryPatterns = map[string]bool{
		"mastery_gated":       true,
		"soft_gated":          true,
		"ungated":             true,
		"diagnostic_adaptive": true,
		"xp_progression":      true,
	}
	pathReinforcementPatterns = map[string]bool{
		"spaced_review": true,
		"interleaved":   true,
		"cumulative":    true,
		"end_review":    true,
		"just_in_time":  true,
		"none":          true,
	}

	moduleSequencingPatterns = map[string]bool{
		"linear_lessons":    true,
		"sandwich":          true,
		"hub_spoke":         true,
		"funnel":            true,
		"expansion":         true,
		"spiral_mini":       true,
		"parallel":          true,
		"comparative_pairs": true,
		"chronological":     true,
		"simple_to_complex": true,
		"dependency_driven": true,
	}
	modulePedagogyPatterns = map[string]bool{
		"theory_then_practice": true,
		"practice_then_theory": true,
		"interleaved":          true,
		"immersion":            true,
		"survey":               true,
		"case_driven":          true,
		"project_milestone":    true,
		"problem_solution":     true,
		"skill_build":          true,
		"concept_build":        true,
		"question_driven":      true,
		"workshop":             true,
	}
	moduleAssessmentPatterns = map[string]bool{
		"quiz_per_lesson":     true,
		"module_end_only":     true,
		"pre_post":            true,
		"continuous_embedded": true,
		"diagnostic_entry":    true,
		"none":                true,
		"portfolio":           true,
		"peer_review":         true,
	}
	moduleContentMixPatterns = map[string]bool{
		"explanation_heavy": true,
		"activity_heavy":    true,
		"balanced":          true,
		"example_rich":      true,
		"visual_rich":       true,
		"discussion_rich":   true,
		"reading_heavy":     true,
		"multimedia_mix":    true,
	}

	lessonOpeningPatterns = map[string]bool{
		"hook_question":         true,
		"hook_problem":          true,
		"hook_story":            true,
		"hook_surprise":         true,
		"hook_relevance":        true,
		"hook_challenge":        true,
		"objectives_first":      true,
		"recap_prior":           true,
		"diagnostic_check":      true,
		"advance_organizer":     true,
		"direct_start":          true,
		"tldr_first":            true,
		"context_setting":       true,
		"misconception_address": true,
	}
	lessonCorePatterns = map[string]bool{
		"direct_instruction":        true,
		"worked_example":            true,
		"faded_example":             true,
		"multiple_examples":         true,
		"non_example":               true,
		"example_non_example_pairs": true,
		"analogy_based":             true,
		"metaphor_extended":         true,
		"compare_contrast":          true,
		"cause_effect":              true,
		"process_steps":             true,
		"classification":            true,
		"definition_elaboration":    true,
		"rule_then_apply":           true,
		"cases_then_rule":           true,
		"principle_illustration":    true,
		"concept_attainment":        true,
		"narrative_embed":           true,
		"dialogue_format":           true,
		"socratic_questioning":      true,
		"discovery_guided":          true,
		"simulation_walkthrough":    true,
		"demonstration":             true,
		"explanation_then_demo":     true,
		"demo_then_explanation":     true,
		"chunked_progressive":       true,
		"layered_depth":             true,
		"problem_solution_reveal":   true,
		"debate_format":             true,
		"q_and_a_format":            true,
		"interview_format":          true,
	}
	lessonExamplePatterns = map[string]bool{
		"single_canonical":   true,
		"multiple_varied":    true,
		"progression":        true,
		"edge_cases":         true,
		"real_world":         true,
		"abstract_formal":    true,
		"relatable_everyday": true,
		"domain_specific":    true,
		"counterexample":     true,
		"minimal_pairs":      true,
		"annotated":          true,
	}
	lessonVisualPatterns = map[string]bool{
		"text_only":           true,
		"diagram_supported":   true,
		"diagram_primary":     true,
		"dual_coded":          true,
		"sequential_visual":   true,
		"before_after":        true,
		"comparison_visual":   true,
		"infographic":         true,
		"flowchart":           true,
		"concept_map":         true,
		"timeline":            true,
		"table_matrix":        true,
		"annotated_image":     true,
		"animation_described": true,
	}
	lessonPracticePatterns = map[string]bool{
		"immediate":              true,
		"delayed_end":            true,
		"interleaved_throughout": true,
		"scaffolded":             true,
		"faded_support":          true,
		"massed":                 true,
		"varied":                 true,
		"retrieval":              true,
		"application":            true,
		"generation":             true,
		"error_analysis":         true,
		"self_explanation":       true,
		"teach_back":             true,
		"prediction":             true,
		"comparison":             true,
		"reflection":             true,
		"none":                   true,
	}
	lessonClosingPatterns = map[string]bool{
		"summary":             true,
		"single_takeaway":     true,
		"connection_forward":  true,
		"connection_backward": true,
		"connection_lateral":  true,
		"reflection_prompt":   true,
		"application_prompt":  true,
		"check_understanding": true,
		"open_question":       true,
		"call_to_action":      true,
		"cliff_hanger":        true,
		"consolidation":       true,
		"none":                true,
	}
	lessonDepthPatterns = map[string]bool{
		"eli5":       true,
		"concise":    true,
		"standard":   true,
		"thorough":   true,
		"exhaustive": true,
		"layered":    true,
		"adaptive":   true,
	}
	lessonEngagementPatterns = map[string]bool{
		"passive":                true,
		"active_embedded":        true,
		"active_end":             true,
		"gamified":               true,
		"challenge_framed":       true,
		"curiosity_driven":       true,
		"choice_driven":          true,
		"personalized_reference": true,
		"social_framed":          true,
		"timed":                  true,
		"untimed":                true,
	}
)

func parseTeachingPatternHierarchy(obj map[string]any) teachingPatternHierarchy {
	if obj == nil {
		return teachingPatternHierarchy{}
	}
	raw, _ := json.Marshal(obj)
	if len(raw) == 0 {
		return teachingPatternHierarchy{}
	}
	var out teachingPatternHierarchy
	_ = json.Unmarshal(raw, &out)
	return out
}

func patternSignalsForPath(nodes []pathStructureNode, edges []*types.ConceptEdge) patternSignals {
	docCounts := map[string]int{}
	kindCounts := map[string]int{}
	for _, n := range nodes {
		docCounts[strings.TrimSpace(strings.ToLower(n.DocTemplate))]++
		kindCounts[strings.TrimSpace(strings.ToLower(n.NodeKind))]++
	}
	prereqEdges := make([]*types.ConceptEdge, 0)
	for _, e := range edges {
		if e == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(e.EdgeType), "prereq") {
			prereqEdges = append(prereqEdges, e)
		}
	}
	return patternSignals{
		ConceptCount:     countConceptsFromNodes(nodes),
		PrereqEdgeCount:  len(prereqEdges),
		MaxPrereqDepth:   maxPrereqDepth(prereqEdges),
		DocTemplateCount: docCounts,
		NodeKindCount:    kindCounts,
	}
}

func countConceptsFromNodes(nodes []pathStructureNode) int {
	seen := map[string]bool{}
	for _, n := range nodes {
		for _, k := range n.ConceptKeys {
			k = strings.TrimSpace(strings.ToLower(k))
			if k != "" {
				seen[k] = true
			}
		}
	}
	return len(seen)
}

func maxPrereqDepth(edges []*types.ConceptEdge) int {
	if len(edges) == 0 {
		return 0
	}
	adj := map[uuid.UUID][]uuid.UUID{}
	for _, e := range edges {
		if e == nil {
			continue
		}
		if e.FromConceptID == uuid.Nil || e.ToConceptID == uuid.Nil {
			continue
		}
		adj[e.FromConceptID] = append(adj[e.FromConceptID], e.ToConceptID)
	}
	visited := map[uuid.UUID]bool{}
	stack := map[uuid.UUID]bool{}
	memo := map[uuid.UUID]int{}
	var dfs func(uuid.UUID) int
	dfs = func(id uuid.UUID) int {
		if v, ok := memo[id]; ok {
			return v
		}
		if stack[id] {
			return 0
		}
		stack[id] = true
		best := 0
		for _, nxt := range adj[id] {
			if nxt == uuid.Nil {
				continue
			}
			d := dfs(nxt)
			if d > best {
				best = d
			}
		}
		stack[id] = false
		visited[id] = true
		memo[id] = best + 1
		return memo[id]
	}
	best := 0
	for id := range adj {
		d := dfs(id)
		if d > best {
			best = d
		}
	}
	if best > 0 {
		return best - 1
	}
	return 0
}

func normalizeTeachingPatternHierarchy(h teachingPatternHierarchy, nodes []pathStructureNode, edges []*types.ConceptEdge, userProfileDoc string) teachingPatternHierarchy {
	signals := patternSignalsForPath(nodes, edges)
	pathPattern := normalizePathPattern(h.Path, signals, userProfileDoc)

	moduleIndices := map[int]bool{}
	lessonIndices := map[int]bool{}
	parentByIndex := map[int]*int{}
	for _, n := range nodes {
		if n.Index <= 0 {
			continue
		}
		parentByIndex[n.Index] = n.ParentIndex
		if strings.EqualFold(strings.TrimSpace(n.NodeKind), "module") {
			moduleIndices[n.Index] = true
		} else {
			lessonIndices[n.Index] = true
		}
	}

	moduleByIndex := map[int]teachingModulePattern{}
	for _, m := range h.Modules {
		if m.ModuleIndex <= 0 || !moduleIndices[m.ModuleIndex] {
			continue
		}
		moduleByIndex[m.ModuleIndex] = normalizeModulePattern(m, pathPattern)
	}
	for idx := range moduleIndices {
		if _, ok := moduleByIndex[idx]; ok {
			continue
		}
		moduleByIndex[idx] = defaultModulePattern(idx, pathPattern, signals)
	}

	lessonByIndex := map[int]teachingLessonPattern{}
	for _, l := range h.Lessons {
		if l.LessonIndex <= 0 || !lessonIndices[l.LessonIndex] {
			continue
		}
		moduleIdx := parentModuleIndexForNode(l.LessonIndex, parentByIndex, moduleIndices)
		mod := moduleByIndex[moduleIdx]
		lessonByIndex[l.LessonIndex] = normalizeLessonPattern(l, mod, pathPattern)
	}

	pathLessonOrder := lessonOrderIndices(nodes)
	firstLesson := 0
	lastLesson := 0
	if len(pathLessonOrder) > 0 {
		firstLesson = pathLessonOrder[0]
		lastLesson = pathLessonOrder[len(pathLessonOrder)-1]
	}

	moduleLessons := lessonsByModule(nodes, moduleIndices)
	for idx := range lessonIndices {
		if _, ok := lessonByIndex[idx]; ok {
			continue
		}
		moduleIdx := parentModuleIndexForNode(idx, parentByIndex, moduleIndices)
		mod := moduleByIndex[moduleIdx]
		lp := defaultLessonPattern(idx, mod, pathPattern)
		if idx == firstLesson {
			lp.Opening = "hook_relevance"
		}
		if idx == lastLesson {
			lp.Closing = "summary"
		}
		if kids := moduleLessons[moduleIdx]; len(kids) > 0 {
			if idx == kids[0] {
				lp.Opening = "advance_organizer"
			}
			if idx == kids[len(kids)-1] {
				lp.Closing = "connection_forward"
			}
		}
		lessonByIndex[idx] = lp
	}

	modules := make([]teachingModulePattern, 0, len(moduleByIndex))
	for _, idx := range sortedKeys(moduleByIndex) {
		modules = append(modules, moduleByIndex[idx])
	}
	lessons := make([]teachingLessonPattern, 0, len(lessonByIndex))
	for _, idx := range sortedKeys(lessonByIndex) {
		lessons = append(lessons, lessonByIndex[idx])
	}

	return teachingPatternHierarchy{
		SchemaVersion: 1,
		Path:          pathPattern,
		Modules:       modules,
		Lessons:       lessons,
	}
}

func normalizePathPattern(in teachingPathPattern, signals patternSignals, userProfileDoc string) teachingPathPattern {
	out := teachingPathPattern{
		Sequencing:    normalizeEnum(in.Sequencing, pathSequencingPatterns, ""),
		Pedagogy:      normalizeEnum(in.Pedagogy, pathPedagogyPatterns, ""),
		Mastery:       normalizeEnum(in.Mastery, pathMasteryPatterns, ""),
		Reinforcement: normalizeEnum(in.Reinforcement, pathReinforcementPatterns, ""),
		Rationale:     strings.TrimSpace(in.Rationale),
	}
	if out.Sequencing == "" {
		out.Sequencing = defaultPathSequencing(signals)
	}
	if out.Pedagogy == "" {
		out.Pedagogy = defaultPathPedagogy(signals)
	}
	if out.Mastery == "" {
		out.Mastery = defaultPathMastery(userProfileDoc)
	}
	if out.Reinforcement == "" {
		out.Reinforcement = defaultPathReinforcement(signals)
	}
	return out
}

func defaultPathSequencing(signals patternSignals) string {
	if signals.MaxPrereqDepth >= 3 {
		return "linear"
	}
	if signals.ConceptCount > 40 {
		return "spiral"
	}
	if signals.PrereqEdgeCount == 0 {
		return "modular"
	}
	return "layered"
}

func defaultPathPedagogy(signals patternSignals) string {
	if signals.DocTemplateCount["project"] > 0 {
		return "project_based"
	}
	if signals.DocTemplateCount["practice"] > 0 {
		return "problem_based"
	}
	return "direct_instruction"
}

func defaultPathMastery(userProfileDoc string) string {
	txt := strings.ToLower(userProfileDoc)
	switch {
	case strings.Contains(txt, "certification"), strings.Contains(txt, "exam"):
		return "mastery_gated"
	case strings.Contains(txt, "overview"), strings.Contains(txt, "skim"):
		return "ungated"
	default:
		return "soft_gated"
	}
}

func defaultPathReinforcement(signals patternSignals) string {
	if signals.NodeKindCount["review"] > 1 {
		return "spaced_review"
	}
	if signals.NodeKindCount["review"] == 1 {
		return "end_review"
	}
	return "none"
}

func normalizeModulePattern(in teachingModulePattern, pathPattern teachingPathPattern) teachingModulePattern {
	out := teachingModulePattern{
		ModuleIndex: in.ModuleIndex,
		Sequencing:  normalizeEnum(in.Sequencing, moduleSequencingPatterns, ""),
		Pedagogy:    normalizeEnum(in.Pedagogy, modulePedagogyPatterns, ""),
		Assessment:  normalizeEnum(in.Assessment, moduleAssessmentPatterns, ""),
		ContentMix:  normalizeEnum(in.ContentMix, moduleContentMixPatterns, ""),
		Rationale:   strings.TrimSpace(in.Rationale),
	}
	if out.Sequencing == "" {
		out.Sequencing = defaultModuleSequencing(pathPattern)
	}
	if out.Pedagogy == "" {
		out.Pedagogy = defaultModulePedagogy(pathPattern)
	}
	if out.Assessment == "" {
		out.Assessment = defaultModuleAssessment(pathPattern)
	}
	if out.ContentMix == "" {
		out.ContentMix = "balanced"
	}
	out = enforcePathModuleConstraints(out, pathPattern)
	return out
}

func defaultModulePattern(idx int, pathPattern teachingPathPattern, signals patternSignals) teachingModulePattern {
	return normalizeModulePattern(teachingModulePattern{ModuleIndex: idx}, pathPattern)
}

func defaultModuleSequencing(pathPattern teachingPathPattern) string {
	switch pathPattern.Sequencing {
	case "linear":
		return "linear_lessons"
	case "spiral":
		return "spiral_mini"
	case "modular":
		return "hub_spoke"
	case "chronological":
		return "chronological"
	default:
		return "linear_lessons"
	}
}

func defaultModulePedagogy(pathPattern teachingPathPattern) string {
	switch pathPattern.Pedagogy {
	case "project_based":
		return "project_milestone"
	case "problem_based":
		return "problem_solution"
	case "case_based":
		return "case_driven"
	case "discovery":
		return "practice_then_theory"
	case "direct_instruction":
		return "theory_then_practice"
	case "narrative":
		return "question_driven"
	default:
		return "theory_then_practice"
	}
}

func defaultModuleAssessment(pathPattern teachingPathPattern) string {
	switch pathPattern.Mastery {
	case "mastery_gated":
		return "quiz_per_lesson"
	case "diagnostic_adaptive":
		return "pre_post"
	default:
		return "continuous_embedded"
	}
}

func enforcePathModuleConstraints(mod teachingModulePattern, pathPattern teachingPathPattern) teachingModulePattern {
	if allowed, ok := pathSequencingToModuleSequencing[pathPattern.Sequencing]; ok {
		if !allowed[mod.Sequencing] {
			mod.Sequencing = firstAllowed(allowed, mod.Sequencing)
		}
	}
	if allowed, ok := pathPedagogyToModulePedagogy[pathPattern.Pedagogy]; ok {
		if !allowed[mod.Pedagogy] {
			mod.Pedagogy = firstAllowed(allowed, mod.Pedagogy)
		}
	}
	if allowed, ok := pathPedagogyToModuleSequencing[pathPattern.Pedagogy]; ok {
		if !allowed[mod.Sequencing] {
			mod.Sequencing = firstAllowed(allowed, mod.Sequencing)
		}
	}
	return mod
}

var pathSequencingToModuleSequencing = map[string]map[string]bool{
	"linear": {
		"linear_lessons":    true,
		"sandwich":          true,
		"funnel":            true,
		"simple_to_complex": true,
	},
	"spiral": {
		"spiral_mini":    true,
		"expansion":      true,
		"linear_lessons": true,
	},
	"modular": {
		"sandwich":  true,
		"hub_spoke": true,
	},
	"chronological": {
		"chronological":  true,
		"linear_lessons": true,
	},
}

var pathPedagogyToModulePedagogy = map[string]map[string]bool{
	"project_based": {
		"project_milestone": true,
		"workshop":          true,
		"skill_build":       true,
	},
	"problem_based": {
		"problem_solution": true,
		"case_driven":      true,
		"question_driven":  true,
	},
	"case_based": {
		"case_driven": true,
	},
	"discovery": {
		"practice_then_theory": true,
		"question_driven":      true,
	},
	"direct_instruction": {
		"theory_then_practice": true,
	},
	"narrative": {
		"question_driven": true,
	},
}

var pathPedagogyToModuleSequencing = map[string]map[string]bool{
	"case_based": {
		"comparative_pairs": true,
	},
	"direct_instruction": {
		"linear_lessons": true,
	},
	"narrative": {
		"chronological":  true,
		"linear_lessons": true,
	},
}

func normalizeLessonPattern(in teachingLessonPattern, mod teachingModulePattern, pathPattern teachingPathPattern) teachingLessonPattern {
	out := teachingLessonPattern{
		LessonIndex: in.LessonIndex,
		Opening:     normalizeEnum(in.Opening, lessonOpeningPatterns, ""),
		Core:        normalizeEnum(in.Core, lessonCorePatterns, ""),
		Example:     normalizeEnum(in.Example, lessonExamplePatterns, ""),
		Visual:      normalizeEnum(in.Visual, lessonVisualPatterns, ""),
		Practice:    normalizeEnum(in.Practice, lessonPracticePatterns, ""),
		Closing:     normalizeEnum(in.Closing, lessonClosingPatterns, ""),
		Depth:       normalizeEnum(in.Depth, lessonDepthPatterns, ""),
		Engagement:  normalizeEnum(in.Engagement, lessonEngagementPatterns, ""),
		Rationale:   strings.TrimSpace(in.Rationale),
	}
	if out.Opening == "" {
		out.Opening = "objectives_first"
	}
	if out.Core == "" {
		out.Core = "direct_instruction"
	}
	if out.Example == "" {
		out.Example = "multiple_varied"
	}
	if out.Visual == "" {
		out.Visual = "diagram_supported"
	}
	if out.Practice == "" {
		out.Practice = "interleaved_throughout"
	}
	if out.Closing == "" {
		out.Closing = "connection_forward"
	}
	if out.Depth == "" {
		out.Depth = "standard"
	}
	if out.Engagement == "" {
		out.Engagement = "active_embedded"
	}
	out = enforceModuleLessonConstraints(out, mod)
	return out
}

func defaultLessonPattern(idx int, mod teachingModulePattern, pathPattern teachingPathPattern) teachingLessonPattern {
	return normalizeLessonPattern(teachingLessonPattern{LessonIndex: idx}, mod, pathPattern)
}

func enforceModuleLessonConstraints(lesson teachingLessonPattern, mod teachingModulePattern) teachingLessonPattern {
	if allowed, ok := modulePedagogyToLessonOpenings[mod.Pedagogy]; ok {
		if !allowed[lesson.Opening] {
			lesson.Opening = firstAllowed(allowed, lesson.Opening)
		}
	}
	if allowed, ok := modulePedagogyToLessonCores[mod.Pedagogy]; ok {
		if !allowed[lesson.Core] {
			lesson.Core = firstAllowed(allowed, lesson.Core)
		}
	}
	if allowed, ok := modulePedagogyToLessonPractice[mod.Pedagogy]; ok {
		if !allowed[lesson.Practice] {
			lesson.Practice = firstAllowed(allowed, lesson.Practice)
		}
	}
	return lesson
}

var modulePedagogyToLessonOpenings = map[string]map[string]bool{
	"theory_then_practice": {"objectives_first": true, "recap_prior": true},
	"practice_then_theory": {"hook_challenge": true, "hook_problem": true},
	"project_milestone":    {"objectives_first": true, "hook_problem": true},
	"case_driven":          {"hook_story": true, "context_setting": true},
	"skill_build":          {"objectives_first": true, "direct_start": true},
	"concept_build":        {"hook_question": true, "recap_prior": true},
	"workshop":             {"direct_start": true, "hook_challenge": true},
	"survey":               {"advance_organizer": true, "tldr_first": true},
}

var modulePedagogyToLessonCores = map[string]map[string]bool{
	"theory_then_practice": {"direct_instruction": true, "worked_example": true},
	"practice_then_theory": {"discovery_guided": true, "problem_solution_reveal": true},
	"project_milestone":    {"demonstration": true, "worked_example": true},
	"case_driven":          {"narrative_embed": true, "socratic_questioning": true},
	"skill_build":          {"process_steps": true, "worked_example": true, "faded_example": true},
	"concept_build":        {"direct_instruction": true, "compare_contrast": true},
	"workshop":             {"demonstration": true, "worked_example": true},
	"survey":               {"direct_instruction": true, "chunked_progressive": true},
}

var modulePedagogyToLessonPractice = map[string]map[string]bool{
	"theory_then_practice": {"delayed_end": true, "massed": true},
	"practice_then_theory": {"immediate": true, "interleaved_throughout": true},
	"project_milestone":    {"application": true, "generation": true},
	"case_driven":          {"application": true, "reflection": true},
	"skill_build":          {"scaffolded": true, "massed": true},
	"concept_build":        {"retrieval": true, "self_explanation": true},
	"workshop":             {"immediate": true, "scaffolded": true},
	"survey":               {"none": true},
}

func normalizeEnum(value string, allowed map[string]bool, fallback string) string {
	v := strings.TrimSpace(strings.ToLower(value))
	if v != "" && allowed[v] {
		return v
	}
	if fallback != "" && allowed[fallback] {
		return fallback
	}
	return ""
}

func firstAllowed(allowed map[string]bool, fallback string) string {
	if fallback != "" && allowed[fallback] {
		return fallback
	}
	keys := make([]string, 0, len(allowed))
	for k := range allowed {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

func parentModuleIndexForNode(idx int, parentByIndex map[int]*int, moduleIndices map[int]bool) int {
	curr := idx
	seen := map[int]bool{}
	for curr > 0 && !seen[curr] {
		seen[curr] = true
		if moduleIndices[curr] {
			return curr
		}
		p := parentByIndex[curr]
		if p == nil || *p <= 0 {
			break
		}
		curr = *p
	}
	return 0
}

func lessonOrderIndices(nodes []pathStructureNode) []int {
	out := make([]int, 0, len(nodes))
	for _, n := range nodes {
		if n.Index <= 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(n.NodeKind), "module") {
			continue
		}
		out = append(out, n.Index)
	}
	sort.Ints(out)
	return out
}

func lessonsByModule(nodes []pathStructureNode, moduleIndices map[int]bool) map[int][]int {
	parentByIndex := map[int]*int{}
	for _, n := range nodes {
		parentByIndex[n.Index] = n.ParentIndex
	}
	out := map[int][]int{}
	for _, n := range nodes {
		if n.Index <= 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(n.NodeKind), "module") {
			continue
		}
		mid := parentModuleIndexForNode(n.Index, parentByIndex, moduleIndices)
		out[mid] = append(out[mid], n.Index)
	}
	for mid, ids := range out {
		sort.Ints(ids)
		out[mid] = ids
	}
	return out
}

func sortedKeys[T any](m map[int]T) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

func teachingPatternHierarchyJSON(h teachingPatternHierarchy) string {
	b, _ := json.Marshal(h)
	if len(b) == 0 || string(b) == "null" {
		return ""
	}
	return string(b)
}

func patternContextJSONForNode(node *types.PathNode, nodes map[uuid.UUID]*types.PathNode, hierarchy teachingPatternHierarchy) string {
	if node == nil || node.ID == uuid.Nil {
		return ""
	}
	if hierarchy.SchemaVersion == 0 {
		return ""
	}
	meta := map[string]any{}
	if len(node.Metadata) > 0 && string(node.Metadata) != "null" {
		_ = json.Unmarshal(node.Metadata, &meta)
	}
	nodeKind := strings.ToLower(strings.TrimSpace(stringFromAny(meta["node_kind"])))
	if p, ok := meta["patterns"]; ok && p != nil {
		if b, err := json.Marshal(p); err == nil && len(b) > 0 && string(b) != "null" {
			return string(b)
		}
	}

	moduleByIndex := map[int]teachingModulePattern{}
	lessonByIndex := map[int]teachingLessonPattern{}
	for _, m := range hierarchy.Modules {
		moduleByIndex[m.ModuleIndex] = m
	}
	for _, l := range hierarchy.Lessons {
		lessonByIndex[l.LessonIndex] = l
	}

	// Build index -> parent index map and module indices from nodes.
	parentByIndex := map[int]*int{}
	moduleIndices := map[int]bool{}
	for _, n := range nodes {
		if n == nil || n.ID == uuid.Nil {
			continue
		}
		if n.ParentNodeID != nil && *n.ParentNodeID != uuid.Nil {
			if parentNode := nodes[*n.ParentNodeID]; parentNode != nil {
				idx := parentNode.Index
				parentByIndex[n.Index] = &idx
			}
		}
		if len(n.Metadata) > 0 && string(n.Metadata) != "null" {
			nmeta := map[string]any{}
			if json.Unmarshal(n.Metadata, &nmeta) == nil {
				if strings.EqualFold(strings.TrimSpace(stringFromAny(nmeta["node_kind"])), "module") {
					moduleIndices[n.Index] = true
				}
			}
		}
	}

	moduleIdx := parentModuleIndexForNode(node.Index, parentByIndex, moduleIndices)
	out := map[string]any{
		"path":         hierarchy.Path,
		"module_index": moduleIdx,
	}
	if moduleIdx > 0 {
		if mod, ok := moduleByIndex[moduleIdx]; ok {
			out["module"] = mod
		}
	}
	if nodeKind != "module" {
		out["lesson_index"] = node.Index
		if lesson, ok := lessonByIndex[node.Index]; ok {
			out["lesson"] = lesson
		}
	}
	b, _ := json.Marshal(out)
	if len(b) == 0 || string(b) == "null" {
		return ""
	}
	return string(b)
}

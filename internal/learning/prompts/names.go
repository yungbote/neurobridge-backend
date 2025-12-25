package prompts

type PromptName string

const (
	// Library + Concepts
	PromptMaterialSetSummary PromptName = "material_set_summary"
	PromptConceptInventory   PromptName = "concept_inventory"
	PromptConceptEdges       PromptName = "concept_edges"
	PromptConceptClusters    PromptName = "concept_clusters"

	// Personalization
	PromptUserProfileDoc           PromptName = "user_profile_doc"
	PromptPopulationCohortFeatures PromptName = "population_cohort_features"

	// Coherence + Planning
	PromptTeachingPatterns              PromptName = "teaching_patterns"
	PromptDiagnosticGate                PromptName = "diagnostic_gate"
	PromptPathCharter                   PromptName = "path_charter"
	PromptPathStructure                 PromptName = "path_structure"
	PromptNodeRepresentationPlan        PromptName = "node_representation_plan"
	PromptChainRepresentationCandidates PromptName = "chain_representation_candidate"
	PromptOverrideResolution            PromptName = "override_resolution"

	// Retrieval + Reuse
	PromptRetrievalSpec PromptName = "retrieval_spec"

	// Realization (generation)
	PromptActivityContent  PromptName = "activity_content"
	PromptActivityVariant  PromptName = "activity_variant"
	PromptDiagramSpec      PromptName = "diagram_spec"
	PromptVideoStoryboard  PromptName = "video_storyboard"
	PromptQuizFromActivity PromptName = "quiz_from_activity"

	// Audits
	PromptCoverageAndCoheranceAudit PromptName = "coverage_and_coherance_audit"
	PromptDecisionTraceExplanation  PromptName = "decision_trace_explanation"
)

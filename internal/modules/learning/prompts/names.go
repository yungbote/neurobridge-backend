package prompts

type PromptName string

const (
	// Library + Concepts
	PromptFileSignatureBuild    PromptName = "file_signature_build"
	PromptMaterialSetSummary    PromptName = "material_set_summary"
	PromptConceptInventory      PromptName = "concept_inventory"
	PromptConceptInventoryDelta PromptName = "concept_inventory_delta"
	PromptConceptEdges          PromptName = "concept_edges"
	PromptMaterialKGExtract     PromptName = "material_kg_extract"
	PromptConceptClusters       PromptName = "concept_clusters"
	PromptLibraryTaxonomyRoute  PromptName = "library_taxonomy_route"
	PromptLibraryTaxonomyRefine PromptName = "library_taxonomy_refine"

	// Personalization
	PromptUserProfileDoc           PromptName = "user_profile_doc"
	PromptPopulationCohortFeatures PromptName = "population_cohort_features"

	// Coherence + Planning
	PromptTeachingPatterns              PromptName = "teaching_patterns"
	PromptDiagnosticGate                PromptName = "diagnostic_gate"
	PromptPathCharter                   PromptName = "path_charter"
	PromptPathStructure                 PromptName = "path_structure"
	PromptTeachingPatternHierarchy      PromptName = "teaching_pattern_hierarchy"
	PromptAssumedKnowledge              PromptName = "assumed_knowledge"
	PromptConceptAlignment              PromptName = "concept_alignment"
	PromptFormulaExtraction             PromptName = "formula_extraction"
	PromptStyleManifest                 PromptName = "style_manifest"
	PromptPathNarrativePlan             PromptName = "path_narrative_plan"
	PromptNodeNarrativePlan             PromptName = "node_narrative_plan"
	PromptMediaRank                     PromptName = "media_rank"
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

package prompts

// Input is a superset of all fields any prompt might need.
// Missing fields render empty strings (templates use missingkey=zero).
type Input struct {
	// Common grounding excerpts (chunk_id lines)
	Excerpts string
	// Per-file context (for signature build)
	FileInfoJSON    string
	OutlineHintJSON string
	// Material signal extraction
	MaterialContextJSON     string
	MaterialIntentJSON      string
	ChunkBatchJSON          string
	MaterialIntentsJSON     string
	MaterialSetIntentJSON   string
	MaterialSetCoverageJSON string
	MaterialSetEdgesJSON    string
	UserSetsJSON            string
	// Material set summary
	BundleExcerpt string
	// Optional global goal context (from path_intake)
	PathIntentMD string
	// Optional style manifest from path charter
	PathStyleJSON string
	// Concepts
	ConceptsJSON         string
	EdgesJSON            string
	CrossDocSectionsJSON string
	PathNarrativeJSON    string
	StyleManifestJSON    string
	NodeNarrativeJSON    string
	MediaRankJSON        string
	NodeDocJSON          string
	PathStructureJSON    string
	PatternHierarchyJSON string
	// User profile
	UserFactsJSON        string
	RecentEventsSummary  string
	UserProfileDoc       string
	TeachingPatternsJSON string
	// Knowledge graph (user mastery / exposure signals)
	UserKnowledgeJSON string
	// Planning context
	PathCharterJSON    string
	CurriculumSpecJSON string
	PatternSignalsJSON string
	PatternContextJSON string
	// Optional: multi-path planning context (from path_intake)
	MaterialPathsJSON string
	// Node / Chain planning
	NodeIndex           int
	NodeTitle           string
	NodeGoal            string
	NodeConceptKeysCSV  string
	ChainKey            string
	ChainConceptKeysCSV string
	// Retrieval
	TargetsJSON string // list of node/slot targets
	CohortHints string
	// Activity Realization
	ActivityKind          string
	ActivityTitle         string
	ConceptKeysCSV        string
	ActivityExcerpts      string
	AssetsJSON            string
	CanonicalActivityJSON string
	Variant               string
	// Quiz
	ActivityContentMD string
	CitationsCSV      string
	// Decision Explanation
	DecisionType          string
	InputsJSON            string
	CandidatesJSON        string
	ChosenJSON            string
	FormulaCandidatesJSON string
	// Audit Inputs
	PathNodesJSON  string
	NodePlansJSON  string
	ChainPlansJSON string
	ActivitiesJSON string
	VariantsJSON   string

	// Library taxonomy (path organization)
	TaxonomyFacet           string
	TaxonomyCandidatesJSON  string
	TaxonomyPathSummaryJSON string
	TaxonomyConstraintsJSON string
}

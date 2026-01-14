package prompts

// Input is a superset of all fields any prompt might need.
// Missing fields render empty strings (templates use missingkey=zero).
type Input struct {
	// Common grounding excerpts (chunk_id lines)
	Excerpts string
	// Material set summary
	BundleExcerpt string
	// Optional global goal context (from path_intake)
	PathIntentMD string
	// Concepts
	ConceptsJSON string
	EdgesJSON    string
	// User profile
	UserFactsJSON        string
	RecentEventsSummary  string
	UserProfileDoc       string
	TeachingPatternsJSON string
	// Knowledge graph (user mastery / exposure signals)
	UserKnowledgeJSON string
	// Planning context
	PathCharterJSON    string
	PathStructureJSON  string
	CurriculumSpecJSON string
	// Optional: multi-track / subpath planning context (from path_intake)
	MaterialTracksJSON string
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
	DecisionType   string
	InputsJSON     string
	CandidatesJSON string
	ChosenJSON     string
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

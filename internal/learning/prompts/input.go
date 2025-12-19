package prompts

// Input is a superset of all fields any prompt might need.
// Missing fields render empty strings (templates use missingkey=zero).
type Input struct {
	// Common grounding excerpts (chunk_id lines)
	Excerpts							string
	// Material set summary
	BundleExcerpt					string
	// Concepts
	ConceptsJSON					string
	EdgesJSON							string
	// User profile
	UserFactsJSON					string
	RecentEventsSummary		string
	UserProfileDoc				string
	// Planning context
	PathCharterJSON				string
	PathStructureJSON			string
	// Node / Chain planning
	NodeIndex							int
	NodeTitle							string
	NodeGoal							string
	NodeConceptKeysCSV		string
	ChainKey							string
	ChainConceptKeysCSV		string
	// Retrieval
	TargetsJSON						string		// list of node/slot targets
	CohortHints						string
	// Activity Realization
	ActivityKind					string
	ActivityTitle					string
	ConceptKeysCSV				string
	ActivityExcerpts			string
	CanonicalActivityJSON	string
	Variant								string
	// Quiz
	ActivityContentMD			string
	CitationsCSV					string
	// Decision Explanation
	DecisionType					string
	InputsJSON						string
	CandidatesJSON				string
	ChosenJSON						string
	// Audit Inputs
	PathNodesJSON					string
	NodePlansJSON					string
	ChainPlansJSON				string
	ActivitiesJSON				string
	VariantsJSON					string
}











package steps

const (
	ScopeThread = "thread"
	ScopePath   = "path"
	ScopeUser   = "user"
)

const (
	DocTypeMessageChunk = "message_chunk"
	// DocTypeMessageRaw is used for SQL-only fallback retrieval (not persisted as chat_doc).
	DocTypeMessageRaw = "message_raw"
	DocTypeSummary    = "summary"
	DocTypeMemory     = "memory"
	DocTypeEntity     = "entity"
	DocTypeClaim      = "claim"

	// Path-scoped derived docs (from canonical learning tables).
	DocTypePathOverview  = "path_overview"
	DocTypePathNode      = "path_node"
	DocTypePathConcepts  = "path_concepts"
	DocTypePathMaterials = "path_materials"
	DocTypePathUnitDoc   = "path_unit_doc"
	DocTypePathUnitBlock = "path_unit_block"
)

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

const (
	MessageStatusSent      = "sent"
	MessageStatusStreaming = "streaming"
	MessageStatusDone      = "done"
	MessageStatusError     = "error"
)

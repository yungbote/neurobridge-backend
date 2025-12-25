package steps

import "fmt"

func promptContextualizeChunk(threadTitle string, role string, chunkText string, recent string) (system string, user string) {
	system = `You produce contextual retrieval text to improve search for a future query.
Return ONLY JSON matching the schema. Keep it concise, factual, and retrieval-friendly.`
	user = "Thread title: " + threadTitle + "\n" +
		"Role: " + role + "\n" +
		"Recent context:\n" + recent + "\n\n" +
		"Chunk:\n" + chunkText + "\n\n" +
		"Task: produce a contextualized version of the chunk that stands alone for retrieval. Include key entities, goals, constraints, decisions, and identifiers."
	return system, user
}

func schemaContextualizeChunk() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"contextual_text": map[string]any{"type": "string"},
			"keywords": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"salience": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
		},
		"required":             []any{"contextual_text", "keywords", "salience"},
		"additionalProperties": false,
	}
}

func promptContextualizeQuery(threadSummary string, recent string, query string) (system string, user string) {
	system = `Rewrite the user query into a standalone retrieval query that can be embedded and used for search.
Return ONLY JSON matching the schema. Be concise and preserve identifiers.`
	user = "Thread summary:\n" + threadSummary + "\n\nRecent messages:\n" + recent + "\n\nUser query:\n" + query + "\n\nTask: rewrite the query so it stands alone and includes any needed context."
	return system, user
}

func schemaContextualizeQuery() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"contextual_query": map[string]any{"type": "string"},
		},
		"required":             []any{"contextual_query"},
		"additionalProperties": false,
	}
}

func promptRerank(query string, items string) (system string, user string) {
	system = `You are a ranking model. Score each item for relevance to the query.
Return ONLY JSON matching the schema. Use scores 0-100 (higher is more relevant).
Be strict: only give high scores if it directly helps answer the query.`
	user = "Query:\n" + query + "\n\nItems:\n" + items
	return system, user
}

func schemaRerank() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"results": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":    map[string]any{"type": "string"},
						"score": map[string]any{"type": "number", "minimum": 0, "maximum": 100},
					},
					"required":             []any{"id", "score"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []any{"results"},
		"additionalProperties": false,
	}
}

func promptMemoryExtract(threadTitle string, window string) (system string, user string) {
	system = `Extract durable memory items. Only store things that are stable and useful later.
Return ONLY JSON matching the schema. Prefer fewer, higher-quality items.
Do NOT store transient chit-chat. Include evidence seqs.`
	user = "Thread title: " + threadTitle + "\n\nWindow:\n" + window
	return system, user
}

func schemaMemoryExtract() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"items": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"kind":  map[string]any{"type": "string", "enum": []any{"fact", "preference", "decision", "todo"}},
						"scope": map[string]any{"type": "string", "enum": []any{"thread", "path", "user"}},
						"key":   map[string]any{"type": "string"},
						"value": map[string]any{"type": "string"},
						"confidence": map[string]any{
							"type":    "number",
							"minimum": 0,
							"maximum": 1,
						},
						"evidence_seqs": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "number"},
						},
					},
					"required":             []any{"kind", "scope", "key", "value", "confidence", "evidence_seqs"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []any{"items"},
		"additionalProperties": false,
	}
}

func promptSummarizeNode(level int, childSummaries string) (system string, user string) {
	system = `You build a hierarchical summary node for long conversations.
Return ONLY JSON matching the schema. Use markdown bullets, preserve identifiers, decisions, TODOs, and open questions.`
	user = fmt.Sprintf("Level: %d\n\nChild summaries:\n%s", level, childSummaries)
	return system, user
}

func schemaSummarizeNode() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary_md": map[string]any{"type": "string"},
		},
		"required":             []any{"summary_md"},
		"additionalProperties": false,
	}
}

func promptGraphExtract(threadTitle string, window string) (system string, user string) {
	system = `Extract a knowledge graph: entities, relations, and claims grounded in the text.
Return ONLY JSON matching the schema. Entities should be canonical and deduplicated.
Relations should be directed and typed. Claims should be short and verifiable.
Include evidence seqs for relations and claims.`
	user = "Thread title: " + threadTitle + "\n\nWindow:\n" + window
	return system, user
}

func schemaGraphExtract() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"entities": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":        map[string]any{"type": "string"},
						"type":        map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"},
						"aliases": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
					},
					"required":             []any{"name", "type", "description", "aliases"},
					"additionalProperties": false,
				},
			},
			"relations": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"src":      map[string]any{"type": "string"},
						"dst":      map[string]any{"type": "string"},
						"relation": map[string]any{"type": "string"},
						"weight":   map[string]any{"type": "number", "minimum": 0, "maximum": 1},
						"evidence_seqs": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "number"},
						},
					},
					"required":             []any{"src", "dst", "relation", "weight", "evidence_seqs"},
					"additionalProperties": false,
				},
			},
			"claims": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"content": map[string]any{"type": "string"},
						"entity_names": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
						"evidence_seqs": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "number"},
						},
					},
					"required":             []any{"content", "entity_names", "evidence_seqs"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []any{"entities", "relations", "claims"},
		"additionalProperties": false,
	}
}

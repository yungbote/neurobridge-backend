package prompts

func MaterialKGExtractSchema() map[string]any {
	entity := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":               map[string]any{"type": "string"},
			"type":               map[string]any{"type": "string"},
			"description":        map[string]any{"type": "string"},
			"aliases":            StringArraySchema(),
			"evidence_chunk_ids": StringArraySchema(),
		},
		"required": []string{
			"name",
			"type",
			"description",
			"aliases",
			"evidence_chunk_ids",
		},
		"additionalProperties": false,
	}

	claim := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"kind": EnumSchema(
				"definition",
				"fact",
				"procedure",
				"warning",
				"example",
				"claim",
			),
			"content":            map[string]any{"type": "string"},
			"confidence":         NumberSchema(),
			"entity_names":       StringArraySchema(),
			"concept_keys":       StringArraySchema(),
			"evidence_chunk_ids": StringArraySchema(),
		},
		"required": []string{
			"kind",
			"content",
			"confidence",
			"entity_names",
			"concept_keys",
			"evidence_chunk_ids",
		},
		"additionalProperties": false,
	}

	return SchemaVersionedObject(1, map[string]any{
		"entities": map[string]any{"type": "array", "items": entity},
		"claims":   map[string]any{"type": "array", "items": claim},
	}, []string{"entities", "claims"})
}

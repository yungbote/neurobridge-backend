package prompts

func LibraryTaxonomyRouteSchema() map[string]any {
	membership := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"node_id": map[string]any{"type": "string"},
			"weight":  NumberSchema(),
			"reason":  map[string]any{"type": "string"},
		},
		"required":             []string{"node_id", "weight", "reason"},
		"additionalProperties": false,
	}

	newNode := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"client_id":       map[string]any{"type": "string"},
			"kind":            map[string]any{"type": "string"},
			"name":            map[string]any{"type": "string"},
			"description":     map[string]any{"type": "string"},
			"parent_node_ids": StringArraySchema(),
			"related_node_ids": StringArraySchema(),
			"membership_weight": NumberSchema(),
			"reason":            map[string]any{"type": "string"},
		},
		"required": []string{
			"client_id",
			"kind",
			"name",
			"description",
			"parent_node_ids",
			"related_node_ids",
			"membership_weight",
			"reason",
		},
		"additionalProperties": false,
	}

	return SchemaVersionedObject(1, map[string]any{
		"facet": map[string]any{"type": "string"},
		"memberships": map[string]any{
			"type":  "array",
			"items": membership,
		},
		"new_nodes": map[string]any{
			"type":  "array",
			"items": newNode,
		},
	}, []string{"facet", "memberships", "new_nodes"})
}

func LibraryTaxonomyRefineSchema() map[string]any {
	decision := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"client_id":     map[string]any{"type": "string"},
			"should_create": BoolSchema(),
			"name":          map[string]any{"type": "string"},
			"description":   map[string]any{"type": "string"},
			"reason":        map[string]any{"type": "string"},
		},
		"required":             []string{"client_id", "should_create", "name", "description", "reason"},
		"additionalProperties": false,
	}
	return SchemaVersionedObject(1, map[string]any{
		"facet": map[string]any{"type": "string"},
		"decisions": map[string]any{
			"type":  "array",
			"items": decision,
		},
	}, []string{"facet", "decisions"})
}


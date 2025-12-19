package prompts

func SchemaVersionedObject(version int, properties map[string]any, required []string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	properties["version"] = map[string]any{"type": "integer", "const": version}
	properties["warnings"] = StringArraySchema()
	properties["diagnostics"] = map[string]any{"type": "object"}

	req := []string{"version", "warnings", "diagnostics"}
	req = append(req, required...)

	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             req,
		"additionalProperties": false,
	}
}

func StringArraySchema() map[string]any {
	return map[string]any{
		"type":  "array",
		"items": map[string]any{"type": "string"},
	}
}

func StringOrNullSchema() map[string]any {
	return map[string]any{
		"type": []any{"string", "null"},
	}
}

func NumberSchema() map[string]any {
	return map[string]any{"type": "number"}
}

func IntSchema() map[string]any {
	return map[string]any{"type": "integer"}
}

func BoolSchema() map[string]any {
	return map[string]any{"type": "boolean"}
}

func EnumSchema(values ...string) map[string]any {
	arr := make([]any, 0, len(values))
	for _, v := range values {
		arr = append(arr, v)
	}
	return map[string]any{"type": "string", "enum": arr}
}











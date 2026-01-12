package schema

import (
	"fmt"
	"sort"
	"strings"
)

// ValidateOpenAIJSONSchema validates a JSON schema against the strict subset required by
// OpenAI "structured outputs" (json_schema).
//
// It is intentionally opinionated:
// - Disallows oneOf/anyOf/allOf unions (OpenAI rejects them).
// - Requires every object with properties to have:
//   - additionalProperties: false
//   - required: [...] including *every* key in properties (OpenAI requirement).
func ValidateOpenAIJSONSchema(name string, schema map[string]any) error {
	if schema == nil {
		return fmt.Errorf("schema is nil")
	}
	path := strings.TrimSpace(name)
	if path == "" {
		path = "$"
	}
	return validateSchemaNode(schema, path)
}

func validateSchemaNode(node any, path string) error {
	m, ok := node.(map[string]any)
	if !ok || m == nil {
		// Primitive schema nodes are fine.
		return nil
	}

	for _, key := range []string{"oneOf", "anyOf", "allOf"} {
		if _, ok := m[key]; ok {
			return fmt.Errorf("%s: %s is not permitted", path, key)
		}
	}

	// Recurse into arrays.
	if items, ok := m["items"]; ok {
		if err := validateSchemaNode(items, path+".items"); err != nil {
			return err
		}
	}

	propsAny, hasProps := m["properties"]
	if !hasProps || propsAny == nil {
		return nil
	}

	props, ok := propsAny.(map[string]any)
	if !ok {
		return fmt.Errorf("%s: properties must be an object", path)
	}

	// Strict schemas should always disable additional properties.
	ap, ok := m["additionalProperties"]
	if !ok || ap != false {
		return fmt.Errorf("%s: additionalProperties must be false", path)
	}

	reqAny, ok := m["required"]
	if !ok || reqAny == nil {
		return fmt.Errorf("%s: required is required to be supplied and to include every key in properties", path)
	}

	reqArr, ok := reqAny.([]any)
	if !ok {
		return fmt.Errorf("%s: required must be an array", path)
	}

	reqSet := map[string]bool{}
	for _, v := range reqArr {
		k := strings.TrimSpace(fmt.Sprint(v))
		if k == "" {
			continue
		}
		reqSet[k] = true
	}

	missing := []string{}
	for k := range props {
		if !reqSet[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("%s: required missing keys: %v", path, missing)
	}

	extra := []string{}
	for k := range reqSet {
		if _, ok := props[k]; !ok {
			extra = append(extra, k)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return fmt.Errorf("%s: required includes unknown keys: %v", path, extra)
	}

	for k, child := range props {
		if err := validateSchemaNode(child, path+".properties."+k); err != nil {
			return err
		}
	}

	return nil
}

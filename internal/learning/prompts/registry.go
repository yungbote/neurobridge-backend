package prompts

import (
	"fmt"
	"strings"
)


type Template struct {
	Name       PromptName
	Version    int
	SchemaName string
	Schema     func() map[string]any
	System     func(Input) string
	User       func(Input) string
	Validate   Validator
}

var registry = map[PromptName]Template{}

// Register registers a compiled Template.
func Register(t Template) {
	registry[t.Name] = t
}

// Build returns a Prompt ready to pass into openai.GenerateJSON.
func Build(name PromptName, in Input) (Prompt, error) {
	t, ok := registry[name]
	if !ok {
		return Prompt{}, fmt.Errorf("unknown prompt: %s", string(name))
	}
	if t.Schema == nil {
		return Prompt{}, fmt.Errorf("prompt %s missing schema", string(name))
	}
	if t.System == nil || t.User == nil {
		return Prompt{}, fmt.Errorf("prompt %s missing system/user renderers", string(name))
	}
	if t.Validate != nil {
		if err := t.Validate(in); err != nil {
			return Prompt{}, fmt.Errorf("%s: %w", string(name), err)
		}
	}

	p := Prompt{
		Name:       string(t.Name),
		Version:    t.Version,
		SchemaName: strings.TrimSpace(t.SchemaName),
		Schema:     t.Schema(),
		System:     strings.TrimSpace(t.System(in)),
		User:       strings.TrimSpace(t.User(in)),
	}
	return p, nil
}

func Schema(name PromptName) (schemaName string, schema map[string]any, ok bool) {
	t, ok := registry[name]
	if !ok || t.Schema == nil {
		return "", nil, false
	}
	return t.SchemaName, t.Schema(), true
}











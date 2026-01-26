package engine

import "context"

type Message struct {
	Role    string
	Content string
}

type JSONSchema struct {
	Name   string
	Schema map[string]any
	Strict bool
}

type TextPair struct {
	A string
	B string
}

type GenerateOptions struct {
	Temperature float64
	JSONSchema  *JSONSchema
}

type Engine interface {
	Embed(ctx context.Context, model string, inputs []string) ([][]float32, error)
	GenerateText(ctx context.Context, model string, messages []Message, opts GenerateOptions) (string, error)
	StreamText(ctx context.Context, model string, messages []Message, opts GenerateOptions, onDelta func(delta string)) (full string, err error)
	ScoreTextPairs(ctx context.Context, model string, pairs []TextPair) ([]float32, error)
}

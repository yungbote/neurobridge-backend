package v1

type Model struct {
	ID string `json:"id"`
}

type ModelsResponse struct {
	Models []Model `json:"models"`
}

type EmbeddingsRequest struct {
	Model  string   `json:"model"`
	Inputs []string `json:"inputs"`
}

type EmbeddingsItem struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

type EmbeddingsResponse struct {
	Model string           `json:"model"`
	Data  []EmbeddingsItem `json:"data"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type JSONSchema struct {
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema"`
	Strict bool           `json:"strict,omitempty"`
}

type TextGenerateRequest struct {
	Model       string      `json:"model"`
	Messages    []Message   `json:"messages"`
	Temperature float64     `json:"temperature,omitempty"`
	Stream      bool        `json:"stream,omitempty"`
	JSONSchema  *JSONSchema `json:"json_schema,omitempty"`
}

type TextGenerateResponse struct {
	Model      string `json:"model"`
	OutputText string `json:"output_text"`
}

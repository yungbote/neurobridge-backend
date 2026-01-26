package client

type embeddingsRequest struct {
	Model  string   `json:"model"`
	Inputs []string `json:"inputs"`
}

type embeddingsResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

type textGenerateRequest struct {
	Model       string                  `json:"model"`
	Messages    []textGenerateMessage   `json:"messages"`
	Temperature float64                 `json:"temperature,omitempty"`
	Stream      bool                    `json:"stream,omitempty"`
	JSONSchema  *textGenerateJSONSchema `json:"json_schema,omitempty"`
}

type textGenerateMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type textGenerateJSONSchema struct {
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema"`
	Strict bool           `json:"strict,omitempty"`
}

type textGenerateResponse struct {
	OutputText string `json:"output_text"`
}

type TextScorePair struct {
	A string `json:"a"`
	B string `json:"b"`
}

type textScoreRequest struct {
	Model string          `json:"model"`
	Pairs []TextScorePair `json:"pairs"`
}

type textScoreResponse struct {
	Model  string    `json:"model"`
	Scores []float32 `json:"scores"`
}

type ImageInput struct {
	ImageURL string
	Detail   string
}

type ImageGeneration struct {
	Bytes         []byte
	MimeType      string
	RevisedPrompt string
	URL           string
}

type VideoGenerationOptions struct {
	DurationSeconds int
	Size            string
}

type VideoGeneration struct {
	Bytes         []byte
	MimeType      string
	RevisedPrompt string
	URL           string
}

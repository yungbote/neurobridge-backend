package oai

type embeddingsRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type embeddingsResponse struct {
	Object string           `json:"object"`
	Data   []embeddingsItem `json:"data"`
	Model  string           `json:"model"`
	Usage  embeddingsUsage  `json:"usage"`
}

type embeddingsItem struct {
	Object    string    `json:"object"`
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

type embeddingsUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type modelsResponse struct {
	Object string       `json:"object"`
	Data   []modelEntry `json:"data"`
}

type modelEntry struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by,omitempty"`
}

type responsesRequest struct {
	Model string `json:"model"`

	Conversation any    `json:"conversation,omitempty"`
	Instructions string `json:"instructions,omitempty"`

	Input []struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	} `json:"input"`

	Text struct {
		Format map[string]any `json:"format,omitempty"`
	} `json:"text,omitempty"`

	Temperature float64 `json:"temperature,omitempty"`
	Stream      bool    `json:"stream,omitempty"`
}

type responsesResponse struct {
	Output []struct {
		Type    string `json:"type"`
		Role    string `json:"role,omitempty"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		} `json:"content,omitempty"`
	} `json:"output"`
	Refusal string `json:"refusal,omitempty"`
}

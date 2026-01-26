package oaihttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/inference/config"
	"github.com/yungbote/neurobridge-backend/internal/inference/engine"
)

type Engine struct {
	baseURL string
	apiKey  string

	chatCompletionsPath string
	embeddingsPath      string

	timeout       time.Duration
	streamTimeout time.Duration

	jsonSchemaMode           string
	jsonSchemaMaxRetries     int
	jsonSchemaMaxPromptBytes int

	httpClient *http.Client
}

func New(cfg config.EngineConfig) (*Engine, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		return nil, errors.New("oai_http: base_url required")
	}

	chatPath := strings.TrimSpace(cfg.ChatCompletionsPath)
	if chatPath == "" {
		chatPath = "/v1/chat/completions"
	}
	embPath := strings.TrimSpace(cfg.EmbeddingsPath)
	if embPath == "" {
		embPath = "/v1/embeddings"
	}

	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	timeout := cfg.Timeout.Duration
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	mode := strings.ToLower(strings.TrimSpace(cfg.JSONSchema.Mode))
	if mode == "" {
		mode = "auto"
	}

	maxRetries := cfg.JSONSchema.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	if maxRetries == 0 {
		maxRetries = 2
	}

	maxPromptBytes := cfg.JSONSchema.MaxPromptBytes
	if maxPromptBytes <= 0 {
		maxPromptBytes = 64 << 10
	}

	return &Engine{
		baseURL:                  baseURL,
		apiKey:                   strings.TrimSpace(cfg.APIKey),
		chatCompletionsPath:      chatPath,
		embeddingsPath:           embPath,
		timeout:                  timeout,
		streamTimeout:            cfg.StreamTimeout.Duration,
		jsonSchemaMode:           mode,
		jsonSchemaMaxRetries:     maxRetries,
		jsonSchemaMaxPromptBytes: maxPromptBytes,
		httpClient:               &http.Client{Transport: tr},
	}, nil
}

// NewWithHTTPClient is intended for tests; it avoids network access by using a custom RoundTripper.
func NewWithHTTPClient(cfg config.EngineConfig, httpClient *http.Client) (*Engine, error) {
	e, err := New(cfg)
	if err != nil {
		return nil, err
	}
	if httpClient != nil {
		e.httpClient = httpClient
	}
	return e, nil
}

// ---------------- Embeddings ----------------

type embeddingsRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type embeddingsResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

func (e *Engine) Embed(ctx context.Context, model string, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return [][]float32{}, nil
	}

	reqBody := embeddingsRequest{
		Model: model,
		Input: inputs,
	}

	var resp embeddingsResponse
	if err := e.doJSON(ctx, e.timeout, "POST", e.embeddingsPath, reqBody, &resp, "application/json"); err != nil {
		return nil, err
	}

	out := make([][]float32, len(inputs))
	for _, d := range resp.Data {
		vec := make([]float32, len(d.Embedding))
		for i, f := range d.Embedding {
			vec[i] = float32(f)
		}
		if d.Index >= 0 && d.Index < len(out) {
			out[d.Index] = vec
		}
	}

	// Best-effort fix: some servers may omit indices but keep ordering.
	for i := range out {
		if out[i] != nil {
			continue
		}
		if i >= 0 && i < len(resp.Data) {
			d := resp.Data[i]
			vec := make([]float32, len(d.Embedding))
			for j, f := range d.Embedding {
				vec[j] = float32(f)
			}
			out[i] = vec
		}
	}

	for i := range out {
		if out[i] == nil || len(out[i]) == 0 {
			return nil, fmt.Errorf("embeddings missing index=%d (model=%s)", i, model)
		}
	}

	return out, nil
}

// ---------------- Text generation (Chat Completions) ----------------

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature,omitempty"`
	Stream      bool          `json:"stream,omitempty"`

	// Optional OpenAI-compatible extensions supported by vLLM/SGLang variants.
	ResponseFormat map[string]any `json:"response_format,omitempty"`
	GuidedJSON     any            `json:"guided_json,omitempty"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content,omitempty"`
		} `json:"message,omitempty"`
		Text string `json:"text,omitempty"`
	} `json:"choices"`
}

type chatCompletionStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content,omitempty"`
		} `json:"delta,omitempty"`
		Text string `json:"text,omitempty"`
	} `json:"choices"`
	Error any `json:"error,omitempty"`
}

func (e *Engine) GenerateText(ctx context.Context, model string, messages []engine.Message, opts engine.GenerateOptions) (string, error) {
	chatMsgs := toChatMessages(messages)
	if len(chatMsgs) == 0 {
		return "", errors.New("no messages")
	}

	attempts := 1
	if opts.JSONSchema != nil && opts.JSONSchema.Strict {
		attempts = 1 + e.jsonSchemaMaxRetries
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		reqBody := e.buildChatRequest(model, chatMsgs, opts, false, attempt)

		var resp chatCompletionResponse
		err := e.doJSON(ctx, e.timeout, "POST", e.chatCompletionsPath, reqBody, &resp, "application/json")
		if err != nil {
			lastErr = err
			continue
		}

		text := extractChatText(resp)
		if strings.TrimSpace(text) == "" {
			lastErr = errors.New("empty upstream completion")
			continue
		}

		if opts.JSONSchema != nil && opts.JSONSchema.Strict {
			clean := sanitizeJSONText(text)
			if err := validateJSON(clean); err != nil {
				lastErr = err
				continue
			}
			return clean, nil
		}

		return text, nil
	}

	if lastErr == nil {
		lastErr = errors.New("generation failed")
	}
	return "", lastErr
}

func (e *Engine) StreamText(ctx context.Context, model string, messages []engine.Message, opts engine.GenerateOptions, onDelta func(delta string)) (string, error) {
	chatMsgs := toChatMessages(messages)
	if len(chatMsgs) == 0 {
		return "", errors.New("no messages")
	}

	timeout := e.streamTimeout
	if timeout <= 0 {
		timeout = 0
	}

	reqBody := e.buildChatRequest(model, chatMsgs, opts, true, 0)

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(reqBody); err != nil {
		return "", err
	}

	ctx2 := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx2, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx2, "POST", e.baseURL+e.chatCompletionsPath, &buf)
	if err != nil {
		return "", err
	}
	e.setHeaders(req, "application/json", "text/event-stream")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return "", &HTTPError{StatusCode: resp.StatusCode, Body: string(raw)}
	}

	var full strings.Builder
	err = streamSSE(resp.Body, func(_ string, data string) error {
		if strings.TrimSpace(data) == "" {
			return nil
		}
		if strings.TrimSpace(data) == "[DONE]" {
			return nil
		}

		var chunk chatCompletionStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return nil
		}
		if chunk.Error != nil {
			b, _ := json.Marshal(chunk.Error)
			return fmt.Errorf("upstream stream error: %s", string(b))
		}

		for _, c := range chunk.Choices {
			delta := c.Delta.Content
			if delta == "" {
				delta = c.Text
			}
			if delta == "" {
				continue
			}
			full.WriteString(delta)
			if onDelta != nil {
				onDelta(delta)
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return full.String(), nil
}

// ---------------- Text pair scoring ----------------

func (e *Engine) ScoreTextPairs(ctx context.Context, model string, pairs []engine.TextPair) ([]float32, error) {
	if len(pairs) == 0 {
		return []float32{}, nil
	}
	system := strings.TrimSpace(strings.Join([]string{
		"You score semantic coherence between two texts.",
		"Return a score from 0 to 1 where 1 means they can be taught together in a single coherent path.",
		"Output JSON only, matching the provided schema.",
	}, "\n"))

	userPairs := make([]map[string]string, 0, len(pairs))
	for _, p := range pairs {
		userPairs = append(userPairs, map[string]string{
			"a": strings.TrimSpace(p.A),
			"b": strings.TrimSpace(p.B),
		})
	}
	payload, _ := json.Marshal(map[string]any{"pairs": userPairs})
	user := "PAIRS_JSON:\n" + string(payload) + "\n"

	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"scores": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "number"},
			},
		},
		"required": []string{"scores"},
	}

	text, err := e.GenerateText(ctx, model, []engine.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}, engine.GenerateOptions{
		Temperature: 0.0,
		JSONSchema: &engine.JSONSchema{
			Name:   "pair_score",
			Schema: schema,
			Strict: true,
		},
	})
	if err != nil {
		return nil, err
	}

	var out struct {
		Scores []float64 `json:"scores"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, err
	}
	if len(out.Scores) == 0 {
		return nil, fmt.Errorf("score_text_pairs: empty scores")
	}

	scores := make([]float32, len(out.Scores))
	for i, v := range out.Scores {
		scores[i] = float32(v)
	}
	return scores, nil
}

func (e *Engine) buildChatRequest(model string, messages []chatMessage, opts engine.GenerateOptions, stream bool, attempt int) chatCompletionRequest {
	req := chatCompletionRequest{
		Model:       model,
		Messages:    messages,
		Temperature: opts.Temperature,
		Stream:      stream,
	}

	if opts.JSONSchema == nil {
		return req
	}

	mode := e.jsonSchemaMode
	if mode == "" {
		mode = "auto"
	}

	useGuided := mode == "guided_json" || (mode == "auto" && attempt == 0)
	usePrompt := mode == "prompt" || (mode == "auto" && attempt > 0)

	if useGuided && opts.JSONSchema.Schema != nil {
		req.ResponseFormat = map[string]any{"type": "json_object"}
		req.GuidedJSON = opts.JSONSchema.Schema
	}

	if usePrompt {
		req.Messages = append(req.Messages, chatMessage{
			Role:    "system",
			Content: e.jsonSchemaPrompt(opts.JSONSchema),
		})
	}

	return req
}

func (e *Engine) jsonSchemaPrompt(s *engine.JSONSchema) string {
	if s == nil {
		return "Return ONLY valid JSON. Do not include markdown or commentary."
	}
	name := strings.TrimSpace(s.Name)

	var schemaText string
	if s.Schema != nil {
		if b, err := json.Marshal(s.Schema); err == nil {
			if len(b) <= e.jsonSchemaMaxPromptBytes {
				schemaText = string(b)
			}
		}
	}

	var b strings.Builder
	b.WriteString("Return ONLY a valid JSON value that conforms to the provided JSON Schema. Do not include markdown or commentary.\n")
	if name != "" {
		b.WriteString("Schema name: ")
		b.WriteString(name)
		b.WriteString("\n")
	}
	if schemaText != "" {
		b.WriteString("Schema:\n")
		b.WriteString(schemaText)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func toChatMessages(messages []engine.Message) []chatMessage {
	out := make([]chatMessage, 0, len(messages))
	for _, m := range messages {
		role := strings.TrimSpace(m.Role)
		content := strings.TrimSpace(m.Content)
		if role == "" || content == "" {
			continue
		}
		out = append(out, chatMessage{Role: role, Content: content})
	}
	return out
}

func extractChatText(resp chatCompletionResponse) string {
	for _, c := range resp.Choices {
		if strings.TrimSpace(c.Message.Content) != "" {
			return c.Message.Content
		}
		if strings.TrimSpace(c.Text) != "" {
			return c.Text
		}
	}
	return ""
}

func sanitizeJSONText(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}

	// Strip leading ```lang and trailing ```
	firstNL := strings.IndexByte(s, '\n')
	if firstNL == -1 {
		return strings.TrimSpace(strings.Trim(s, "`"))
	}
	s = s[firstNL+1:]

	if idx := strings.LastIndex(s, "```"); idx != -1 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

func validateJSON(s string) error {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return fmt.Errorf("invalid json: %w", err)
	}
	return nil
}

// ---------------- HTTP helpers ----------------

func (e *Engine) setHeaders(req *http.Request, contentType string, accept string) {
	if strings.TrimSpace(contentType) != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if strings.TrimSpace(accept) != "" {
		req.Header.Set("Accept", accept)
	}
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}
}

func (e *Engine) doJSON(ctx context.Context, timeout time.Duration, method string, path string, body any, out any, accept string) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}

	ctx2 := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx2, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx2, method, e.baseURL+path, &buf)
	if err != nil {
		return err
	}
	e.setHeaders(req, "application/json", accept)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return &HTTPError{StatusCode: resp.StatusCode, Body: string(raw)}
	}

	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

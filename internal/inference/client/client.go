package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/platform/promptstyle"
)

type Options struct {
	BaseURL string
	APIKey  string

	Model      string
	EmbedModel string
	ScoreModel string
	ImageModel string
	VideoModel string

	ImageSize string
	VideoSize string

	Timeout       time.Duration
	StreamTimeout time.Duration
	MaxRetries    int

	HTTPClient *http.Client
}

type Client struct {
	baseURL string
	apiKey  string

	model      string
	embedModel string
	scoreModel string
	imageModel string
	videoModel string

	imageSize string
	videoSize string

	timeout       time.Duration
	streamTimeout time.Duration
	maxRetries    int

	httpClient *http.Client
}

func New(opts Options) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		return nil, errors.New("baseURL required")
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	maxRetries := opts.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}

	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{}
	}

	return &Client{
		baseURL:       baseURL,
		apiKey:        strings.TrimSpace(opts.APIKey),
		model:         strings.TrimSpace(opts.Model),
		embedModel:    strings.TrimSpace(opts.EmbedModel),
		scoreModel:    strings.TrimSpace(opts.ScoreModel),
		imageModel:    strings.TrimSpace(opts.ImageModel),
		videoModel:    strings.TrimSpace(opts.VideoModel),
		imageSize:     strings.TrimSpace(opts.ImageSize),
		videoSize:     strings.TrimSpace(opts.VideoSize),
		timeout:       timeout,
		streamTimeout: opts.StreamTimeout,
		maxRetries:    maxRetries,
		httpClient:    hc,
	}, nil
}

func NewFromEnv() (*Client, error) {
	timeoutSeconds := intFromEnv("NB_INFERENCE_TIMEOUT_SECONDS", 60)
	streamTimeoutSeconds := intFromEnv("NB_INFERENCE_STREAM_TIMEOUT_SECONDS", 0)
	maxRetries := intFromEnv("NB_INFERENCE_MAX_RETRIES", 2)

	return New(Options{
		BaseURL:       getEnv("NB_INFERENCE_BASE_URL", "http://localhost:8080"),
		APIKey:        strings.TrimSpace(os.Getenv("NB_INFERENCE_API_KEY")),
		Model:         strings.TrimSpace(os.Getenv("NB_INFERENCE_MODEL")),
		EmbedModel:    strings.TrimSpace(os.Getenv("NB_INFERENCE_EMBED_MODEL")),
		ScoreModel:    strings.TrimSpace(os.Getenv("NB_INFERENCE_SCORE_MODEL")),
		ImageModel:    strings.TrimSpace(os.Getenv("NB_INFERENCE_IMAGE_MODEL")),
		ImageSize:     strings.TrimSpace(os.Getenv("NB_INFERENCE_IMAGE_SIZE")),
		VideoModel:    strings.TrimSpace(os.Getenv("NB_INFERENCE_VIDEO_MODEL")),
		VideoSize:     strings.TrimSpace(os.Getenv("NB_INFERENCE_VIDEO_SIZE")),
		Timeout:       time.Duration(timeoutSeconds) * time.Second,
		StreamTimeout: time.Duration(streamTimeoutSeconds) * time.Second,
		MaxRetries:    maxRetries,
	})
}

func (c *Client) BaseURL() string { return c.baseURL }

func (c *Client) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if strings.TrimSpace(c.embedModel) == "" {
		return nil, errors.New("missing NB_INFERENCE_EMBED_MODEL")
	}

	req := embeddingsRequest{
		Model:  c.embedModel,
		Inputs: normalizeStrings(inputs),
	}

	var resp embeddingsResponse
	if err := c.doJSON(ctx, c.timeout, http.MethodPost, "/v1/embeddings", req, &resp); err != nil {
		return nil, err
	}

	out := make([][]float32, len(inputs))
	for _, d := range resp.Data {
		if d.Index >= 0 && d.Index < len(out) {
			out[d.Index] = d.Embedding
		}
	}
	for i := range out {
		if out[i] == nil {
			return nil, fmt.Errorf("embeddings missing index=%d", i)
		}
	}
	return out, nil
}

func (c *Client) ScorePairs(ctx context.Context, pairs []TextScorePair) ([]float32, error) {
	if strings.TrimSpace(c.scoreModel) == "" {
		return nil, ErrScoreNotConfigured
	}
	if len(pairs) == 0 {
		return []float32{}, nil
	}

	req := textScoreRequest{
		Model: c.scoreModel,
		Pairs: pairs,
	}

	var resp textScoreResponse
	if err := c.doJSON(ctx, c.timeout, http.MethodPost, "/v1/text/score", req, &resp); err != nil {
		if herr, ok := err.(*HTTPError); ok {
			if herr.StatusCode == http.StatusNotFound || herr.StatusCode == http.StatusNotImplemented {
				return nil, ErrScoreNotSupported
			}
		}
		return nil, err
	}
	if len(resp.Scores) != len(pairs) {
		return nil, fmt.Errorf("score response length mismatch: got %d want %d", len(resp.Scores), len(pairs))
	}
	return resp.Scores, nil
}

func (c *Client) GenerateText(ctx context.Context, system string, user string) (string, error) {
	if strings.TrimSpace(c.model) == "" {
		return "", errors.New("missing NB_INFERENCE_MODEL")
	}
	system = promptstyle.ApplySystem(system, "text")

	req := textGenerateRequest{
		Model: c.model,
		Messages: []textGenerateMessage{
			{Role: "system", Content: strings.TrimSpace(system)},
			{Role: "user", Content: user},
		},
		Temperature: 0.2,
	}

	var resp textGenerateResponse
	if err := c.doJSON(ctx, c.timeout, http.MethodPost, "/v1/text/generate", req, &resp); err != nil {
		return "", err
	}

	text := resp.OutputText
	if strings.TrimSpace(text) == "" {
		return "", errors.New("empty output_text")
	}
	return text, nil
}

func (c *Client) GenerateJSON(ctx context.Context, system string, user string, schemaName string, schema map[string]any) (map[string]any, error) {
	if strings.TrimSpace(c.model) == "" {
		return nil, errors.New("missing NB_INFERENCE_MODEL")
	}
	if strings.TrimSpace(schemaName) == "" {
		return nil, errors.New("schemaName required")
	}
	if schema == nil {
		return nil, errors.New("schema required")
	}
	system = promptstyle.ApplySystem(system, "json")

	req := textGenerateRequest{
		Model: c.model,
		Messages: []textGenerateMessage{
			{Role: "system", Content: strings.TrimSpace(system)},
			{Role: "user", Content: user},
		},
		Temperature: 0.2,
	}
	req.JSONSchema = &textGenerateJSONSchema{
		Name:   strings.TrimSpace(schemaName),
		Schema: schema,
		Strict: true,
	}

	var resp textGenerateResponse
	if err := c.doJSON(ctx, c.timeout, http.MethodPost, "/v1/text/generate", req, &resp); err != nil {
		return nil, err
	}

	text := resp.OutputText
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("empty output_text")
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		return nil, fmt.Errorf("failed to parse json output: %w", err)
	}
	return obj, nil
}

func (c *Client) StreamText(ctx context.Context, system string, user string, onDelta func(delta string)) (string, error) {
	if strings.TrimSpace(c.model) == "" {
		return "", errors.New("missing NB_INFERENCE_MODEL")
	}
	system = promptstyle.ApplySystem(system, "text")

	timeout := c.streamTimeout
	if timeout < 0 {
		timeout = 0
	}

	reqBody := textGenerateRequest{
		Model: c.model,
		Messages: []textGenerateMessage{
			{Role: "system", Content: strings.TrimSpace(system)},
			{Role: "user", Content: user},
		},
		Temperature: 0.2,
		Stream:      true,
	}

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

	req, err := http.NewRequestWithContext(ctx2, http.MethodPost, c.baseURL+"/v1/text/generate", &buf)
	if err != nil {
		return "", err
	}
	c.setHeaders(req, "application/json", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return "", parseHTTPError(resp.StatusCode, raw)
	}

	var full strings.Builder
	err = streamSSE(resp.Body, func(event string, data string) error {
		if strings.TrimSpace(data) == "" || strings.TrimSpace(data) == "[DONE]" {
			return nil
		}

		switch strings.TrimSpace(event) {
		case "text.delta":
			var obj struct {
				Delta string `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &obj); err != nil {
				return nil
			}
			d := strings.TrimRight(obj.Delta, "\u0000")
			if d == "" {
				return nil
			}
			full.WriteString(d)
			if onDelta != nil {
				onDelta(d)
			}
			return nil
		case "error":
			var obj struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal([]byte(data), &obj); err == nil && strings.TrimSpace(obj.Message) != "" {
				return fmt.Errorf("stream error: %s", strings.TrimSpace(obj.Message))
			}
			return fmt.Errorf("stream error: %s", strings.TrimSpace(data))
		default:
			return nil
		}
	})
	if err != nil {
		return "", err
	}
	return full.String(), nil
}

// ---- Images (planned) ----

func (c *Client) GenerateImage(ctx context.Context, prompt string) (ImageGeneration, error) {
	_ = ctx
	_ = prompt
	return ImageGeneration{}, errors.New("image generation not implemented in inference gateway yet")
}

// ---- Videos (planned) ----

func (c *Client) GenerateVideo(ctx context.Context, prompt string, opts VideoGenerationOptions) (VideoGeneration, error) {
	_ = ctx
	_ = prompt
	_ = opts
	return VideoGeneration{}, errors.New("video generation not implemented in inference gateway yet")
}

// ---------------- HTTP helpers ----------------

func (c *Client) setHeaders(req *http.Request, contentType string, accept string) {
	if strings.TrimSpace(contentType) != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if strings.TrimSpace(accept) != "" {
		req.Header.Set("Accept", accept)
	}
	if strings.TrimSpace(c.apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.apiKey))
	}
}

func (c *Client) doJSON(ctx context.Context, timeout time.Duration, method string, path string, body any, out any) error {
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

	var lastErr error
	backoff := 250 * time.Millisecond
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if ctx2.Err() != nil {
			return ctx2.Err()
		}

		req, err := http.NewRequestWithContext(ctx2, method, c.baseURL+path, bytes.NewReader(buf.Bytes()))
		if err != nil {
			return err
		}
		c.setHeaders(req, "application/json", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
		} else {
			raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			_ = resp.Body.Close()
			if readErr != nil {
				return readErr
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				lastErr = parseHTTPError(resp.StatusCode, raw)
			} else {
				if out == nil {
					return nil
				}
				if err := json.Unmarshal(raw, out); err != nil {
					return err
				}
				return nil
			}
		}

		if attempt < c.maxRetries {
			select {
			case <-ctx2.Done():
				return ctx2.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			continue
		}
	}

	if lastErr == nil {
		lastErr = errors.New("request failed")
	}
	return lastErr
}

func normalizeStrings(inputs []string) []string {
	if len(inputs) == 0 {
		return []string{}
	}
	out := make([]string, len(inputs))
	for i := range inputs {
		s := strings.TrimSpace(inputs[i])
		if s == "" {
			s = " "
		}
		out[i] = s
	}
	return out
}

func getEnv(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

func intFromEnv(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

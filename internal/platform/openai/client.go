package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/httpx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/promptstyle"
	"github.com/yungbote/neurobridge-backend/internal/observability"
)

// ImageInput is the normalized multimodal image input used by Client.
type ImageInput struct {
	// Can be https://... or data:image/...;base64,...
	ImageURL string
	// Optional. Some models may ignore; kept for compatibility.
	Detail string // "low" | "high"
}

type ImageGeneration struct {
	Bytes         []byte
	MimeType      string
	RevisedPrompt string
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

// Client is the OpenAI API client used by the rest of the backend.
type Client interface {
	Embed(ctx context.Context, inputs []string) ([][]float32, error)

	// Structured outputs (json_schema)
	GenerateJSON(ctx context.Context, system string, user string, schemaName string, schema map[string]any) (map[string]any, error)

	// Plain text (no schema)
	GenerateText(ctx context.Context, system string, user string) (string, error)

	// Multimodal: user prompt + images -> plain text
	GenerateTextWithImages(ctx context.Context, system string, user string, images []ImageInput) (string, error)

	// Image generation (raster). Returns bytes (PNG by default).
	GenerateImage(ctx context.Context, prompt string) (ImageGeneration, error)

	// Video generation (Sora). Returns bytes (mp4/webm) when possible; may also return a URL.
	GenerateVideo(ctx context.Context, prompt string, opts VideoGenerationOptions) (VideoGeneration, error)

	// Stream output_text deltas without conversation state. Returns the full text.
	StreamText(ctx context.Context, system string, user string, onDelta func(delta string)) (string, error)

	// Conversations API (server-side turn storage).
	CreateConversation(ctx context.Context) (conversationID string, err error)

	// Responses API w/ conversation: instructions are not persisted automatically.
	GenerateTextInConversation(ctx context.Context, conversationID string, instructions string, user string) (string, error)

	// Stream output_text deltas for a conversation-backed response. Returns the full text.
	StreamTextInConversation(ctx context.Context, conversationID string, instructions string, user string, onDelta func(delta string)) (string, error)
}

// ---- Backwards-compat aliases (so you don't break existing imports immediately) ----
type OpenAIClient = Client
type OpenAIImageInput = ImageInput

func NewOpenAIClient(log *logger.Logger) (OpenAIClient, error) { return NewClient(log) }

// -------------------------------------------------------------------------------

// WithModel returns a client that uses the provided model for text generation calls.
// If model is empty or base is nil, it returns the base client unchanged.
func WithModel(base Client, model string) Client {
	model = strings.TrimSpace(model)
	if base == nil || model == "" {
		return base
	}
	if c, ok := base.(*client); ok {
		return c.cloneWithModel(model)
	}
	return base
}

type client struct {
	log             *logger.Logger
	baseURL         string
	apiKey          string
	model           string
	embedModel      string
	imageModel      string
	imageSize       string
	videoModel      string
	videoSize       string
	httpClient      *http.Client
	responsesClient *http.Client

	maxRetries int

	// Temperature control (client-level)
	temperature        *float64
	disableTemperature bool

	// Optional static denylist from env (so you can avoid the first-failure retry)
	noTempModels   map[string]bool // exact model ids (lowercased)
	noTempPrefixes []string        // prefix matches (lowercased), e.g. "o1-", "o3-"

	// Runtime learning: if a model rejects temperature, remember for TTL and omit thereafter.
	noTempMu   sync.RWMutex
	noTempSeen map[string]time.Time
	noTempTTL  time.Duration
}

func NewClient(log *logger.Logger) (Client, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return nil, fmt.Errorf("missing OPENAI_API_KEY")
	}

	baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	model := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
	if model == "" {
		model = "gpt-5.2"
	}

	embed := strings.TrimSpace(os.Getenv("OPENAI_EMBED_MODEL"))
	if embed == "" {
		embed = "text-embedding-3-small"
	}

	imageModel := strings.TrimSpace(os.Getenv("OPENAI_IMAGE_MODEL"))
	imageSize := strings.TrimSpace(os.Getenv("OPENAI_IMAGE_SIZE"))
	if imageSize == "" {
		imageSize = "1024x1024"
	}

	videoModel := strings.TrimSpace(os.Getenv("OPENAI_VIDEO_MODEL"))
	videoSize := strings.TrimSpace(os.Getenv("OPENAI_VIDEO_SIZE"))
	if videoSize == "" {
		videoSize = "1280x720"
	}

	timeoutSec := 180
	if v := os.Getenv("OPENAI_TIMEOUT_SECONDS"); v != "" {
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && parsed > 0 {
			timeoutSec = parsed
		}
	}

	responsesTimeoutSec := 0
	if v := os.Getenv("OPENAI_RESPONSES_TIMEOUT_SECONDS"); v != "" {
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && parsed > 0 {
			responsesTimeoutSec = parsed
		}
	}
	if responsesTimeoutSec <= 0 {
		responsesTimeoutSec = timeoutSec
		if responsesTimeoutSec < 600 {
			responsesTimeoutSec = 600
		}
	}

	maxRetries := 4
	if v := os.Getenv("OPENAI_MAX_RETRIES"); v != "" {
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && parsed >= 0 {
			maxRetries = parsed
		}
	}

	// Temperature: default 0.2, but can be disabled or overridden.
	disableTemperature := parseBoolEnv("OPENAI_DISABLE_TEMPERATURE", false)

	tempPtr := (*float64)(nil)
	if !disableTemperature {
		temp := 0.2
		if v := strings.TrimSpace(os.Getenv("OPENAI_TEMPERATURE")); v != "" {
			low := strings.ToLower(strings.TrimSpace(v))
			if low == "off" || low == "none" || low == "nil" || low == "false" {
				disableTemperature = true
			} else if f, err := strconv.ParseFloat(v, 64); err == nil {
				temp = f
			}
		}
		if !disableTemperature {
			tempPtr = f64ptr(temp)
		}
	}

	// Optional: static denylist so we can omit temperature without first triggering a 400.
	noTempModels, noTempPrefixes := parseNoTempModelRules(os.Getenv("OPENAI_NO_TEMPERATURE_MODELS"))

	noTempTTL := 24 * time.Hour
	if v := strings.TrimSpace(os.Getenv("OPENAI_NO_TEMPERATURE_TTL_SECONDS")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			noTempTTL = time.Duration(parsed) * time.Second
		}
	}

	if log == nil {
		return nil, fmt.Errorf("logger required")
	}

	return &client{
		log:                log.With("service", "OpenAIClient"),
		baseURL:            baseURL,
		apiKey:             apiKey,
		model:              model,
		embedModel:         embed,
		imageModel:         imageModel,
		imageSize:          imageSize,
		videoModel:         videoModel,
		videoSize:          videoSize,
		httpClient:         &http.Client{Timeout: time.Duration(timeoutSec) * time.Second},
		responsesClient:    &http.Client{Timeout: time.Duration(responsesTimeoutSec) * time.Second},
		maxRetries:         maxRetries,
		temperature:        tempPtr,
		disableTemperature: disableTemperature,
		noTempModels:       noTempModels,
		noTempPrefixes:     noTempPrefixes,
		noTempSeen:         map[string]time.Time{},
		noTempTTL:          noTempTTL,
	}, nil
}

// NewClientWithModel returns a client configured with the provided model override.
// It uses the same env configuration as NewClient, but replaces the model if non-empty.
func NewClientWithModel(log *logger.Logger, modelOverride string) (Client, error) {
	c, err := NewClient(log)
	if err != nil {
		return nil, err
	}
	if modelOverride == "" {
		return c, nil
	}
	if cc, ok := c.(*client); ok {
		cc.model = strings.TrimSpace(modelOverride)
	}
	return c, nil
}

func (c *client) cloneWithModel(model string) *client {
	if c == nil || strings.TrimSpace(model) == "" {
		return c
	}
	clone := &client{
		log:                c.log,
		baseURL:            c.baseURL,
		apiKey:             c.apiKey,
		model:              strings.TrimSpace(model),
		embedModel:         c.embedModel,
		imageModel:         c.imageModel,
		imageSize:          c.imageSize,
		videoModel:         c.videoModel,
		videoSize:          c.videoSize,
		httpClient:         c.httpClient,
		responsesClient:    c.responsesClient,
		maxRetries:         c.maxRetries,
		temperature:        c.temperature,
		disableTemperature: c.disableTemperature,
		noTempModels:       c.noTempModels,
		noTempPrefixes:     c.noTempPrefixes,
		noTempSeen:         map[string]time.Time{},
		noTempTTL:          c.noTempTTL,
	}

	c.noTempMu.RLock()
	for k, v := range c.noTempSeen {
		clone.noTempSeen[k] = v
	}
	c.noTempMu.RUnlock()

	return clone
}

func parseBoolEnv(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return def
	}
}

func f64ptr(v float64) *float64 { return &v }

func normalizeModelKey(m string) string {
	return strings.ToLower(strings.TrimSpace(m))
}

// OPENAI_NO_TEMPERATURE_MODELS: comma-separated list, supports "*" suffix for prefix match.
// Examples:
// - "o1-* , o3-*"
// - "gpt-5, gpt-5-chat-latest"
func parseNoTempModelRules(raw string) (map[string]bool, []string) {
	m := map[string]bool{}
	var prefixes []string
	for _, part := range strings.Split(raw, ",") {
		s := normalizeModelKey(part)
		if s == "" {
			continue
		}
		if strings.HasSuffix(s, "*") {
			p := strings.TrimSuffix(s, "*")
			p = strings.TrimSpace(strings.TrimRight(p, "-_./:"))
			if p != "" {
				prefixes = append(prefixes, p)
			}
			continue
		}
		m[s] = true
	}
	return m, prefixes
}

func (c *client) modelIsNoTemp(model string) bool {
	m := normalizeModelKey(model)
	if m == "" {
		return false
	}

	// Static rules (env).
	if c.noTempModels != nil && c.noTempModels[m] {
		return true
	}
	for _, p := range c.noTempPrefixes {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(m, p) {
			return true
		}
	}

	// Learned rules (runtime).
	c.noTempMu.RLock()
	ts, ok := c.noTempSeen[m]
	ttl := c.noTempTTL
	c.noTempMu.RUnlock()
	if !ok {
		return false
	}
	if ttl <= 0 {
		return true
	}
	// If within TTL, treat as no-temp.
	if time.Since(ts) < ttl {
		return true
	}
	// Expired: allow again.
	return false
}

func (c *client) noteNoTempModel(model string) {
	m := normalizeModelKey(model)
	if m == "" {
		return
	}
	c.noTempMu.Lock()
	if c.noTempSeen == nil {
		c.noTempSeen = map[string]time.Time{}
	}
	c.noTempSeen[m] = time.Now().UTC()
	c.noTempMu.Unlock()
}

func (c *client) applyTemperature(req *responsesRequest) {
	if req == nil {
		return
	}
	if c.disableTemperature || c.temperature == nil {
		return
	}
	if c.modelIsNoTemp(req.Model) {
		return
	}
	req.Temperature = c.temperature
}

type openAIHTTPError struct {
	StatusCode int
	Body       string
}

func (e *openAIHTTPError) Error() string {
	return fmt.Sprintf("openai http %d: %s", e.StatusCode, e.Body)
}

func (e *openAIHTTPError) HTTPStatusCode() int {
	if e == nil {
		return 0
	}
	return e.StatusCode
}

func isUnsupportedTemperatureMessage(s string) bool {
	msg := strings.ToLower(strings.TrimSpace(s))
	if msg == "" {
		return false
	}
	if !strings.Contains(msg, "temperature") {
		return false
	}
	// Match common variants seen across OpenAI / OpenAI-compatible endpoints.
	if strings.Contains(msg, "unsupported parameter") {
		return true
	}
	if strings.Contains(msg, "unknown parameter") {
		return true
	}
	if strings.Contains(msg, "unrecognized parameter") {
		return true
	}
	if strings.Contains(msg, "not supported") {
		return true
	}
	if strings.Contains(msg, "does not support") {
		return true
	}
	if strings.Contains(msg, "only the default") {
		return true
	}
	if strings.Contains(msg, "unsupported_value") || strings.Contains(msg, "invalid_request_error") {
		return true
	}
	return false
}

func isUnsupportedTemperatureParam(err error) bool {
	if err == nil {
		return false
	}
	return isUnsupportedTemperatureMessage(err.Error())
}

func (c *client) doOnce(ctx context.Context, httpClient *http.Client, method, path string, body any) (*http.Response, []byte, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, nil, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, &buf)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	if httpClient == nil {
		httpClient = c.httpClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}

	raw, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return resp, nil, readErr
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp, raw, &openAIHTTPError{StatusCode: resp.StatusCode, Body: string(raw)}
	}
	return resp, raw, nil
}

func (c *client) doWithClient(ctx context.Context, httpClient *http.Client, method, path string, body any, out any) error {
	backoff := 1 * time.Second
	start := time.Now()
	model := extractModelFromRequest(body)

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		resp, raw, err := c.doOnce(ctx, httpClient, method, path, body)
		if err == nil {
			if metrics := observability.Current(); metrics != nil {
				inputTokens, outputTokens := extractUsageFromRaw(raw)
				metrics.ObserveLLMRequest(model, path, statusFromResp(resp), time.Since(start), inputTokens, outputTokens)
			}
			if out == nil {
				return nil
			}
			if uErr := json.Unmarshal(raw, out); uErr != nil {
				return fmt.Errorf("openai decode error: %w; raw=%s", uErr, string(raw))
			}
			return nil
		}

		if !httpx.IsRetryableError(err) {
			if metrics := observability.Current(); metrics != nil {
				metrics.ObserveLLMRequest(model, path, statusFromRespErr(resp, err), time.Since(start), 0, 0)
			}
			return err
		}
		if attempt == c.maxRetries {
			if metrics := observability.Current(); metrics != nil {
				metrics.ObserveLLMRequest(model, path, statusFromRespErr(resp, err), time.Since(start), 0, 0)
			}
			return err
		}

		sleepFor := httpx.RetryAfterDuration(resp, backoff, 10*time.Second)
		sleepFor = httpx.JitterSleep(sleepFor)

		c.log.Warn("OpenAI request retrying",
			"path", path,
			"attempt", attempt+1,
			"max_retries", c.maxRetries,
			"sleep", sleepFor.String(),
			"error", err.Error(),
		)

		time.Sleep(sleepFor)
		backoff *= 2
	}

	return fmt.Errorf("unreachable retry loop")
}

func (c *client) do(ctx context.Context, method, path string, body any, out any) error {
	return c.doWithClient(ctx, c.httpClient, method, path, body, out)
}

func (c *client) doResponses(ctx context.Context, method, path string, body any, out any) error {
	httpClient := c.responsesClient
	if httpClient == nil {
		httpClient = c.httpClient
	}
	return c.doWithClient(ctx, httpClient, method, path, body, out)
}

// doResponsesWithTempFallback retries exactly once without temperature if the model rejects it.
func (c *client) doResponsesWithTempFallback(ctx context.Context, method, path string, req *responsesRequest, out any) error {
	if req == nil {
		return c.doResponses(ctx, method, path, nil, out)
	}
	err := c.doResponses(ctx, method, path, req, out)
	if err == nil {
		return nil
	}
	if req.Temperature == nil {
		return err
	}
	if !isUnsupportedTemperatureParam(err) {
		return err
	}

	// Learn + retry once without temperature.
	c.noteNoTempModel(req.Model)
	req.Temperature = nil
	return c.doResponses(ctx, method, path, req, out)
}

// -------------------- Embeddings --------------------

type embeddingsRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingsResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

func (c *client) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return [][]float32{}, nil
	}

	clean := make([]string, len(inputs))
	for i := range inputs {
		s := strings.TrimSpace(inputs[i])
		if s == "" {
			s = " "
		}
		clean[i] = s
	}

	req := embeddingsRequest{
		Model: c.embedModel,
		Input: clean,
	}

	var resp embeddingsResponse
	if err := c.do(ctx, "POST", "/v1/embeddings", req, &resp); err != nil {
		return nil, err
	}

	out := make([][]float32, len(clean))

	for _, d := range resp.Data {
		vec := make([]float32, len(d.Embedding))
		for i, f := range d.Embedding {
			vec[i] = float32(f)
		}
		if d.Index >= 0 && d.Index < len(out) {
			out[d.Index] = vec
		}
	}

	if hasMissingEmbeddings(out) && len(resp.Data) == len(clean) {
		for i := 0; i < len(clean); i++ {
			if out[i] != nil {
				continue
			}
			d := resp.Data[i]
			vec := make([]float32, len(d.Embedding))
			for j, f := range d.Embedding {
				vec[j] = float32(f)
			}
			out[i] = vec
		}
	}

	if hasMissingEmbeddings(out) {
		c.log.Warn("Embeddings response missing indices; retrying once",
			"requested", len(clean),
			"returned", len(resp.Data),
			"model", c.embedModel,
		)

		var resp2 embeddingsResponse
		if err := c.do(ctx, "POST", "/v1/embeddings", req, &resp2); err != nil {
			return nil, err
		}

		out2 := make([][]float32, len(clean))
		for _, d := range resp2.Data {
			vec := make([]float32, len(d.Embedding))
			for i, f := range d.Embedding {
				vec[i] = float32(f)
			}
			if d.Index >= 0 && d.Index < len(out2) {
				out2[d.Index] = vec
			}
		}
		if hasMissingEmbeddings(out2) && len(resp2.Data) == len(clean) {
			for i := 0; i < len(clean); i++ {
				if out2[i] != nil {
					continue
				}
				d := resp2.Data[i]
				vec := make([]float32, len(d.Embedding))
				for j, f := range d.Embedding {
					vec[j] = float32(f)
				}
				out2[i] = vec
			}
		}

		if hasMissingEmbeddings(out2) {
			return nil, fmt.Errorf("openai embeddings missing indices after retry: requested=%d returned=%d model=%s", len(clean), len(resp2.Data), c.embedModel)
		}
		return out2, nil
	}

	return out, nil
}

func hasMissingEmbeddings(v [][]float32) bool {
	for i := range v {
		if v[i] == nil || len(v[i]) == 0 {
			return true
		}
	}
	return false
}

// -------------------- Images API --------------------

type imagesGenerationRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	N              int    `json:"n,omitempty"`
	Size           string `json:"size,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"` // b64_json|url
}

type imagesGenerationResponse struct {
	Data []struct {
		B64JSON       string `json:"b64_json"`
		URL           string `json:"url"`
		RevisedPrompt string `json:"revised_prompt"`
	} `json:"data"`
}

func isUnknownResponseFormatParam(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unknown parameter") && strings.Contains(msg, "response_format")
}

func (c *client) GenerateImage(ctx context.Context, prompt string) (ImageGeneration, error) {
	var out ImageGeneration
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return out, errors.New("image prompt required")
	}
	if strings.TrimSpace(c.imageModel) == "" {
		return out, errors.New("missing OPENAI_IMAGE_MODEL")
	}

	responseFormat := "b64_json"
	if strings.HasPrefix(strings.ToLower(c.imageModel), "gpt-image-") {
		responseFormat = ""
	}
	req := imagesGenerationRequest{
		Model:          c.imageModel,
		Prompt:         prompt,
		N:              1,
		Size:           strings.TrimSpace(c.imageSize),
		ResponseFormat: responseFormat,
	}

	var resp imagesGenerationResponse
	if err := c.do(ctx, "POST", "/v1/images/generations", req, &resp); err != nil {
		if isUnknownResponseFormatParam(err) {
			req.ResponseFormat = ""
			if err2 := c.do(ctx, "POST", "/v1/images/generations", req, &resp); err2 != nil {
				return out, err2
			}
		} else {
			return out, err
		}
	}
	if len(resp.Data) == 0 {
		return out, errors.New("no image returned")
	}
	item := resp.Data[0]
	out.RevisedPrompt = strings.TrimSpace(item.RevisedPrompt)
	b64 := strings.TrimSpace(item.B64JSON)
	if b64 == "" {
		if u := strings.TrimSpace(item.URL); u != "" {
			b, ct, err := c.downloadBytes(ctx, u)
			if err != nil {
				return out, fmt.Errorf("download generated image: %w", err)
			}
			out.Bytes = b
			if strings.TrimSpace(ct) != "" {
				out.MimeType = strings.TrimSpace(strings.Split(ct, ";")[0])
			}
			if out.MimeType == "" {
				out.MimeType = "image/png"
			}
			return out, nil
		}
		return out, errors.New("image response missing b64_json and url")
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw) == 0 {
		return out, fmt.Errorf("decode image base64: %w", err)
	}
	out.Bytes = raw
	out.MimeType = "image/png"
	return out, nil
}

// -------------------- Videos API (Sora) --------------------

type videoJobResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func normalizeVideoDurationSeconds(dur int) int {
	if dur <= 0 {
		return 8
	}
	allowed := []int{4, 8, 12}
	best := allowed[0]
	bestDiff := absInt(dur - best)
	for _, v := range allowed[1:] {
		diff := absInt(dur - v)
		if diff < bestDiff {
			best = v
			bestDiff = diff
		}
	}
	return best
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (c *client) createVideoJob(ctx context.Context, prompt, model, size string, seconds int) (videoJobResponse, error) {
	var out videoJobResponse
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("prompt", prompt)
	_ = writer.WriteField("model", model)
	if strings.TrimSpace(size) != "" {
		_ = writer.WriteField("size", size)
	}
	if seconds > 0 {
		_ = writer.WriteField("seconds", strconv.Itoa(seconds))
	}
	_ = writer.Close()

	payload := buf.Bytes()
	contentType := writer.FormDataContentType()
	if err := c.doMultipart(ctx, "POST", "/v1/videos", payload, contentType, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *client) getVideoJob(ctx context.Context, id string) (videoJobResponse, error) {
	var out videoJobResponse
	if strings.TrimSpace(id) == "" {
		return out, errors.New("video id required")
	}
	if err := c.do(ctx, "GET", "/v1/videos/"+id, nil, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *client) downloadVideoContent(ctx context.Context, id string) ([]byte, string, error) {
	if strings.TrimSpace(id) == "" {
		return nil, "", errors.New("video id required")
	}
	url := c.baseURL + "/v1/videos/" + id + "/content"
	return c.downloadBytes(ctx, url)
}

func (c *client) GenerateVideo(ctx context.Context, prompt string, opts VideoGenerationOptions) (VideoGeneration, error) {
	var out VideoGeneration
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return out, errors.New("video prompt required")
	}
	if strings.TrimSpace(c.videoModel) == "" {
		return out, errors.New("missing OPENAI_VIDEO_MODEL")
	}

	dur := normalizeVideoDurationSeconds(opts.DurationSeconds)

	size := strings.TrimSpace(opts.Size)
	if size == "" {
		size = strings.TrimSpace(c.videoSize)
	}
	if size == "" {
		size = "1280x720"
	}

	job, err := c.createVideoJob(ctx, prompt, c.videoModel, size, dur)
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(job.ID) == "" {
		return out, errors.New("video create missing id")
	}

	status := strings.ToLower(strings.TrimSpace(job.Status))
	if status == "" {
		status = "queued"
	}
	deadline := time.Now().Add(20 * time.Minute)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}

	for {
		if status == "completed" || status == "succeeded" {
			break
		}
		if status == "failed" || status == "canceled" {
			msg := "video generation failed"
			if job.Error != nil && strings.TrimSpace(job.Error.Message) != "" {
				msg = job.Error.Message
			}
			return out, errors.New(msg)
		}
		if time.Now().After(deadline) {
			return out, errors.New("video generation timeout")
		}

		select {
		case <-ctx.Done():
			return out, ctx.Err()
		case <-time.After(2 * time.Second):
		}

		job, err = c.getVideoJob(ctx, job.ID)
		if err != nil {
			return out, err
		}
		status = strings.ToLower(strings.TrimSpace(job.Status))
	}

	b, ct, err := c.downloadVideoContent(ctx, job.ID)
	if err != nil {
		return out, err
	}
	out.Bytes = b
	out.MimeType = strings.TrimSpace(strings.Split(ct, ";")[0])
	if out.MimeType == "" {
		out.MimeType = sniffVideoMime(b)
	}
	return out, nil
}

func (c *client) downloadBytes(ctx context.Context, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctxutil.Default(ctx), "GET", url, nil)
	if err != nil {
		return nil, "", err
	}
	// Only attach OpenAI auth when downloading from OpenAI-controlled hosts.
	// Signed blob URLs (e.g., from image generations) can break if we send an unrelated Authorization header.
	if shouldAttachOpenAIAuth(c.baseURL, url) {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	raw, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, "", readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", &openAIHTTPError{StatusCode: resp.StatusCode, Body: string(raw)}
	}
	return raw, strings.TrimSpace(resp.Header.Get("Content-Type")), nil
}

func shouldAttachOpenAIAuth(baseURL, rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u == nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" {
		return false
	}

	// Prefer the configured base URL host (supports proxies and Azure-style base URLs).
	if bu, err := url.Parse(strings.TrimSpace(baseURL)); err == nil && bu != nil {
		baseHost := strings.ToLower(strings.TrimSpace(bu.Hostname()))
		if baseHost != "" && host == baseHost {
			return true
		}
	}

	// Fallback allowlist for known OpenAI domains.
	if host == "openai.com" || strings.HasSuffix(host, ".openai.com") {
		return true
	}
	if host == "openai.azure.com" || strings.HasSuffix(host, ".openai.azure.com") {
		return true
	}
	return false
}

func (c *client) doMultipart(ctx context.Context, method, path string, payload []byte, contentType string, out any) error {
	backoff := 1 * time.Second

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		req, err := http.NewRequestWithContext(ctxutil.Default(ctx), method, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", contentType)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if attempt < c.maxRetries {
				time.Sleep(backoff)
				backoff *= 2
				continue
			}
			return err
		}

		raw, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return &openAIHTTPError{StatusCode: resp.StatusCode, Body: string(raw)}
		}
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(raw, out); err != nil {
			return err
		}
		return nil
	}
	return errors.New("openai multipart request failed")
}

func sniffVideoMime(b []byte) string {
	if len(b) >= 12 {
		// MP4 has an ftyp box near the start.
		if bytes.Contains(b[:12], []byte("ftyp")) {
			return "video/mp4"
		}
	}
	if len(b) >= 4 {
		// WebM/Matroska EBML header.
		if b[0] == 0x1A && b[1] == 0x45 && b[2] == 0xDF && b[3] == 0xA3 {
			return "video/webm"
		}
	}
	return "video/mp4"
}

// -------------------- Responses API (text + structured + multimodal) --------------------

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

	Temperature *float64 `json:"temperature,omitempty"`

	Stream bool `json:"stream,omitempty"`
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
	Usage   struct {
		InputTokens      int `json:"input_tokens"`
		OutputTokens     int `json:"output_tokens"`
		TotalTokens      int `json:"total_tokens"`
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage,omitempty"`
}

func extractOutputText(resp responsesResponse) string {
	var out strings.Builder
	for _, item := range resp.Output {
		if item.Type == "message" && item.Role == "assistant" {
			for _, c := range item.Content {
				if c.Type == "output_text" && c.Text != "" {
					out.WriteString(c.Text)
				}
			}
		}
	}
	return out.String()
}

func (c *client) GenerateJSON(ctx context.Context, system string, user string, schemaName string, schema map[string]any) (map[string]any, error) {
	if schemaName == "" {
		return nil, errors.New("schemaName required")
	}
	if schema == nil {
		return nil, errors.New("schema required")
	}
	system = promptstyle.ApplySystem(system, "json")

	req := responsesRequest{
		Model: c.model,
		Input: []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		}{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	}
	c.applyTemperature(&req)

	req.Text.Format = map[string]any{
		"type":   "json_schema",
		"name":   schemaName,
		"schema": schema,
		"strict": true,
	}

	var resp responsesResponse
	if err := c.doResponsesWithTempFallback(ctx, "POST", "/v1/responses", &req, &resp); err != nil {
		return nil, err
	}
	if resp.Refusal != "" {
		return nil, fmt.Errorf("model refused: %s", resp.Refusal)
	}

	jsonText := extractOutputText(resp)
	if strings.TrimSpace(jsonText) == "" {
		return nil, fmt.Errorf("no output_text found in response")
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(jsonText), &obj); err != nil {
		return nil, fmt.Errorf("failed to parse model JSON: %w; text=%s", err, jsonText)
	}
	return obj, nil
}

func (c *client) GenerateText(ctx context.Context, system string, user string) (string, error) {
	system = promptstyle.ApplySystem(system, "text")
	req := responsesRequest{
		Model: c.model,
		Input: []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		}{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	}
	c.applyTemperature(&req)

	var resp responsesResponse
	if err := c.doResponsesWithTempFallback(ctx, "POST", "/v1/responses", &req, &resp); err != nil {
		return "", err
	}
	if resp.Refusal != "" {
		return "", fmt.Errorf("model refused: %s", resp.Refusal)
	}

	text := extractOutputText(resp)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("no output_text found in response")
	}
	return text, nil
}

func (c *client) GenerateTextWithImages(ctx context.Context, system string, user string, images []ImageInput) (string, error) {
	system = promptstyle.ApplySystem(system, "text")
	content := make([]map[string]any, 0, 1+len(images))
	content = append(content, map[string]any{
		"type": "input_text",
		"text": user,
	})
	for _, img := range images {
		u := strings.TrimSpace(img.ImageURL)
		if u == "" {
			continue
		}
		item := map[string]any{
			"type":      "input_image",
			"image_url": u,
		}
		if strings.TrimSpace(img.Detail) != "" {
			item["detail"] = strings.TrimSpace(img.Detail)
		}
		content = append(content, item)
	}

	if len(content) == 1 {
		return c.GenerateText(ctx, system, user)
	}

	req := responsesRequest{
		Model: c.model,
		Input: []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		}{
			{Role: "system", Content: system},
			{Role: "user", Content: content},
		},
	}
	c.applyTemperature(&req)

	var resp responsesResponse
	if err := c.doResponsesWithTempFallback(ctx, "POST", "/v1/responses", &req, &resp); err != nil {
		return "", err
	}
	if resp.Refusal != "" {
		return "", fmt.Errorf("model refused: %s", resp.Refusal)
	}

	text := extractOutputText(resp)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("no output_text found in response")
	}
	return text, nil
}

// StreamText streams output_text deltas from the Responses API (no conversation state).
// It is best-effort: any non-empty delta is forwarded to onDelta and accumulated into the returned text.
func (c *client) StreamText(ctx context.Context, system string, user string, onDelta func(delta string)) (string, error) {
	system = promptstyle.ApplySystem(system, "text")
	reqBody := responsesRequest{
		Model: c.model,
		Input: []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		}{
			{Role: "system", Content: strings.TrimSpace(system)},
			{Role: "user", Content: user},
		},
		Stream: true,
	}
	c.applyTemperature(&reqBody)
	start := time.Now()
	inputTokens := estimateTokens(system) + estimateTokens(user)

	doStream := func(body responsesRequest) (*http.Response, []byte, error) {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, nil, err
		}

		req, err := http.NewRequestWithContext(ctxutil.Default(ctx), "POST", c.baseURL+"/v1/responses", &buf)
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, nil, err
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil, nil
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, raw, &openAIHTTPError{StatusCode: resp.StatusCode, Body: string(raw)}
	}

	resp, raw, err := doStream(reqBody)
	if err != nil {
		if reqBody.Temperature != nil && isUnsupportedTemperatureMessage(string(raw)) {
			c.noteNoTempModel(reqBody.Model)
			reqBody.Temperature = nil
			resp, raw, err = doStream(reqBody)
		}
	}
	if err != nil {
		if metrics := observability.Current(); metrics != nil {
			metrics.ObserveLLMRequest(reqBody.Model, "/v1/responses", statusFromRespErr(resp, err), time.Since(start), inputTokens, 0)
		}
		return "", err
	}
	defer resp.Body.Close()

	var full strings.Builder
	err = streamSSE(resp.Body, func(event string, data string) error {
		// OpenAI may send "data: [DONE]" style sentinels.
		if strings.TrimSpace(data) == "" || strings.TrimSpace(data) == "[DONE]" {
			return nil
		}

		var obj map[string]any
		if err := json.Unmarshal([]byte(data), &obj); err != nil {
			return nil
		}

		evt := strings.TrimSpace(event)
		if t, ok := obj["type"].(string); ok && strings.TrimSpace(t) != "" {
			evt = strings.TrimSpace(t)
		}

		if r, ok := obj["refusal"].(string); ok && strings.TrimSpace(r) != "" {
			return fmt.Errorf("model refused: %s", r)
		}
		if eAny, ok := obj["error"]; ok && eAny != nil {
			b, _ := json.Marshal(eAny)
			return fmt.Errorf("openai stream error: %s", string(b))
		}

		if d, ok := obj["delta"].(string); ok {
			d = strings.TrimRight(d, "\u0000")
			if d == "" {
				return nil
			}
			if strings.Contains(evt, "output_text.delta") {
				full.WriteString(d)
				if onDelta != nil {
					onDelta(d)
				}
			}
		}

		return nil
	})
	if err != nil {
		if metrics := observability.Current(); metrics != nil {
			metrics.ObserveLLMRequest(reqBody.Model, "/v1/responses", statusFromRespErr(resp, err), time.Since(start), inputTokens, estimateTokens(full.String()))
		}
		return "", err
	}
	if metrics := observability.Current(); metrics != nil {
		metrics.ObserveLLMRequest(reqBody.Model, "/v1/responses", statusFromResp(resp), time.Since(start), inputTokens, estimateTokens(full.String()))
	}
	return full.String(), nil
}

// -------------------- Conversations API --------------------

func (c *client) CreateConversation(ctx context.Context) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, "POST", "/v1/conversations", map[string]any{}, &out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.ID) == "" {
		return "", fmt.Errorf("openai create conversation: missing id")
	}
	return strings.TrimSpace(out.ID), nil
}

func (c *client) GenerateTextInConversation(ctx context.Context, conversationID string, instructions string, user string) (string, error) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return "", fmt.Errorf("conversation_id required")
	}
	instructions = promptstyle.ApplySystem(instructions, "text")

	req := responsesRequest{
		Model:        c.model,
		Conversation: conversationID,
		Instructions: strings.TrimSpace(instructions),
		Input: []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		}{
			{Role: "user", Content: user},
		},
	}
	c.applyTemperature(&req)

	var resp responsesResponse
	if err := c.doResponsesWithTempFallback(ctx, "POST", "/v1/responses", &req, &resp); err != nil {
		return "", err
	}
	if resp.Refusal != "" {
		return "", fmt.Errorf("model refused: %s", resp.Refusal)
	}

	text := extractOutputText(resp)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("no output_text found in response")
	}
	return text, nil
}

// StreamTextInConversation streams output_text deltas from the Responses API.
// It is best-effort: any non-empty delta is forwarded to onDelta and accumulated into the returned text.
func (c *client) StreamTextInConversation(ctx context.Context, conversationID string, instructions string, user string, onDelta func(delta string)) (string, error) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return "", fmt.Errorf("conversation_id required")
	}
	instructions = promptstyle.ApplySystem(instructions, "text")

	reqBody := responsesRequest{
		Model:        c.model,
		Conversation: conversationID,
		Instructions: strings.TrimSpace(instructions),
		Input: []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		}{
			{Role: "user", Content: user},
		},
		Stream: true,
	}
	c.applyTemperature(&reqBody)
	start := time.Now()
	inputTokens := estimateTokens(instructions) + estimateTokens(user)

	doStream := func(body responsesRequest) (*http.Response, []byte, error) {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, nil, err
		}

		req, err := http.NewRequestWithContext(ctxutil.Default(ctx), "POST", c.baseURL+"/v1/responses", &buf)
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, nil, err
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil, nil
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, raw, &openAIHTTPError{StatusCode: resp.StatusCode, Body: string(raw)}
	}

	resp, raw, err := doStream(reqBody)
	if err != nil {
		if reqBody.Temperature != nil && isUnsupportedTemperatureMessage(string(raw)) {
			c.noteNoTempModel(reqBody.Model)
			reqBody.Temperature = nil
			resp, raw, err = doStream(reqBody)
		}
	}
	if err != nil {
		if metrics := observability.Current(); metrics != nil {
			metrics.ObserveLLMRequest(reqBody.Model, "/v1/responses", statusFromRespErr(resp, err), time.Since(start), inputTokens, 0)
		}
		return "", err
	}
	defer resp.Body.Close()

	var full strings.Builder
	err = streamSSE(resp.Body, func(event string, data string) error {
		// OpenAI may send "data: [DONE]" style sentinels.
		if strings.TrimSpace(data) == "" || strings.TrimSpace(data) == "[DONE]" {
			return nil
		}

		// Some events are JSON objects with { delta: "..." }.
		var obj map[string]any
		if err := json.Unmarshal([]byte(data), &obj); err != nil {
			return nil
		}

		evt := strings.TrimSpace(event)
		if t, ok := obj["type"].(string); ok && strings.TrimSpace(t) != "" {
			evt = strings.TrimSpace(t)
		}

		// Refusal/error signals.
		if r, ok := obj["refusal"].(string); ok && strings.TrimSpace(r) != "" {
			return fmt.Errorf("model refused: %s", r)
		}
		if eAny, ok := obj["error"]; ok && eAny != nil {
			b, _ := json.Marshal(eAny)
			return fmt.Errorf("openai stream error: %s", string(b))
		}

		// Only forward deltas for delta events (or if the payload clearly carries a delta).
		if d, ok := obj["delta"].(string); ok {
			d = strings.TrimRight(d, "\u0000")
			if d == "" {
				return nil
			}
			if strings.Contains(evt, "output_text.delta") {
				full.WriteString(d)
				if onDelta != nil {
					onDelta(d)
				}
			}
		}

		return nil
	})
	if err != nil {
		if metrics := observability.Current(); metrics != nil {
			metrics.ObserveLLMRequest(reqBody.Model, "/v1/responses", statusFromRespErr(resp, err), time.Since(start), inputTokens, estimateTokens(full.String()))
		}
		return "", err
	}
	if metrics := observability.Current(); metrics != nil {
		metrics.ObserveLLMRequest(reqBody.Model, "/v1/responses", statusFromResp(resp), time.Since(start), inputTokens, estimateTokens(full.String()))
	}
	return full.String(), nil
}

func extractUsageFromRaw(raw []byte) (int, int) {
	if len(raw) == 0 {
		return 0, 0
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0, 0
	}
	usageAny, ok := payload["usage"]
	if !ok || usageAny == nil {
		return 0, 0
	}
	usage, ok := usageAny.(map[string]any)
	if !ok {
		return 0, 0
	}

	inTokens := intFromAny(usage["input_tokens"])
	outTokens := intFromAny(usage["output_tokens"])
	if inTokens == 0 && outTokens == 0 {
		inTokens = intFromAny(usage["prompt_tokens"])
		outTokens = intFromAny(usage["completion_tokens"])
	}
	if inTokens == 0 && outTokens == 0 {
		if total := intFromAny(usage["total_tokens"]); total > 0 {
			inTokens = total
		}
	}
	return inTokens, outTokens
}

func intFromAny(v any) int {
	switch val := v.(type) {
	case nil:
		return 0
	case int:
		return val
	case int32:
		return int(val)
	case int64:
		return int(val)
	case float32:
		return int(val)
	case float64:
		return int(val)
	case json.Number:
		if i, err := val.Int64(); err == nil {
			return int(i)
		}
		if f, err := val.Float64(); err == nil {
			return int(f)
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
			return i
		}
	}
	return 0
}

func extractModelFromRequest(body any) string {
	switch v := body.(type) {
	case nil:
		return ""
	case responsesRequest:
		return strings.TrimSpace(v.Model)
	case *responsesRequest:
		if v == nil {
			return ""
		}
		return strings.TrimSpace(v.Model)
	case embeddingsRequest:
		return strings.TrimSpace(v.Model)
	case *embeddingsRequest:
		if v == nil {
			return ""
		}
		return strings.TrimSpace(v.Model)
	case imagesGenerationRequest:
		return strings.TrimSpace(v.Model)
	case *imagesGenerationRequest:
		if v == nil {
			return ""
		}
		return strings.TrimSpace(v.Model)
	case map[string]any:
		if m, ok := v["model"].(string); ok {
			return strings.TrimSpace(m)
		}
	case map[string]string:
		if m, ok := v["model"]; ok {
			return strings.TrimSpace(m)
		}
	}

	b, err := json.Marshal(body)
	if err != nil {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(b, &payload); err != nil {
		return ""
	}
	if m, ok := payload["model"].(string); ok {
		return strings.TrimSpace(m)
	}
	return ""
}

func statusFromResp(resp *http.Response) string {
	if resp == nil {
		return "unknown"
	}
	return strconv.Itoa(resp.StatusCode)
}

func statusFromRespErr(resp *http.Response, err error) string {
	if resp != nil {
		return strconv.Itoa(resp.StatusCode)
	}
	var httpErr *openAIHTTPError
	if err != nil && errors.As(err, &httpErr) {
		return strconv.Itoa(httpErr.StatusCode)
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "error"
}

func estimateTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	runes := []rune(text)
	return int(math.Ceil(float64(len(runes)) / 4.0))
}

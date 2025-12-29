package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/pkg/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/pkg/httpx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
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

type client struct {
	log        *logger.Logger
	baseURL    string
	apiKey     string
	model      string
	embedModel string
	imageModel string
	imageSize  string
	videoModel string
	videoSize  string
	httpClient *http.Client

	maxRetries int
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

	maxRetries := 4
	if v := os.Getenv("OPENAI_MAX_RETRIES"); v != "" {
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && parsed >= 0 {
			maxRetries = parsed
		}
	}

	if log == nil {
		return nil, fmt.Errorf("logger required")
	}

	return &client{
		log:        log.With("service", "OpenAIClient"),
		baseURL:    baseURL,
		apiKey:     apiKey,
		model:      model,
		embedModel: embed,
		imageModel: imageModel,
		imageSize:  imageSize,
		videoModel: videoModel,
		videoSize:  videoSize,
		httpClient: &http.Client{Timeout: time.Duration(timeoutSec) * time.Second},
		maxRetries: maxRetries,
	}, nil
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

func (c *client) doOnce(ctx context.Context, method, path string, body any) (*http.Response, []byte, error) {
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

	resp, err := c.httpClient.Do(req)
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

func (c *client) do(ctx context.Context, method, path string, body any, out any) error {
	backoff := 1 * time.Second

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		resp, raw, err := c.doOnce(ctx, method, path, body)
		if err == nil {
			if out == nil {
				return nil
			}
			if uErr := json.Unmarshal(raw, out); uErr != nil {
				return fmt.Errorf("openai decode error: %w; raw=%s", uErr, string(raw))
			}
			return nil
		}

		if !httpx.IsRetryableError(err) {
			return err
		}
		if attempt == c.maxRetries {
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
	// Some endpoints may require auth; include it but safe for signed URLs too.
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

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

	Temperature float64 `json:"temperature,omitempty"`

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

	req := responsesRequest{
		Model: c.model,
		Input: []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		}{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: 0.2,
	}
	req.Text.Format = map[string]any{
		"type":   "json_schema",
		"name":   schemaName,
		"schema": schema,
		"strict": true,
	}

	var resp responsesResponse
	if err := c.do(ctx, "POST", "/v1/responses", req, &resp); err != nil {
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
	req := responsesRequest{
		Model: c.model,
		Input: []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		}{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: 0.2,
	}

	var resp responsesResponse
	if err := c.do(ctx, "POST", "/v1/responses", req, &resp); err != nil {
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
		Temperature: 0.2,
	}

	var resp responsesResponse
	if err := c.do(ctx, "POST", "/v1/responses", req, &resp); err != nil {
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
	reqBody := responsesRequest{
		Model: c.model,
		Input: []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		}{
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

	req, err := http.NewRequestWithContext(ctxutil.Default(ctx), "POST", c.baseURL+"/v1/responses", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return "", &openAIHTTPError{StatusCode: resp.StatusCode, Body: string(raw)}
	}

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
		return "", err
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
		Temperature: 0.2,
	}

	var resp responsesResponse
	if err := c.do(ctx, "POST", "/v1/responses", req, &resp); err != nil {
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
		Temperature: 0.2,
		Stream:      true,
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(reqBody); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctxutil.Default(ctx), "POST", c.baseURL+"/v1/responses", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return "", &openAIHTTPError{StatusCode: resp.StatusCode, Body: string(raw)}
	}

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
		return "", err
	}
	return full.String(), nil
}

func streamSSE(r io.Reader, onEvent func(event string, data string) error) error {
	br := bufio.NewReader(r)
	var (
		eventName string
		dataLines []string
	)

	flush := func() error {
		if len(dataLines) == 0 {
			eventName = ""
			return nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = nil
		ev := eventName
		eventName = ""

		// Capture deltas into out by reusing onEvent callback behavior.
		// We accumulate by intercepting onEvent's side effects via a wrapper.
		if onEvent == nil {
			return nil
		}
		// Wrap: if callback wants to write deltas, it can do so via closure.
		return onEvent(ev, data)
	}

	for {
		line, err := br.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				_ = flush()
				break
			}
			return err
		}
		line = strings.TrimRight(line, "\r\n")

		// Blank line ends event.
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}

		// Comment.
		if strings.HasPrefix(line, ":") {
			continue
		}

		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}

		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			continue
		}
	}

	return nil
}

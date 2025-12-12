package services

import (
  "bytes"
  "context"
  "encoding/json"
  "errors"
  "fmt"
  "io"
  "math/rand"
  "net"
  "net/http"
  "os"
  "strconv"
  "strings"
  "time"

  "github.com/yungbote/neurobridge-backend/internal/logger"
)

type OpenAIClient interface {
  Embed(ctx context.Context, inputs []string) ([][]float32, error)
  GenerateJSON(ctx context.Context, system string, user string, schemaName string, schema map[string]any) (map[string]any, error)
}

type openAIClient struct {
  log        *logger.Logger
  baseURL    string
  apiKey     string
  model      string
  embedModel string
  httpClient *http.Client

  maxRetries int
}

func NewOpenAIClient(log *logger.Logger) (OpenAIClient, error) {
  apiKey := os.Getenv("OPENAI_API_KEY")
  if apiKey == "" {
    return nil, fmt.Errorf("missing OPENAI_API_KEY")
  }

  baseURL := os.Getenv("OPENAI_BASE_URL")
  if baseURL == "" {
    baseURL = "https://api.openai.com"
  }

  model := os.Getenv("OPENAI_MODEL")
  if model == "" {
    model = "gpt-5.2"
  }

  embed := os.Getenv("OPENAI_EMBED_MODEL")
  if embed == "" {
    embed = "text-embedding-3-small"
  }

  // IMPORTANT: default timeout higher for production generation workloads
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

  return &openAIClient{
    log:        log.With("service", "OpenAIClient"),
    baseURL:    baseURL,
    apiKey:     apiKey,
    model:      model,
    embedModel: embed,
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

func isRetryableHTTP(code int) bool {
  if code == 408 || code == 429 {
    return true
  }
  if code >= 500 && code <= 599 {
    return true
  }
  return false
}

func isRetryableErr(err error) bool {
  if err == nil {
    return false
  }
  if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
    // if caller canceled, don't retry; if it's our timeout, we will retry anyway.
    // We can only distinguish reliably by checking ctx, which we do in call loop.
    return true
  }
  var netErr net.Error
  if errors.As(err, &netErr) {
    if netErr.Timeout() || netErr.Temporary() {
      return true
    }
  }
  var httpErr *openAIHTTPError
  if errors.As(err, &httpErr) {
    return isRetryableHTTP(httpErr.StatusCode)
  }
  return false
}

func jitterSleep(base time.Duration) time.Duration {
  // +/- 20%
  if base <= 0 {
    return 0
  }
  j := 0.2
  delta := base.Seconds() * j
  low := base.Seconds() - delta
  high := base.Seconds() + delta
  if low < 0 {
    low = 0
  }
  v := low + rand.Float64()*(high-low)
  return time.Duration(v * float64(time.Second))
}

func (c *openAIClient) doOnce(ctx context.Context, method, path string, body any) (*http.Response, []byte, error) {
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

func (c *openAIClient) do(ctx context.Context, method, path string, body any, out any) error {
  // exponential backoff: 1s, 2s, 4s, 8s (cap ~10s)
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

    // If non-retryable: fail immediately
    if !isRetryableErr(err) {
      return err
    }

    // If we've exhausted retries: return last error
    if attempt == c.maxRetries {
      return err
    }

    // Respect Retry-After when present
    sleepFor := backoff
    if resp != nil {
      ra := strings.TrimSpace(resp.Header.Get("Retry-After"))
      if ra != "" {
        if secs, parseErr := strconv.Atoi(ra); parseErr == nil && secs > 0 {
          sleepFor = time.Duration(secs) * time.Second
        }
      }
    }

    // Cap + jitter
    if sleepFor > 10*time.Second {
      sleepFor = 10 * time.Second
    }
    sleepFor = jitterSleep(sleepFor)

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

// ---- Embeddings ----

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

func (c *openAIClient) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
  if len(inputs) == 0 {
    return [][]float32{}, nil
  }
  req := embeddingsRequest{
    Model: c.embedModel,
    Input: inputs,
  }
  var resp embeddingsResponse
  if err := c.do(ctx, "POST", "/v1/embeddings", req, &resp); err != nil {
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
  for i := range out {
    if out[i] == nil {
      return nil, fmt.Errorf("missing embedding for index %d", i)
    }
  }
  return out, nil
}

// ---- Responses JSON (Structured Outputs via text.format json_schema) ----

type responsesRequest struct {
  Model string `json:"model"`
  Input []struct {
    Role    string `json:"role"`
    Content string `json:"content"`
  } `json:"input"`
  Text struct {
    Format map[string]any `json:"format"`
  } `json:"text"`
  Temperature float64 `json:"temperature,omitempty"`
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

func (c *openAIClient) GenerateJSON(ctx context.Context, system string, user string, schemaName string, schema map[string]any) (map[string]any, error) {
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
      Content string `json:"content"`
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

  var jsonText string
  for _, item := range resp.Output {
    if item.Type == "message" && item.Role == "assistant" {
      for _, c := range item.Content {
        if c.Type == "output_text" && c.Text != "" {
          jsonText += c.Text
        }
      }
    }
  }
  if jsonText == "" {
    return nil, fmt.Errorf("no output_text found in response")
  }

  var obj map[string]any
  if err := json.Unmarshal([]byte(jsonText), &obj); err != nil {
    return nil, fmt.Errorf("failed to parse model JSON: %w; text=%s", err, jsonText)
  }
  return obj, nil
}











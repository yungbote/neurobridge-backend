package oaihttp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/inference/config"
	"github.com/yungbote/neurobridge-backend/internal/inference/engine"
)

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestEmbeddings(t *testing.T) {
	cfg := config.EngineConfig{
		Type:           "oai_http",
		BaseURL:        "http://upstream",
		EmbeddingsPath: "/v1/embeddings",
		Timeout:        config.Duration{Duration: 2 * time.Second},
	}

	client := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/v1/embeddings" {
				t.Fatalf("unexpected path: %s", req.URL.Path)
			}

			var in embeddingsRequest
			if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
				t.Fatalf("decode req: %v", err)
			}

			if in.Model != "upstream-model" {
				t.Fatalf("model=%q", in.Model)
			}

			out := embeddingsResponse{
				Data: []struct {
					Embedding []float64 `json:"embedding"`
					Index     int       `json:"index"`
				}{
					{Embedding: []float64{0.1, 0.2}, Index: 0},
					{Embedding: []float64{0.3, 0.4}, Index: 1},
				},
			}

			b, _ := json.Marshal(out)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewReader(b)),
			}, nil
		}),
	}

	e, err := NewWithHTTPClient(cfg, client)
	if err != nil {
		t.Fatalf("NewWithHTTPClient: %v", err)
	}

	vecs, err := e.Embed(context.Background(), "upstream-model", []string{"a", "b"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("len=%d", len(vecs))
	}
	if len(vecs[0]) != 2 {
		t.Fatalf("dims=%d", len(vecs[0]))
	}
}

func TestGenerateText_JSONSchemaAutoRetries(t *testing.T) {
	var calls int32

	cfg := config.EngineConfig{
		Type:                "oai_http",
		BaseURL:             "http://upstream",
		ChatCompletionsPath: "/v1/chat/completions",
		Timeout:             config.Duration{Duration: 2 * time.Second},
		JSONSchema: config.JSONSchemaConfig{
			Mode:           "auto",
			MaxRetries:     2,
			MaxPromptBytes: 4096,
		},
	}

	client := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/v1/chat/completions" {
				t.Fatalf("unexpected path: %s", req.URL.Path)
			}

			n := atomic.AddInt32(&calls, 1)

			var payload map[string]any
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatalf("decode req: %v", err)
			}

			if n == 1 {
				if _, ok := payload["guided_json"]; !ok {
					t.Fatalf("expected guided_json on first attempt")
				}
				msgsAny, _ := payload["messages"].([]any)
				if len(msgsAny) != 2 {
					t.Fatalf("expected 2 messages on first attempt, got %d", len(msgsAny))
				}
				resp := chatCompletionResponse{
					Choices: []struct {
						Message struct {
							Content string `json:"content,omitempty"`
						} `json:"message,omitempty"`
						Text string `json:"text,omitempty"`
					}{
						{Message: struct {
							Content string `json:"content,omitempty"`
						}{Content: "not json"}},
					},
				}
				b, _ := json.Marshal(resp)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewReader(b)),
				}, nil
			}

			if _, ok := payload["guided_json"]; ok {
				t.Fatalf("did not expect guided_json on retry")
			}
			msgsAny, _ := payload["messages"].([]any)
			if len(msgsAny) != 3 {
				t.Fatalf("expected 3 messages on retry, got %d", len(msgsAny))
			}

			resp := chatCompletionResponse{
				Choices: []struct {
					Message struct {
						Content string `json:"content,omitempty"`
					} `json:"message,omitempty"`
					Text string `json:"text,omitempty"`
				}{
					{Message: struct {
						Content string `json:"content,omitempty"`
					}{Content: `{"ok":true}`}},
				},
			}
			b, _ := json.Marshal(resp)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewReader(b)),
			}, nil
		}),
	}

	e, err := NewWithHTTPClient(cfg, client)
	if err != nil {
		t.Fatalf("NewWithHTTPClient: %v", err)
	}

	out, err := e.GenerateText(context.Background(), "upstream-model", []engine.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "user"},
	}, engine.GenerateOptions{
		Temperature: 0,
		JSONSchema: &engine.JSONSchema{
			Name:   "test",
			Schema: map[string]any{"type": "object"},
			Strict: true,
		},
	})
	if err != nil {
		t.Fatalf("GenerateText: %v", err)
	}
	if strings.TrimSpace(out) != `{"ok":true}` {
		t.Fatalf("out=%q", out)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("calls=%d", got)
	}
}

func TestStreamText(t *testing.T) {
	cfg := config.EngineConfig{
		Type:                "oai_http",
		BaseURL:             "http://upstream",
		ChatCompletionsPath: "/v1/chat/completions",
		StreamTimeout:       config.Duration{Duration: 2 * time.Second},
	}

	client := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/v1/chat/completions" {
				t.Fatalf("unexpected path: %s", req.URL.Path)
			}
			if !strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
				t.Fatalf("accept=%q", req.Header.Get("Accept"))
			}
			sse := strings.Join([]string{
				`data: {"choices":[{"delta":{"content":"hel"}}]}`,
				"",
				`data: {"choices":[{"delta":{"content":"lo"}}]}`,
				"",
				"data: [DONE]",
				"",
				"",
			}, "\n")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(sse)),
			}, nil
		}),
	}

	e, err := NewWithHTTPClient(cfg, client)
	if err != nil {
		t.Fatalf("NewWithHTTPClient: %v", err)
	}

	var deltas strings.Builder
	full, err := e.StreamText(context.Background(), "upstream-model", []engine.Message{
		{Role: "user", Content: "hi"},
	}, engine.GenerateOptions{}, func(delta string) {
		deltas.WriteString(delta)
	})
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}
	if full != "hello" {
		t.Fatalf("full=%q", full)
	}
	if deltas.String() != "hello" {
		t.Fatalf("deltas=%q", deltas.String())
	}
}

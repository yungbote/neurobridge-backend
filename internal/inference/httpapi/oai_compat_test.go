package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yungbote/neurobridge-backend/internal/inference/config"
	"github.com/yungbote/neurobridge-backend/internal/inference/router"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

func testHandler(t *testing.T) http.Handler {
	t.Helper()

	cfg := &config.Config{
		Env: "development",
		HTTP: config.HTTPConfig{
			MaxRequestBytes: 1 << 20,
			EnableOAICompat: true,
		},
		Models: []config.ModelConfig{
			{ID: "mock-1", Engine: config.EngineConfig{Type: "mock"}},
		},
	}

	log, err := logger.New("development")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	r, err := router.New(cfg)
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	return NewHandler(cfg, log, r)
}

func TestModels(t *testing.T) {
	h := testHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/compat/oai/v1/models", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var out testModelsResponse
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Data) != 1 || out.Data[0].ID != "mock-1" {
		t.Fatalf("unexpected models: %+v", out)
	}
}

func TestEmbeddings(t *testing.T) {
	h := testHandler(t)

	reqBody := `{"model":"mock-1","input":["a","b"]}`
	req := httptest.NewRequest(http.MethodPost, "/compat/oai/v1/embeddings", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var out testEmbeddingsResponse
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Data) != 2 {
		t.Fatalf("unexpected embeddings length: %d", len(out.Data))
	}
	if len(out.Data[0].Embedding) == 0 {
		t.Fatalf("missing embedding vector")
	}
}

func TestResponsesNonStream(t *testing.T) {
	h := testHandler(t)

	reqBody := `{"model":"mock-1","input":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/compat/oai/v1/responses", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var out testResponsesResponse
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := extractOutputText(out); !strings.Contains(got, "mock:") {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestResponsesStream(t *testing.T) {
	h := testHandler(t)

	body := bytes.NewBufferString(`{"model":"mock-1","stream":true,"input":[{"role":"user","content":"hello"}]}`)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/compat/oai/v1/responses", body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	fr := &flushRecorder{ResponseRecorder: rr}
	h.ServeHTTP(fr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type=%q", ct)
	}

	sc := bufio.NewScanner(rr.Body)
	var sawDelta bool
	var sawDone bool
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "data: [DONE]" {
			sawDone = true
			break
		}
		if strings.HasPrefix(line, "data:") && strings.Contains(line, "output_text.delta") {
			sawDelta = true
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !sawDelta {
		t.Fatalf("expected at least one delta event")
	}
	if !sawDone {
		t.Fatalf("expected [DONE]")
	}
}

type flushRecorder struct {
	*httptest.ResponseRecorder
}

func (f *flushRecorder) Flush() {}

type testModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

type testEmbeddingsResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

type testResponsesResponse struct {
	Output []struct {
		Type    string `json:"type"`
		Role    string `json:"role,omitempty"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		} `json:"content,omitempty"`
	} `json:"output"`
}

func extractOutputText(resp testResponsesResponse) string {
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

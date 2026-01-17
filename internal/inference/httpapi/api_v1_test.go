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
)

func TestV1Models(t *testing.T) {
	h := testHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var out struct {
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Models) != 1 || out.Models[0].ID != "mock-1" {
		t.Fatalf("unexpected models: %+v", out)
	}
}

func TestV1Embeddings(t *testing.T) {
	h := testHandler(t)

	reqBody := `{"model":"mock-1","inputs":["a","b"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
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

func TestV1TextGenerateNonStream(t *testing.T) {
	h := testHandler(t)

	reqBody := `{"model":"mock-1","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/text/generate", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var out struct {
		OutputText string `json:"output_text"`
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := strings.TrimSpace(out.OutputText); !strings.Contains(got, "mock:") {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestV1TextGenerateStream(t *testing.T) {
	h := testHandler(t)

	body := bytes.NewBufferString(`{"model":"mock-1","stream":true,"messages":[{"role":"user","content":"hello"}]}`)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/text/generate", body)
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
		if strings.HasPrefix(line, "event: text.delta") {
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

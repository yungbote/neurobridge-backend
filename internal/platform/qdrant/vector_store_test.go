package qdrant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
)

func TestVectorStoreUpsertRequestShape(t *testing.T) {
	var captured map[string]any
	s := newTestVectorStore(t, func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPut {
			t.Fatalf("method: want=%s got=%s", http.MethodPut, r.Method)
		}
		if r.URL.Path != "/collections/neurobridge/points" {
			t.Fatalf("path: want=%q got=%q", "/collections/neurobridge/points", r.URL.Path)
		}
		if r.URL.RawQuery != "wait=true" {
			t.Fatalf("query: want=%q got=%q", "wait=true", r.URL.RawQuery)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		return okResponse(t, map[string]any{"status": "acknowledged"}), nil
	})

	meta := map[string]any{"type": "chunk"}
	err := s.Upsert(context.Background(), "lesson", []pinecone.Vector{
		{ID: "vec-1", Values: []float32{1, 2, 3}, Metadata: meta},
		{ID: "vec-2", Values: []float32{4, 5, 6}, Metadata: map[string]any{"type": "summary"}},
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	pointsRaw, ok := captured["points"].([]any)
	if !ok {
		t.Fatalf("points type: got=%T", captured["points"])
	}
	if len(pointsRaw) != 2 {
		t.Fatalf("points length: want=2 got=%d", len(pointsRaw))
	}

	first, ok := pointsRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("point[0] type: got=%T", pointsRaw[0])
	}
	if first["id"] != s.pointID("nb:lesson", "vec-1") {
		t.Fatalf("point id mismatch: got=%v", first["id"])
	}
	payload, ok := first["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload type: got=%T", first["payload"])
	}
	if payload[payloadNamespaceKey] != "nb:lesson" {
		t.Fatalf("payload namespace: want=%q got=%v", "nb:lesson", payload[payloadNamespaceKey])
	}
	if payload[payloadVectorIDKey] != "vec-1" {
		t.Fatalf("payload vector id: want=%q got=%v", "vec-1", payload[payloadVectorIDKey])
	}

	if _, exists := meta[payloadNamespaceKey]; exists {
		t.Fatalf("input metadata mutated: namespace key should not exist")
	}
	if _, exists := meta[payloadVectorIDKey]; exists {
		t.Fatalf("input metadata mutated: vector id key should not exist")
	}
}

func TestVectorStoreQueryMatchesFilterNamespaceAndScoreNormalization(t *testing.T) {
	var captured map[string]any
	s := newTestVectorStore(t, func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			t.Fatalf("method: want=%s got=%s", http.MethodPost, r.Method)
		}
		if r.URL.Path != "/collections/neurobridge/points/search" {
			t.Fatalf("path: want=%q got=%q", "/collections/neurobridge/points/search", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		return okResponse(t, []map[string]any{
			{
				"id":    "ignored-id-b",
				"score": 0.90,
				"payload": map[string]any{
					payloadVectorIDKey: "vec-b",
				},
			},
			{
				"id":    "ignored-id-a",
				"score": 0.10,
				"payload": map[string]any{
					payloadVectorIDKey: "vec-a",
				},
			},
		}), nil
	})
	s.distance = "euclid"

	matches, err := s.QueryMatches(context.Background(), "lesson", []float32{1, 2, 3}, 2, map[string]any{
		"material_file_id": map[string]any{
			"$in": []any{"file-1", "file-2"},
		},
	})
	if err != nil {
		t.Fatalf("QueryMatches: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("matches length: want=2 got=%d", len(matches))
	}
	if matches[0].ID != "vec-a" || matches[1].ID != "vec-b" {
		t.Fatalf("match ordering mismatch: got=%v", []string{matches[0].ID, matches[1].ID})
	}
	if !(matches[0].Score > matches[1].Score) {
		t.Fatalf("expected normalized descending scores, got=%v", []float64{matches[0].Score, matches[1].Score})
	}

	filter, ok := captured["filter"].(map[string]any)
	if !ok {
		t.Fatalf("filter type: got=%T", captured["filter"])
	}
	must, ok := filter["must"].([]any)
	if !ok {
		t.Fatalf("must type: got=%T", filter["must"])
	}
	nsCond := findConditionByKey(must, payloadNamespaceKey)
	if nsCond == nil {
		t.Fatalf("missing namespace condition in filter")
	}
	nsMatch, ok := nsCond["match"].(map[string]any)
	if !ok || nsMatch["value"] != "nb:lesson" {
		t.Fatalf("namespace match: got=%v", nsCond["match"])
	}

	fileCond := findConditionByKey(must, "material_file_id")
	if fileCond == nil {
		t.Fatalf("missing material_file_id condition")
	}
	fileMatch, ok := fileCond["match"].(map[string]any)
	if !ok {
		t.Fatalf("material_file_id match type: got=%T", fileCond["match"])
	}
	anyVals, ok := fileMatch["any"].([]any)
	if !ok {
		t.Fatalf("material_file_id any type: got=%T", fileMatch["any"])
	}
	if len(anyVals) != 2 {
		t.Fatalf("material_file_id any length: want=2 got=%d", len(anyVals))
	}
}

func TestVectorStoreQueryIDs(t *testing.T) {
	s := newTestVectorStore(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/collections/neurobridge/points/search" {
			t.Fatalf("path: want=%q got=%q", "/collections/neurobridge/points/search", r.URL.Path)
		}
		return okResponse(t, []map[string]any{
			{
				"id":    "ignored-2",
				"score": 0.20,
				"payload": map[string]any{
					payloadVectorIDKey: "vec-2",
				},
			},
			{
				"id":    "ignored-1",
				"score": 0.30,
				"payload": map[string]any{
					payloadVectorIDKey: "vec-1",
				},
			},
		}), nil
	})

	ids, err := s.QueryIDs(context.Background(), "lesson", []float32{1, 2, 3}, 5, nil)
	if err != nil {
		t.Fatalf("QueryIDs: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("ids length: want=2 got=%d", len(ids))
	}
	if ids[0] != "vec-1" || ids[1] != "vec-2" {
		t.Fatalf("ids mismatch: got=%v", ids)
	}
}

func TestVectorStoreDeleteIDsDedupesAndNamespacedPointIDs(t *testing.T) {
	var captured map[string]any
	s := newTestVectorStore(t, func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			t.Fatalf("method: want=%s got=%s", http.MethodPost, r.Method)
		}
		if r.URL.Path != "/collections/neurobridge/points/delete" {
			t.Fatalf("path: want=%q got=%q", "/collections/neurobridge/points/delete", r.URL.Path)
		}
		if r.URL.RawQuery != "wait=true" {
			t.Fatalf("query: want=%q got=%q", "wait=true", r.URL.RawQuery)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		return okResponse(t, map[string]any{"status": "acknowledged"}), nil
	})

	err := s.DeleteIDs(context.Background(), "lesson", []string{"vec-1", "vec-1", " ", "vec-2"})
	if err != nil {
		t.Fatalf("DeleteIDs: %v", err)
	}

	points, ok := captured["points"].([]any)
	if !ok {
		t.Fatalf("points type: got=%T", captured["points"])
	}
	if len(points) != 2 {
		t.Fatalf("points length: want=2 got=%d", len(points))
	}

	got := map[string]struct{}{}
	for _, p := range points {
		id, ok := p.(string)
		if !ok {
			t.Fatalf("point id type: got=%T", p)
		}
		got[id] = struct{}{}
	}
	wantA := s.pointID("nb:lesson", "vec-1")
	wantB := s.pointID("nb:lesson", "vec-2")
	if _, ok := got[wantA]; !ok {
		t.Fatalf("missing point id: %s", wantA)
	}
	if _, ok := got[wantB]; !ok {
		t.Fatalf("missing point id: %s", wantB)
	}
}

func TestVectorStoreQueryMatchesUnsupportedFilterError(t *testing.T) {
	s := &vectorStore{
		cfg:      Config{Collection: "neurobridge", VectorDim: 3},
		baseURL:  "http://qdrant.local",
		nsPrefix: "nb",
		http:     &http.Client{},
		log:      newTestLogger(t),
	}

	_, err := s.QueryMatches(context.Background(), "lesson", []float32{1, 2, 3}, 3, map[string]any{
		"type": map[string]any{
			"$gt": 1,
		},
	})
	if err == nil {
		t.Fatalf("QueryMatches: expected error, got nil")
	}
	var opErr *OperationError
	if !errors.As(err, &opErr) {
		t.Fatalf("expected OperationError, got=%T", err)
	}
	if opErr.Code != OperationErrorUnsupportedFilter {
		t.Fatalf("error code: want=%q got=%q", OperationErrorUnsupportedFilter, opErr.Code)
	}
}

func TestClassifyHTTPCallErrorTimeout(t *testing.T) {
	err := classifyHTTPCallError("query", "timeout", context.DeadlineExceeded)
	var opErr *OperationError
	if !errors.As(err, &opErr) {
		t.Fatalf("expected OperationError, got=%T", err)
	}
	if opErr.Code != OperationErrorTimeout {
		t.Fatalf("error code: want=%q got=%q", OperationErrorTimeout, opErr.Code)
	}
}

func TestClassifyHTTPCallErrorTransport(t *testing.T) {
	err := classifyHTTPCallError("query", "transport", fmt.Errorf("boom"))
	var opErr *OperationError
	if !errors.As(err, &opErr) {
		t.Fatalf("expected OperationError, got=%T", err)
	}
	if opErr.Code != OperationErrorTransportFailed {
		t.Fatalf("error code: want=%q got=%q", OperationErrorTransportFailed, opErr.Code)
	}
}

func newTestVectorStore(t *testing.T, roundTrip func(*http.Request) (*http.Response, error)) *vectorStore {
	t.Helper()
	client := &http.Client{
		Transport: roundTripFunc(roundTrip),
	}
	return &vectorStore{
		log:      newTestLogger(t),
		cfg:      Config{Collection: "neurobridge", VectorDim: 3},
		baseURL:  "http://qdrant.local",
		nsPrefix: "nb",
		http:     client,
		distance: "cosine",
	}
}

func newTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New("development")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	t.Cleanup(func() {
		log.Sync()
	})
	return log
}

func okResponse(t *testing.T, result any) *http.Response {
	t.Helper()
	payload := map[string]any{
		"result": result,
		"status": "ok",
		"time":   0.001,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(raw)),
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

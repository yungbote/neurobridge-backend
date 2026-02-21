package qdrant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
)

func TestVectorStoreIntegrationAgainstLocalQdrant(t *testing.T) {
	if !qdrantIntegrationEnabled() {
		t.Skip("set QDRANT_INTEGRATION=1 to run Qdrant integration tests")
	}

	baseURL := qdrantIntegrationURL()
	if err := waitForQdrantReady(baseURL); err != nil {
		t.Fatalf("qdrant not ready: %v", err)
	}

	collection := "nb_it_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := createIntegrationCollection(baseURL, collection, 3); err != nil {
		t.Fatalf("create collection: %v", err)
	}
	t.Cleanup(func() {
		_ = deleteIntegrationCollection(baseURL, collection)
	})

	log, err := logger.New("development")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	t.Cleanup(func() {
		log.Sync()
	})

	vs, err := NewVectorStore(log, Config{
		URL:             baseURL,
		Collection:      collection,
		NamespacePrefix: "it",
		VectorDim:       3,
	})
	if err != nil {
		t.Fatalf("NewVectorStore: %v", err)
	}

	ctx := context.Background()
	namespace := "phase5"
	if err := vs.Upsert(ctx, namespace, []pinecone.Vector{
		{
			ID:     "vec-1",
			Values: []float32{1, 0, 0},
			Metadata: map[string]any{
				"type":             "chunk",
				"material_file_id": "file-1",
			},
		},
		{
			ID:     "vec-2",
			Values: []float32{0, 1, 0},
			Metadata: map[string]any{
				"type":             "chunk",
				"material_file_id": "file-2",
			},
		},
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	filter := map[string]any{
		"type": "chunk",
		"material_file_id": map[string]any{
			"$in": []any{"file-1"},
		},
	}
	matches, err := vs.QueryMatches(ctx, namespace, []float32{1, 0, 0}, 5, filter)
	if err != nil {
		t.Fatalf("QueryMatches: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("QueryMatches: expected at least one match")
	}
	if matches[0].ID != "vec-1" {
		t.Fatalf("QueryMatches first id: want=%q got=%q", "vec-1", matches[0].ID)
	}

	ids, err := vs.QueryIDs(ctx, namespace, []float32{1, 0, 0}, 5, filter)
	if err != nil {
		t.Fatalf("QueryIDs: %v", err)
	}
	if len(ids) != 1 || ids[0] != "vec-1" {
		t.Fatalf("QueryIDs filtered: want=[vec-1] got=%v", ids)
	}

	if err := vs.DeleteIDs(ctx, namespace, []string{"vec-1"}); err != nil {
		t.Fatalf("DeleteIDs: %v", err)
	}
	allIDs, err := vs.QueryIDs(ctx, namespace, []float32{1, 0, 0}, 5, nil)
	if err != nil {
		t.Fatalf("QueryIDs after delete: %v", err)
	}
	if containsString(allIDs, "vec-1") {
		t.Fatalf("deleted vector still returned: ids=%v", allIDs)
	}
	if !containsString(allIDs, "vec-2") {
		t.Fatalf("expected remaining vec-2 after delete: ids=%v", allIDs)
	}
}

func qdrantIntegrationEnabled() bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv("QDRANT_INTEGRATION")))
	return raw == "1" || raw == "true" || raw == "yes"
}

func qdrantIntegrationURL() string {
	if url := strings.TrimSpace(os.Getenv("QDRANT_INTEGRATION_URL")); url != "" {
		return strings.TrimRight(url, "/")
	}
	if url := strings.TrimSpace(os.Getenv("QDRANT_URL")); url != "" {
		return strings.TrimRight(url, "/")
	}
	return "http://127.0.0.1:6333"
}

func waitForQdrantReady(baseURL string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	readyURL := baseURL + "/readyz"
	var lastErr error
	for i := 0; i < 20; i++ {
		req, err := http.NewRequest(http.MethodGet, readyURL, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil && resp != nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("status=%d", resp.StatusCode)
		} else if err != nil {
			lastErr = err
		}
		time.Sleep(200 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("timeout")
	}
	return fmt.Errorf("ready check failed for %s: %w", readyURL, lastErr)
}

func createIntegrationCollection(baseURL, collection string, dim int) error {
	body := map[string]any{
		"vectors": map[string]any{
			"size":     dim,
			"distance": "Cosine",
		},
	}
	return doQdrantCollectionRequest(http.MethodPut, baseURL, collection, body)
}

func deleteIntegrationCollection(baseURL, collection string) error {
	return doQdrantCollectionRequest(http.MethodDelete, baseURL, collection, nil)
}

func doQdrantCollectionRequest(method, baseURL, collection string, body map[string]any) error {
	var reader io.Reader
	if body != nil {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
		reader = &buf
	}

	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("%s/collections/%s", strings.TrimRight(baseURL, "/"), collection)
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("qdrant collection request failed: method=%s status=%d body=%q", method, resp.StatusCode, string(raw))
	}
	return nil
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}

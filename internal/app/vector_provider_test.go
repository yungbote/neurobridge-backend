package app

import (
	"context"
	"errors"
	"testing"

	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/platform/qdrant"
)

func TestResolveVectorStoreProviderQdrantSelected(t *testing.T) {
	log, err := logger.New("development")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	defer log.Sync()

	orig := newQdrantVectorStore
	t.Cleanup(func() {
		newQdrantVectorStore = orig
	})

	stubStore := &testVectorStore{}
	var captured qdrant.Config
	newQdrantVectorStore = func(_ *logger.Logger, cfg qdrant.Config) (pinecone.VectorStore, error) {
		captured = cfg
		return stubStore, nil
	}

	pc, vs, err := resolveVectorStoreProvider(log, Config{
		ObjectStorageMode:        "gcs_emulator",
		VectorProvider:           "qdrant",
		VectorProviderModeSource: "object_storage_mode_default",
		QdrantURL:                "http://qdrant:6333",
		QdrantCollection:         "neurobridge",
		QdrantNamespacePrefix:    "nb",
		QdrantVectorDim:          3072,
	})
	if err != nil {
		t.Fatalf("resolveVectorStoreProvider: %v", err)
	}
	if pc != nil {
		t.Fatalf("pinecone client: expected nil in qdrant mode")
	}
	if vs == nil {
		t.Fatalf("vector store: expected non-nil qdrant vector store")
	}
	if err := vs.Upsert(context.Background(), "ns", []pinecone.Vector{
		{ID: "vec-1", Values: []float32{1, 2, 3}},
	}); err != nil {
		t.Fatalf("vector store upsert: %v", err)
	}
	if stubStore.upsertCalls != 1 {
		t.Fatalf("underlying qdrant store not called; upsert_calls=%d", stubStore.upsertCalls)
	}
	if captured.URL != "http://qdrant:6333" {
		t.Fatalf("qdrant.URL: want=%q got=%q", "http://qdrant:6333", captured.URL)
	}
	if captured.Collection != "neurobridge" {
		t.Fatalf("qdrant.Collection: want=%q got=%q", "neurobridge", captured.Collection)
	}
}

func TestResolveVectorStoreProviderEmulatorNeverCallsPineconeInit(t *testing.T) {
	log, err := logger.New("development")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	defer log.Sync()

	origQdrant := newQdrantVectorStore
	origPineconeClient := newPineconeClient
	origPineconeVectorStore := newPineconeVectorStore
	t.Cleanup(func() {
		newQdrantVectorStore = origQdrant
		newPineconeClient = origPineconeClient
		newPineconeVectorStore = origPineconeVectorStore
	})

	qdrantCalls := 0
	pineconeClientCalls := 0
	pineconeVectorStoreCalls := 0
	newQdrantVectorStore = func(_ *logger.Logger, _ qdrant.Config) (pinecone.VectorStore, error) {
		qdrantCalls++
		return &testVectorStore{}, nil
	}
	newPineconeClient = func(_ *logger.Logger, _ pinecone.Config) (pinecone.Client, error) {
		pineconeClientCalls++
		return &testPineconeClient{}, nil
	}
	newPineconeVectorStore = func(_ *logger.Logger, _ pinecone.Client) (pinecone.VectorStore, error) {
		pineconeVectorStoreCalls++
		return &testVectorStore{}, nil
	}

	_, _, err = resolveVectorStoreProvider(log, Config{
		ObjectStorageMode:        "gcs_emulator",
		VectorProvider:           "qdrant",
		VectorProviderModeSource: "object_storage_mode_default",
		QdrantURL:                "http://qdrant:6333",
		QdrantCollection:         "neurobridge",
		QdrantNamespacePrefix:    "nb",
		QdrantVectorDim:          3072,
	})
	if err != nil {
		t.Fatalf("resolveVectorStoreProvider: %v", err)
	}
	if qdrantCalls != 1 {
		t.Fatalf("qdrant init call count: want=1 got=%d", qdrantCalls)
	}
	if pineconeClientCalls != 0 {
		t.Fatalf("pinecone client init should be skipped in emulator mode; calls=%d", pineconeClientCalls)
	}
	if pineconeVectorStoreCalls != 0 {
		t.Fatalf("pinecone vector store init should be skipped in emulator mode; calls=%d", pineconeVectorStoreCalls)
	}
}

func TestResolveVectorStoreProviderPineconeDisabledWhenNoAPIKey(t *testing.T) {
	log, err := logger.New("development")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	defer log.Sync()

	t.Setenv("PINECONE_API_KEY", "")

	pc, vs, err := resolveVectorStoreProvider(log, Config{
		ObjectStorageMode:        "gcs",
		VectorProvider:           "pinecone",
		VectorProviderModeSource: "object_storage_mode_default",
	})
	if err != nil {
		t.Fatalf("resolveVectorStoreProvider: %v", err)
	}
	if pc != nil {
		t.Fatalf("pinecone client: expected nil when API key missing")
	}
	if vs != nil {
		t.Fatalf("vector store: expected nil when API key missing")
	}
}

func TestResolveVectorStoreProviderCloudUsesPineconeAndSkipsQdrant(t *testing.T) {
	log, err := logger.New("development")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	defer log.Sync()

	t.Setenv("PINECONE_API_KEY", "test-key")
	t.Setenv("PINECONE_API_VERSION", "2025-10")
	t.Setenv("PINECONE_BASE_URL", "https://api.pinecone.io")

	origQdrant := newQdrantVectorStore
	origPineconeClient := newPineconeClient
	origPineconeVectorStore := newPineconeVectorStore
	t.Cleanup(func() {
		newQdrantVectorStore = origQdrant
		newPineconeClient = origPineconeClient
		newPineconeVectorStore = origPineconeVectorStore
	})

	qdrantCalls := 0
	pineconeClientCalls := 0
	pineconeVectorStoreCalls := 0
	var capturedCfg pinecone.Config
	fakeClient := &testPineconeClient{}
	stubStore := &testVectorStore{}

	newQdrantVectorStore = func(_ *logger.Logger, _ qdrant.Config) (pinecone.VectorStore, error) {
		qdrantCalls++
		return &testVectorStore{}, nil
	}
	newPineconeClient = func(_ *logger.Logger, cfg pinecone.Config) (pinecone.Client, error) {
		pineconeClientCalls++
		capturedCfg = cfg
		return fakeClient, nil
	}
	newPineconeVectorStore = func(_ *logger.Logger, client pinecone.Client) (pinecone.VectorStore, error) {
		pineconeVectorStoreCalls++
		if client != fakeClient {
			t.Fatalf("pinecone client mismatch")
		}
		return stubStore, nil
	}

	pc, vs, err := resolveVectorStoreProvider(log, Config{
		ObjectStorageMode:        "gcs",
		VectorProvider:           "pinecone",
		VectorProviderModeSource: "object_storage_mode_default",
	})
	if err != nil {
		t.Fatalf("resolveVectorStoreProvider: %v", err)
	}
	if pc != fakeClient {
		t.Fatalf("pinecone client mismatch")
	}
	if vs == nil {
		t.Fatalf("vector store: expected non-nil")
	}
	if err := vs.Upsert(context.Background(), "ns", []pinecone.Vector{
		{ID: "vec-1", Values: []float32{1, 2, 3}},
	}); err != nil {
		t.Fatalf("vector store upsert: %v", err)
	}
	if stubStore.upsertCalls != 1 {
		t.Fatalf("underlying pinecone store not called; upsert_calls=%d", stubStore.upsertCalls)
	}
	if qdrantCalls != 0 {
		t.Fatalf("qdrant init should be skipped in cloud pinecone mode; calls=%d", qdrantCalls)
	}
	if pineconeClientCalls != 1 {
		t.Fatalf("pinecone client init call count: want=1 got=%d", pineconeClientCalls)
	}
	if pineconeVectorStoreCalls != 1 {
		t.Fatalf("pinecone vector store init call count: want=1 got=%d", pineconeVectorStoreCalls)
	}
	if capturedCfg.APIKey != "test-key" {
		t.Fatalf("pinecone api key mismatch: got=%q", capturedCfg.APIKey)
	}
	if capturedCfg.APIVersion != "2025-10" {
		t.Fatalf("pinecone api version mismatch: got=%q", capturedCfg.APIVersion)
	}
	if capturedCfg.BaseURL != "https://api.pinecone.io" {
		t.Fatalf("pinecone base URL mismatch: got=%q", capturedCfg.BaseURL)
	}
}

func TestClassifyVectorProviderBootstrapErrorInvalidQdrantVectorDim(t *testing.T) {
	err := classifyVectorProviderBootstrapError(
		"qdrant",
		"gcs_emulator",
		&qdrant.ConfigError{Code: qdrant.ConfigErrorInvalidVectorDim},
	)
	var got *VectorProviderBootstrapError
	if !errors.As(err, &got) {
		t.Fatalf("expected VectorProviderBootstrapError, got=%T", err)
	}
	if got.Code != VectorProviderBootstrapErrorInvalidQdrantVector {
		t.Fatalf("code: want=%q got=%q", VectorProviderBootstrapErrorInvalidQdrantVector, got.Code)
	}
}

func TestResolveVectorStoreProviderInvalidProvider(t *testing.T) {
	log, err := logger.New("development")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	defer log.Sync()

	_, _, err = resolveVectorStoreProvider(log, Config{
		ObjectStorageMode:        "gcs",
		VectorProvider:           "bad-provider",
		VectorProviderModeSource: "object_storage_mode_default",
	})
	if err == nil {
		t.Fatalf("resolveVectorStoreProvider: expected error, got nil")
	}
	var got *VectorProviderBootstrapError
	if !errors.As(err, &got) {
		t.Fatalf("expected VectorProviderBootstrapError, got=%T", err)
	}
	if got.Code != VectorProviderBootstrapErrorInvalidProvider {
		t.Fatalf("code: want=%q got=%q", VectorProviderBootstrapErrorInvalidProvider, got.Code)
	}
}

type testVectorStore struct {
	upsertCalls int
}

func (t *testVectorStore) Upsert(ctx context.Context, namespace string, vectors []pinecone.Vector) error {
	t.upsertCalls++
	return nil
}

func (t *testVectorStore) QueryMatches(ctx context.Context, namespace string, q []float32, topK int, filter map[string]any) ([]pinecone.VectorMatch, error) {
	return nil, nil
}

func (t *testVectorStore) QueryIDs(ctx context.Context, namespace string, q []float32, topK int, filter map[string]any) ([]string, error) {
	return nil, nil
}

func (t *testVectorStore) DeleteIDs(ctx context.Context, namespace string, ids []string) error {
	return nil
}

type testPineconeClient struct{}

func (t *testPineconeClient) DescribeIndex(ctx context.Context, indexName string) (*pinecone.IndexDescription, error) {
	return &pinecone.IndexDescription{}, nil
}

func (t *testPineconeClient) UpsertVectors(ctx context.Context, host string, req pinecone.UpsertRequest) (*pinecone.UpsertResponse, error) {
	return &pinecone.UpsertResponse{}, nil
}

func (t *testPineconeClient) Query(ctx context.Context, host string, req pinecone.QueryRequest) (*pinecone.QueryResponse, error) {
	return &pinecone.QueryResponse{}, nil
}

func (t *testPineconeClient) DeleteVectors(ctx context.Context, host string, req pinecone.DeleteRequest) (*pinecone.DeleteResponse, error) {
	return &pinecone.DeleteResponse{}, nil
}

package app

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
)

func TestVectorProviderEmulatorModeSmokeUpsertAndQuery(t *testing.T) {
	if !vectorEmulatorSmokeEnabled() {
		t.Skip("set NB_RUN_VECTOR_EMULATOR_SMOKE=true to run vector emulator smoke tests")
	}

	_, vs := mustResolveEmulatorVectorStore(t)
	vectorID := smokeVectorID()
	namespace := smokeVectorNamespace()
	vector := smokeVectorValues(t)

	err := vs.Upsert(context.Background(), namespace, []pinecone.Vector{
		{
			ID:     vectorID,
			Values: vector,
			Metadata: map[string]any{
				"type":     "phase6_smoke",
				"smoke_id": vectorID,
			},
		},
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	ids, err := vs.QueryIDs(
		context.Background(),
		namespace,
		vector,
		3,
		map[string]any{
			"type":     "phase6_smoke",
			"smoke_id": vectorID,
		},
	)
	if err != nil {
		t.Fatalf("QueryIDs: %v", err)
	}
	if !containsStringID(ids, vectorID) {
		t.Fatalf("expected vector id %q in query results, got=%v", vectorID, ids)
	}
}

func TestVectorProviderEmulatorModeSmokeQueryExisting(t *testing.T) {
	if !vectorEmulatorSmokeEnabled() {
		t.Skip("set NB_RUN_VECTOR_EMULATOR_SMOKE=true to run vector emulator smoke tests")
	}

	_, vs := mustResolveEmulatorVectorStore(t)
	vectorID := smokeVectorID()
	namespace := smokeVectorNamespace()
	vector := smokeVectorValues(t)

	ids, err := vs.QueryIDs(
		context.Background(),
		namespace,
		vector,
		3,
		map[string]any{
			"type":     "phase6_smoke",
			"smoke_id": vectorID,
		},
	)
	if err != nil {
		t.Fatalf("QueryIDs: %v", err)
	}
	if !containsStringID(ids, vectorID) {
		t.Fatalf("expected persisted vector id %q after restart, got=%v", vectorID, ids)
	}
}

func TestVectorProviderEmulatorModeSmokeCleanup(t *testing.T) {
	if !vectorEmulatorSmokeEnabled() {
		t.Skip("set NB_RUN_VECTOR_EMULATOR_SMOKE=true to run vector emulator smoke tests")
	}

	_, vs := mustResolveEmulatorVectorStore(t)
	vectorID := smokeVectorID()
	namespace := smokeVectorNamespace()

	if err := vs.DeleteIDs(context.Background(), namespace, []string{vectorID}); err != nil {
		t.Fatalf("DeleteIDs: %v", err)
	}
}

func TestResolveVectorStoreProviderQdrantUnavailableIsExplicit(t *testing.T) {
	log, err := logger.New("development")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	defer log.Sync()

	_, _, err = resolveVectorStoreProvider(log, Config{
		ObjectStorageMode:        string(gcp.ObjectStorageModeGCSEmulator),
		VectorProvider:           string(VectorProviderQdrant),
		VectorProviderModeSource: "object_storage_mode_default",
		QdrantURL:                "http://127.0.0.1:65534",
		QdrantCollection:         "neurobridge",
		QdrantNamespacePrefix:    "nb",
		QdrantVectorDim:          3,
	})
	if err == nil {
		t.Fatalf("resolveVectorStoreProvider: expected error, got nil")
	}
	var bootErr *VectorProviderBootstrapError
	if !errors.As(err, &bootErr) {
		t.Fatalf("expected VectorProviderBootstrapError, got=%T", err)
	}
	if bootErr.Code != VectorProviderBootstrapErrorConnectFailed {
		t.Fatalf("error code: want=%q got=%q", VectorProviderBootstrapErrorConnectFailed, bootErr.Code)
	}
}

func mustResolveEmulatorVectorStore(t *testing.T) (pinecone.Client, pinecone.VectorStore) {
	t.Helper()

	qurl := strings.TrimSpace(os.Getenv("QDRANT_URL"))
	qcollection := strings.TrimSpace(os.Getenv("QDRANT_COLLECTION"))
	qprefix := strings.TrimSpace(os.Getenv("QDRANT_NAMESPACE_PREFIX"))
	qdimRaw := strings.TrimSpace(os.Getenv("QDRANT_VECTOR_DIM"))

	if qurl == "" {
		qurl = "http://127.0.0.1:6333"
		t.Setenv("QDRANT_URL", qurl)
	}
	if qcollection == "" {
		qcollection = "neurobridge"
		t.Setenv("QDRANT_COLLECTION", qcollection)
	}
	if qprefix == "" {
		qprefix = "nb"
		t.Setenv("QDRANT_NAMESPACE_PREFIX", qprefix)
	}
	if qdimRaw == "" {
		qdimRaw = "3072"
		t.Setenv("QDRANT_VECTOR_DIM", qdimRaw)
	}
	qdim, err := strconv.Atoi(qdimRaw)
	if err != nil || qdim <= 0 {
		t.Fatalf("invalid QDRANT_VECTOR_DIM=%q", qdimRaw)
	}

	modeCfg, err := resolveVectorProviderConfig(gcp.ObjectStorageModeGCSEmulator)
	if err != nil {
		t.Fatalf("resolveVectorProviderConfig: %v", err)
	}
	if modeCfg.Provider != VectorProviderQdrant {
		t.Fatalf("provider: want=%q got=%q", VectorProviderQdrant, modeCfg.Provider)
	}

	log, err := logger.New("development")
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	t.Cleanup(func() {
		log.Sync()
	})

	pc, vs, err := resolveVectorStoreProvider(log, Config{
		ObjectStorageMode:        string(gcp.ObjectStorageModeGCSEmulator),
		VectorProvider:           string(modeCfg.Provider),
		VectorProviderModeSource: modeCfg.ModeSource,
		QdrantURL:                qurl,
		QdrantCollection:         qcollection,
		QdrantNamespacePrefix:    qprefix,
		QdrantVectorDim:          qdim,
	})
	if err != nil {
		t.Fatalf("resolveVectorStoreProvider: %v", err)
	}
	if pc != nil {
		t.Fatalf("expected nil pinecone client in emulator mode")
	}
	if vs == nil {
		t.Fatalf("expected non-nil vector store in emulator mode")
	}
	return pc, vs
}

func vectorEmulatorSmokeEnabled() bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv("NB_RUN_VECTOR_EMULATOR_SMOKE")))
	return raw == "1" || raw == "true" || raw == "yes"
}

func smokeVectorNamespace() string {
	ns := strings.TrimSpace(os.Getenv("NB_VECTOR_SMOKE_NAMESPACE"))
	if ns == "" {
		return "phase6_smoke"
	}
	return ns
}

func smokeVectorID() string {
	id := strings.TrimSpace(os.Getenv("NB_VECTOR_SMOKE_ID"))
	if id == "" {
		return "phase6_smoke_vector"
	}
	return id
}

func smokeVectorValues(t *testing.T) []float32 {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("QDRANT_VECTOR_DIM"))
	if raw == "" {
		raw = "3072"
	}
	dim, err := strconv.Atoi(raw)
	if err != nil || dim <= 0 {
		t.Fatalf("invalid QDRANT_VECTOR_DIM=%q", raw)
	}
	if dim < 3 {
		t.Fatalf("QDRANT_VECTOR_DIM must be >= 3 for smoke test, got=%d", dim)
	}
	out := make([]float32, dim)
	out[0] = 1.0
	out[1] = 0.25
	out[2] = 0.125
	return out
}

func containsStringID(items []string, target string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}

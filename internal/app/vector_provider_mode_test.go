package app

import (
	"errors"
	"testing"

	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/qdrant"
)

func TestResolveVectorProviderConfigForGCS(t *testing.T) {
	cfg, err := resolveVectorProviderConfig(gcp.ObjectStorageModeGCS)
	if err != nil {
		t.Fatalf("resolveVectorProviderConfig: %v", err)
	}
	if cfg.Provider != VectorProviderPinecone {
		t.Fatalf("provider: want=%q got=%q", VectorProviderPinecone, cfg.Provider)
	}
	if cfg.ModeSource != "object_storage_mode_default" {
		t.Fatalf("mode source: want=%q got=%q", "object_storage_mode_default", cfg.ModeSource)
	}
}

func TestResolveVectorProviderConfigForGCSEmulator(t *testing.T) {
	t.Setenv("QDRANT_URL", "http://qdrant:6333")
	t.Setenv("QDRANT_COLLECTION", "neurobridge")
	t.Setenv("QDRANT_NAMESPACE_PREFIX", "nb")
	t.Setenv("QDRANT_VECTOR_DIM", "3072")

	cfg, err := resolveVectorProviderConfig(gcp.ObjectStorageModeGCSEmulator)
	if err != nil {
		t.Fatalf("resolveVectorProviderConfig: %v", err)
	}
	if cfg.Provider != VectorProviderQdrant {
		t.Fatalf("provider: want=%q got=%q", VectorProviderQdrant, cfg.Provider)
	}
	if cfg.ModeSource != "object_storage_mode_default" {
		t.Fatalf("mode source: want=%q got=%q", "object_storage_mode_default", cfg.ModeSource)
	}
	if cfg.Qdrant.URL != "http://qdrant:6333" {
		t.Fatalf("qdrant.URL: want=%q got=%q", "http://qdrant:6333", cfg.Qdrant.URL)
	}
	if cfg.Qdrant.Collection != "neurobridge" {
		t.Fatalf("qdrant.Collection: want=%q got=%q", "neurobridge", cfg.Qdrant.Collection)
	}
	if cfg.Qdrant.VectorDim != 3072 {
		t.Fatalf("qdrant.VectorDim: want=%d got=%d", 3072, cfg.Qdrant.VectorDim)
	}
}

func TestResolveVectorProviderConfigForGCSEmulatorMissingQdrantURL(t *testing.T) {
	t.Setenv("QDRANT_URL", "")
	t.Setenv("QDRANT_COLLECTION", "neurobridge")
	t.Setenv("QDRANT_VECTOR_DIM", "3072")

	_, err := resolveVectorProviderConfig(gcp.ObjectStorageModeGCSEmulator)
	if err == nil {
		t.Fatalf("resolveVectorProviderConfig: expected error, got nil")
	}
	var got *VectorProviderConfigError
	if !errors.As(err, &got) {
		t.Fatalf("expected VectorProviderConfigError, got=%T", err)
	}
	if got.Code != VectorProviderConfigErrorMissingQdrantURL {
		t.Fatalf("code: want=%q got=%q", VectorProviderConfigErrorMissingQdrantURL, got.Code)
	}
}

func TestMapVectorProviderConfigError(t *testing.T) {
	err := mapVectorProviderConfigError(
		gcp.ObjectStorageModeGCSEmulator,
		&qdrant.ConfigError{Code: qdrant.ConfigErrorMissingCollection},
	)
	var got *VectorProviderConfigError
	if !errors.As(err, &got) {
		t.Fatalf("expected VectorProviderConfigError, got=%T", err)
	}
	if got.Code != VectorProviderConfigErrorMissingQdrantColl {
		t.Fatalf("code: want=%q got=%q", VectorProviderConfigErrorMissingQdrantColl, got.Code)
	}
}

func TestResolveVectorProviderConfigInvalidStorageMode(t *testing.T) {
	_, err := resolveVectorProviderConfig(gcp.ObjectStorageMode("bogus"))
	if err == nil {
		t.Fatalf("resolveVectorProviderConfig: expected error, got nil")
	}
	var got *VectorProviderConfigError
	if !errors.As(err, &got) {
		t.Fatalf("expected VectorProviderConfigError, got=%T", err)
	}
	if got.Code != VectorProviderConfigErrorInvalidStorageMode {
		t.Fatalf("code: want=%q got=%q", VectorProviderConfigErrorInvalidStorageMode, got.Code)
	}
}

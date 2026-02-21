package qdrant

import "testing"

func TestResolveConfigFromEnvValid(t *testing.T) {
	t.Setenv("QDRANT_URL", "http://qdrant:6333")
	t.Setenv("QDRANT_COLLECTION", "neurobridge")
	t.Setenv("QDRANT_NAMESPACE_PREFIX", "nb")
	t.Setenv("QDRANT_VECTOR_DIM", "3072")

	cfg, err := ResolveConfigFromEnv()
	if err != nil {
		t.Fatalf("ResolveConfigFromEnv: %v", err)
	}
	if cfg.URL != "http://qdrant:6333" {
		t.Fatalf("URL: want=%q got=%q", "http://qdrant:6333", cfg.URL)
	}
	if cfg.Collection != "neurobridge" {
		t.Fatalf("Collection: want=%q got=%q", "neurobridge", cfg.Collection)
	}
	if cfg.NamespacePrefix != "nb" {
		t.Fatalf("NamespacePrefix: want=%q got=%q", "nb", cfg.NamespacePrefix)
	}
	if cfg.VectorDim != 3072 {
		t.Fatalf("VectorDim: want=%d got=%d", 3072, cfg.VectorDim)
	}
}

func TestResolveConfigFromEnvMissingURL(t *testing.T) {
	t.Setenv("QDRANT_URL", "")
	t.Setenv("QDRANT_COLLECTION", "neurobridge")
	t.Setenv("QDRANT_VECTOR_DIM", "3072")

	_, err := ResolveConfigFromEnv()
	if err == nil {
		t.Fatalf("ResolveConfigFromEnv: expected error, got nil")
	}
	cfgErr, ok := err.(*ConfigError)
	if !ok {
		t.Fatalf("expected *ConfigError, got=%T", err)
	}
	if cfgErr.Code != ConfigErrorMissingURL {
		t.Fatalf("code: want=%q got=%q", ConfigErrorMissingURL, cfgErr.Code)
	}
}

func TestResolveConfigFromEnvInvalidURL(t *testing.T) {
	t.Setenv("QDRANT_URL", "qdrant:6333")
	t.Setenv("QDRANT_COLLECTION", "neurobridge")
	t.Setenv("QDRANT_VECTOR_DIM", "3072")

	_, err := ResolveConfigFromEnv()
	if err == nil {
		t.Fatalf("ResolveConfigFromEnv: expected error, got nil")
	}
	cfgErr, ok := err.(*ConfigError)
	if !ok {
		t.Fatalf("expected *ConfigError, got=%T", err)
	}
	if cfgErr.Code != ConfigErrorInvalidURL {
		t.Fatalf("code: want=%q got=%q", ConfigErrorInvalidURL, cfgErr.Code)
	}
}

func TestResolveConfigFromEnvMissingCollection(t *testing.T) {
	t.Setenv("QDRANT_URL", "http://qdrant:6333")
	t.Setenv("QDRANT_COLLECTION", "")
	t.Setenv("QDRANT_VECTOR_DIM", "3072")

	_, err := ResolveConfigFromEnv()
	if err == nil {
		t.Fatalf("ResolveConfigFromEnv: expected error, got nil")
	}
	cfgErr, ok := err.(*ConfigError)
	if !ok {
		t.Fatalf("expected *ConfigError, got=%T", err)
	}
	if cfgErr.Code != ConfigErrorMissingCollection {
		t.Fatalf("code: want=%q got=%q", ConfigErrorMissingCollection, cfgErr.Code)
	}
}

func TestResolveConfigFromEnvMissingVectorDim(t *testing.T) {
	t.Setenv("QDRANT_URL", "http://qdrant:6333")
	t.Setenv("QDRANT_COLLECTION", "neurobridge")
	t.Setenv("QDRANT_VECTOR_DIM", "")

	_, err := ResolveConfigFromEnv()
	if err == nil {
		t.Fatalf("ResolveConfigFromEnv: expected error, got nil")
	}
	cfgErr, ok := err.(*ConfigError)
	if !ok {
		t.Fatalf("expected *ConfigError, got=%T", err)
	}
	if cfgErr.Code != ConfigErrorMissingVectorDim {
		t.Fatalf("code: want=%q got=%q", ConfigErrorMissingVectorDim, cfgErr.Code)
	}
}

func TestResolveConfigFromEnvInvalidVectorDim(t *testing.T) {
	t.Setenv("QDRANT_URL", "http://qdrant:6333")
	t.Setenv("QDRANT_COLLECTION", "neurobridge")
	t.Setenv("QDRANT_VECTOR_DIM", "0")

	_, err := ResolveConfigFromEnv()
	if err == nil {
		t.Fatalf("ResolveConfigFromEnv: expected error, got nil")
	}
	cfgErr, ok := err.(*ConfigError)
	if !ok {
		t.Fatalf("expected *ConfigError, got=%T", err)
	}
	if cfgErr.Code != ConfigErrorInvalidVectorDim {
		t.Fatalf("code: want=%q got=%q", ConfigErrorInvalidVectorDim, cfgErr.Code)
	}
}

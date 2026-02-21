package gcp

import (
	"testing"
)

func TestResolveObjectStorageConfigFromEnvDefaultGCS(t *testing.T) {
	t.Setenv("OBJECT_STORAGE_MODE", "")
	t.Setenv("STORAGE_EMULATOR_HOST", "")

	cfg, err := ResolveObjectStorageConfigFromEnv()
	if err != nil {
		t.Fatalf("ResolveObjectStorageConfigFromEnv: %v", err)
	}
	if cfg.Mode != ObjectStorageModeGCS {
		t.Fatalf("mode: want=%q got=%q", ObjectStorageModeGCS, cfg.Mode)
	}
	if cfg.CompatibilityFallback {
		t.Fatalf("compatibility fallback: want=false got=true")
	}
}

func TestResolveObjectStorageConfigFromEnvExplicitGCS(t *testing.T) {
	t.Setenv("OBJECT_STORAGE_MODE", "gcs")
	t.Setenv("STORAGE_EMULATOR_HOST", "http://fake-gcs:4443")

	cfg, err := ResolveObjectStorageConfigFromEnv()
	if err != nil {
		t.Fatalf("ResolveObjectStorageConfigFromEnv: %v", err)
	}
	if cfg.Mode != ObjectStorageModeGCS {
		t.Fatalf("mode: want=%q got=%q", ObjectStorageModeGCS, cfg.Mode)
	}
	if cfg.CompatibilityFallback {
		t.Fatalf("compatibility fallback: want=false got=true")
	}
}

func TestResolveObjectStorageConfigFromEnvExplicitEmulator(t *testing.T) {
	t.Setenv("OBJECT_STORAGE_MODE", "gcs_emulator")
	t.Setenv("STORAGE_EMULATOR_HOST", "http://fake-gcs:4443")

	cfg, err := ResolveObjectStorageConfigFromEnv()
	if err != nil {
		t.Fatalf("ResolveObjectStorageConfigFromEnv: %v", err)
	}
	if cfg.Mode != ObjectStorageModeGCSEmulator {
		t.Fatalf("mode: want=%q got=%q", ObjectStorageModeGCSEmulator, cfg.Mode)
	}
	if cfg.CompatibilityFallback {
		t.Fatalf("compatibility fallback: want=false got=true")
	}
}

func TestResolveObjectStorageConfigFromEnvCompatibilityFallback(t *testing.T) {
	t.Setenv("OBJECT_STORAGE_MODE", "")
	t.Setenv("STORAGE_EMULATOR_HOST", "http://fake-gcs:4443")

	cfg, err := ResolveObjectStorageConfigFromEnv()
	if err != nil {
		t.Fatalf("ResolveObjectStorageConfigFromEnv: %v", err)
	}
	if cfg.Mode != ObjectStorageModeGCSEmulator {
		t.Fatalf("mode: want=%q got=%q", ObjectStorageModeGCSEmulator, cfg.Mode)
	}
	if !cfg.CompatibilityFallback {
		t.Fatalf("compatibility fallback: want=true got=false")
	}
}

func TestResolveObjectStorageConfigFromEnvInvalidMode(t *testing.T) {
	t.Setenv("OBJECT_STORAGE_MODE", "local")
	t.Setenv("STORAGE_EMULATOR_HOST", "")

	_, err := ResolveObjectStorageConfigFromEnv()
	if err == nil {
		t.Fatalf("ResolveObjectStorageConfigFromEnv: expected error, got nil")
	}
}

func TestResolveObjectStorageConfigFromEnvMissingEmulatorHost(t *testing.T) {
	t.Setenv("OBJECT_STORAGE_MODE", "gcs_emulator")
	t.Setenv("STORAGE_EMULATOR_HOST", "")

	_, err := ResolveObjectStorageConfigFromEnv()
	if err == nil {
		t.Fatalf("ResolveObjectStorageConfigFromEnv: expected error, got nil")
	}
}

func TestResolveObjectStorageConfigFromEnvInvalidEmulatorHost(t *testing.T) {
	t.Setenv("OBJECT_STORAGE_MODE", "gcs_emulator")
	t.Setenv("STORAGE_EMULATOR_HOST", "fake-gcs:4443")

	_, err := ResolveObjectStorageConfigFromEnv()
	if err == nil {
		t.Fatalf("ResolveObjectStorageConfigFromEnv: expected error, got nil")
	}
}

package gcp

import "testing"

func TestObjectStorageModeHelpers(t *testing.T) {
	if !IsSupportedObjectStorageMode(ObjectStorageModeGCS) {
		t.Fatalf("ObjectStorageModeGCS should be supported")
	}
	if !IsSupportedObjectStorageMode(ObjectStorageModeGCSEmulator) {
		t.Fatalf("ObjectStorageModeGCSEmulator should be supported")
	}
	if IsSupportedObjectStorageMode(ObjectStorageMode("invalid")) {
		t.Fatalf("invalid mode should not be supported")
	}

	if IsEmulatorObjectStorageMode(ObjectStorageModeGCS) {
		t.Fatalf("ObjectStorageModeGCS should not be emulator mode")
	}
	if !IsEmulatorObjectStorageMode(ObjectStorageModeGCSEmulator) {
		t.Fatalf("ObjectStorageModeGCSEmulator should be emulator mode")
	}
}

func TestObjectStorageConfigHelpers(t *testing.T) {
	cfg := ObjectStorageConfig{Mode: ObjectStorageModeGCS}
	if cfg.IsEmulatorMode() {
		t.Fatalf("gcs config should not be emulator mode")
	}
	if got := cfg.ModeSource(); got != "explicit_or_default" {
		t.Fatalf("ModeSource: want=%q got=%q", "explicit_or_default", got)
	}

	cfg = ObjectStorageConfig{
		Mode:                  ObjectStorageModeGCSEmulator,
		CompatibilityFallback: true,
	}
	if !cfg.IsEmulatorMode() {
		t.Fatalf("gcs_emulator config should be emulator mode")
	}
	if got := cfg.ModeSource(); got != "compatibility_fallback" {
		t.Fatalf("ModeSource: want=%q got=%q", "compatibility_fallback", got)
	}
}

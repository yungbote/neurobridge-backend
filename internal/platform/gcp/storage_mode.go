package gcp

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

type ObjectStorageMode string

const (
	ObjectStorageModeGCS         ObjectStorageMode = "gcs"
	ObjectStorageModeGCSEmulator ObjectStorageMode = "gcs_emulator"
)

type ObjectStorageConfig struct {
	Mode                  ObjectStorageMode
	EmulatorHost          string
	CompatibilityFallback bool
}

func IsSupportedObjectStorageMode(mode ObjectStorageMode) bool {
	switch mode {
	case ObjectStorageModeGCS, ObjectStorageModeGCSEmulator:
		return true
	default:
		return false
	}
}

func IsEmulatorObjectStorageMode(mode ObjectStorageMode) bool {
	return mode == ObjectStorageModeGCSEmulator
}

func (cfg ObjectStorageConfig) IsEmulatorMode() bool {
	return IsEmulatorObjectStorageMode(cfg.Mode)
}

func (cfg ObjectStorageConfig) ModeSource() string {
	if cfg.CompatibilityFallback {
		return "compatibility_fallback"
	}
	return "explicit_or_default"
}

type ObjectStorageConfigErrorCode string

const (
	ObjectStorageConfigErrorInvalidMode         ObjectStorageConfigErrorCode = "invalid_mode"
	ObjectStorageConfigErrorMissingEmulatorHost ObjectStorageConfigErrorCode = "missing_emulator_host"
	ObjectStorageConfigErrorInvalidEmulatorHost ObjectStorageConfigErrorCode = "invalid_emulator_host"
)

type ObjectStorageConfigError struct {
	Code         ObjectStorageConfigErrorCode
	Mode         string
	EmulatorHost string
	Cause        error
}

func (e *ObjectStorageConfigError) Error() string {
	if e == nil {
		return "invalid object storage config"
	}
	switch e.Code {
	case ObjectStorageConfigErrorInvalidMode:
		return fmt.Sprintf(
			"invalid OBJECT_STORAGE_MODE=%q (allowed: %q, %q)",
			e.Mode,
			ObjectStorageModeGCS,
			ObjectStorageModeGCSEmulator,
		)
	case ObjectStorageConfigErrorMissingEmulatorHost:
		return fmt.Sprintf(
			"OBJECT_STORAGE_MODE=%q requires STORAGE_EMULATOR_HOST to be set",
			ObjectStorageModeGCSEmulator,
		)
	case ObjectStorageConfigErrorInvalidEmulatorHost:
		return fmt.Sprintf(
			"invalid STORAGE_EMULATOR_HOST=%q; expected absolute URL like http://fake-gcs:4443",
			e.EmulatorHost,
		)
	default:
		return "invalid object storage config"
	}
}

func (e *ObjectStorageConfigError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func ResolveObjectStorageConfigFromEnv() (ObjectStorageConfig, error) {
	cfg := ObjectStorageConfig{
		EmulatorHost: strings.TrimSpace(os.Getenv("STORAGE_EMULATOR_HOST")),
	}

	rawMode := strings.TrimSpace(os.Getenv("OBJECT_STORAGE_MODE"))
	mode := ObjectStorageMode(strings.ToLower(rawMode))

	switch mode {
	case "":
		if cfg.EmulatorHost != "" {
			cfg.Mode = ObjectStorageModeGCSEmulator
			cfg.CompatibilityFallback = true
		} else {
			cfg.Mode = ObjectStorageModeGCS
		}
	case ObjectStorageModeGCS:
		cfg.Mode = ObjectStorageModeGCS
	case ObjectStorageModeGCSEmulator:
		cfg.Mode = ObjectStorageModeGCSEmulator
	default:
		return cfg, &ObjectStorageConfigError{
			Code: ObjectStorageConfigErrorInvalidMode,
			Mode: rawMode,
		}
	}

	if err := ValidateObjectStorageConfig(cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}

func ValidateObjectStorageConfig(cfg ObjectStorageConfig) error {
	if !IsSupportedObjectStorageMode(cfg.Mode) {
		return &ObjectStorageConfigError{
			Code: ObjectStorageConfigErrorInvalidMode,
			Mode: string(cfg.Mode),
		}
	}
	if !cfg.IsEmulatorMode() {
		return nil
	}

	if cfg.EmulatorHost == "" {
		return &ObjectStorageConfigError{
			Code: ObjectStorageConfigErrorMissingEmulatorHost,
			Mode: string(cfg.Mode),
		}
	}
	u, err := url.Parse(cfg.EmulatorHost)
	if err != nil || strings.TrimSpace(u.Scheme) == "" || strings.TrimSpace(u.Host) == "" {
		return &ObjectStorageConfigError{
			Code:         ObjectStorageConfigErrorInvalidEmulatorHost,
			Mode:         string(cfg.Mode),
			EmulatorHost: cfg.EmulatorHost,
			Cause:        err,
		}
	}

	return nil
}

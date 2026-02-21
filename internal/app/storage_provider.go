package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/yungbote/neurobridge-backend/internal/observability"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

var newBucketServiceWithConfig = gcp.NewBucketServiceWithConfig

type StorageProviderBootstrapErrorCode string

const (
	StorageProviderBootstrapErrorInvalidMode         StorageProviderBootstrapErrorCode = "invalid_mode"
	StorageProviderBootstrapErrorMissingEmulatorHost StorageProviderBootstrapErrorCode = "missing_emulator_host"
	StorageProviderBootstrapErrorInvalidEmulatorHost StorageProviderBootstrapErrorCode = "invalid_emulator_host"
	StorageProviderBootstrapErrorConnectFailed       StorageProviderBootstrapErrorCode = "connect_failed"
)

type StorageProviderBootstrapError struct {
	Code         StorageProviderBootstrapErrorCode
	Mode         string
	EmulatorHost string
	Cause        error
}

func (e *StorageProviderBootstrapError) Error() string {
	if e == nil {
		return "object storage bootstrap failed"
	}
	return fmt.Sprintf(
		"object storage bootstrap failed (code=%s mode=%q emulator_host=%q): %v",
		e.Code,
		e.Mode,
		e.EmulatorHost,
		e.Cause,
	)
}

func (e *StorageProviderBootstrapError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func resolveBucketService(log *logger.Logger, cfg Config) (gcp.BucketService, error) {
	storageCfg := gcp.ObjectStorageConfig{
		Mode:                  gcp.ObjectStorageMode(strings.TrimSpace(cfg.ObjectStorageMode)),
		EmulatorHost:          strings.TrimSpace(cfg.StorageEmulatorHost),
		CompatibilityFallback: cfg.StorageModeCompatFallback,
	}
	modeSource := storageCfg.ModeSource()
	metrics := observability.Current()
	if metrics != nil {
		metrics.SetObjectStorageModeActive(string(storageCfg.Mode))
	}

	if !gcp.IsSupportedObjectStorageMode(storageCfg.Mode) {
		err := &StorageProviderBootstrapError{
			Code:         StorageProviderBootstrapErrorInvalidMode,
			Mode:         string(storageCfg.Mode),
			EmulatorHost: storageCfg.EmulatorHost,
			Cause:        fmt.Errorf("unsupported object storage mode %q", storageCfg.Mode),
		}
		if metrics != nil {
			metrics.ObserveObjectStorageProviderBootstrap(string(storageCfg.Mode), "error", string(err.Code))
		}
		log.Error(
			"Object storage provider selection failed",
			"mode", storageCfg.Mode,
			"mode_source", modeSource,
			"compatibility_fallback", storageCfg.CompatibilityFallback,
			"emulator_host", storageCfg.EmulatorHost,
			"error_code", err.Code,
			"error", err,
		)
		return nil, err
	}

	log.Info(
		"Selecting object storage provider",
		"mode", storageCfg.Mode,
		"mode_source", modeSource,
		"compatibility_fallback", storageCfg.CompatibilityFallback,
		"emulator_host", storageCfg.EmulatorHost,
	)

	bucket, err := newBucketServiceWithConfig(log, storageCfg)
	if err != nil {
		classified := classifyStorageProviderBootstrapError(storageCfg, err)
		code := storageProviderBootstrapErrorCode(classified)
		if metrics != nil {
			metrics.ObserveObjectStorageProviderBootstrap(string(storageCfg.Mode), "error", string(code))
		}
		log.Error(
			"Object storage provider bootstrap failed",
			"mode", storageCfg.Mode,
			"mode_source", modeSource,
			"compatibility_fallback", storageCfg.CompatibilityFallback,
			"emulator_host", storageCfg.EmulatorHost,
			"error_code", code,
			"error", classified,
		)
		return nil, classified
	}
	if metrics != nil {
		metrics.ObserveObjectStorageProviderBootstrap(string(storageCfg.Mode), "success", "none")
	}
	return bucket, nil
}

func classifyStorageProviderBootstrapError(storageCfg gcp.ObjectStorageConfig, err error) error {
	var cfgErr *gcp.ObjectStorageConfigError
	if errors.As(err, &cfgErr) {
		switch cfgErr.Code {
		case gcp.ObjectStorageConfigErrorInvalidMode:
			return &StorageProviderBootstrapError{
				Code:         StorageProviderBootstrapErrorInvalidMode,
				Mode:         string(storageCfg.Mode),
				EmulatorHost: storageCfg.EmulatorHost,
				Cause:        err,
			}
		case gcp.ObjectStorageConfigErrorMissingEmulatorHost:
			return &StorageProviderBootstrapError{
				Code:         StorageProviderBootstrapErrorMissingEmulatorHost,
				Mode:         string(storageCfg.Mode),
				EmulatorHost: storageCfg.EmulatorHost,
				Cause:        err,
			}
		case gcp.ObjectStorageConfigErrorInvalidEmulatorHost:
			return &StorageProviderBootstrapError{
				Code:         StorageProviderBootstrapErrorInvalidEmulatorHost,
				Mode:         string(storageCfg.Mode),
				EmulatorHost: storageCfg.EmulatorHost,
				Cause:        err,
			}
		}
	}

	return &StorageProviderBootstrapError{
		Code:         StorageProviderBootstrapErrorConnectFailed,
		Mode:         string(storageCfg.Mode),
		EmulatorHost: storageCfg.EmulatorHost,
		Cause:        err,
	}
}

func storageProviderBootstrapErrorCode(err error) StorageProviderBootstrapErrorCode {
	var bootstrapErr *StorageProviderBootstrapError
	if errors.As(err, &bootstrapErr) {
		if bootstrapErr.Code != "" {
			return bootstrapErr.Code
		}
	}
	return StorageProviderBootstrapErrorConnectFailed
}

package app

import (
	"errors"
	"fmt"

	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/qdrant"
)

type VectorProvider string

const (
	VectorProviderPinecone VectorProvider = "pinecone"
	VectorProviderQdrant   VectorProvider = "qdrant"
)

type VectorProviderConfigErrorCode string

const (
	VectorProviderConfigErrorInvalidStorageMode   VectorProviderConfigErrorCode = "invalid_storage_mode"
	VectorProviderConfigErrorMissingQdrantURL     VectorProviderConfigErrorCode = "missing_qdrant_url"
	VectorProviderConfigErrorInvalidQdrantURL     VectorProviderConfigErrorCode = "invalid_qdrant_url"
	VectorProviderConfigErrorMissingQdrantColl    VectorProviderConfigErrorCode = "missing_qdrant_collection"
	VectorProviderConfigErrorMissingQdrantVector  VectorProviderConfigErrorCode = "missing_qdrant_vector_dim"
	VectorProviderConfigErrorInvalidQdrantVector  VectorProviderConfigErrorCode = "invalid_qdrant_vector_dim"
	VectorProviderConfigErrorUnknownQdrantFailure VectorProviderConfigErrorCode = "qdrant_config_error"
)

type VectorProviderConfigError struct {
	Code        VectorProviderConfigErrorCode
	Provider    VectorProvider
	StorageMode string
	Cause       error
}

func (e *VectorProviderConfigError) Error() string {
	if e == nil {
		return "invalid vector provider config"
	}
	return fmt.Sprintf(
		"invalid vector provider config (code=%s provider=%q object_storage_mode=%q): %v",
		e.Code,
		e.Provider,
		e.StorageMode,
		e.Cause,
	)
}

func (e *VectorProviderConfigError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type VectorProviderConfig struct {
	Provider   VectorProvider
	ModeSource string
	Qdrant     qdrant.Config
}

func resolveVectorProviderConfig(storageMode gcp.ObjectStorageMode) (VectorProviderConfig, error) {
	switch storageMode {
	case gcp.ObjectStorageModeGCSEmulator:
		qcfg, err := qdrant.ResolveConfigFromEnv()
		if err != nil {
			return VectorProviderConfig{}, mapVectorProviderConfigError(storageMode, err)
		}
		return VectorProviderConfig{
			Provider:   VectorProviderQdrant,
			ModeSource: "object_storage_mode_default",
			Qdrant:     qcfg,
		}, nil
	case gcp.ObjectStorageModeGCS:
		return VectorProviderConfig{
			Provider:   VectorProviderPinecone,
			ModeSource: "object_storage_mode_default",
		}, nil
	default:
		return VectorProviderConfig{}, &VectorProviderConfigError{
			Code:        VectorProviderConfigErrorInvalidStorageMode,
			Provider:    "",
			StorageMode: string(storageMode),
			Cause:       fmt.Errorf("unsupported object storage mode %q", storageMode),
		}
	}
}

func mapVectorProviderConfigError(storageMode gcp.ObjectStorageMode, err error) error {
	var qerr *qdrant.ConfigError
	if errors.As(err, &qerr) {
		code := VectorProviderConfigErrorUnknownQdrantFailure
		switch qerr.Code {
		case qdrant.ConfigErrorMissingURL:
			code = VectorProviderConfigErrorMissingQdrantURL
		case qdrant.ConfigErrorInvalidURL:
			code = VectorProviderConfigErrorInvalidQdrantURL
		case qdrant.ConfigErrorMissingCollection:
			code = VectorProviderConfigErrorMissingQdrantColl
		case qdrant.ConfigErrorMissingVectorDim:
			code = VectorProviderConfigErrorMissingQdrantVector
		case qdrant.ConfigErrorInvalidVectorDim:
			code = VectorProviderConfigErrorInvalidQdrantVector
		}
		return &VectorProviderConfigError{
			Code:        code,
			Provider:    VectorProviderQdrant,
			StorageMode: string(storageMode),
			Cause:       err,
		}
	}
	return &VectorProviderConfigError{
		Code:        VectorProviderConfigErrorUnknownQdrantFailure,
		Provider:    VectorProviderQdrant,
		StorageMode: string(storageMode),
		Cause:       err,
	}
}

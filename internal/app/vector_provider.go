package app

import (
	"errors"
	"fmt"
	"net"
	neturl "net/url"
	"os"
	"strings"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/observability"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/platform/qdrant"
)

var (
	newPineconeClient      = pinecone.New
	newPineconeVectorStore = pinecone.NewVectorStore
	newQdrantVectorStore   = qdrant.NewVectorStore
)

type VectorProviderBootstrapErrorCode string

const (
	VectorProviderBootstrapErrorInvalidProvider      VectorProviderBootstrapErrorCode = "invalid_provider"
	VectorProviderBootstrapErrorMissingQdrantURL     VectorProviderBootstrapErrorCode = "missing_qdrant_url"
	VectorProviderBootstrapErrorInvalidQdrantURL     VectorProviderBootstrapErrorCode = "invalid_qdrant_url"
	VectorProviderBootstrapErrorMissingQdrantColl    VectorProviderBootstrapErrorCode = "missing_qdrant_collection"
	VectorProviderBootstrapErrorMissingQdrantVector  VectorProviderBootstrapErrorCode = "missing_qdrant_vector_dim"
	VectorProviderBootstrapErrorInvalidQdrantVector  VectorProviderBootstrapErrorCode = "invalid_qdrant_vector_dim"
	VectorProviderBootstrapErrorQdrantConfigFailed   VectorProviderBootstrapErrorCode = "qdrant_config_failed"
	VectorProviderBootstrapErrorConnectFailed        VectorProviderBootstrapErrorCode = "connect_failed"
	VectorProviderBootstrapErrorProviderInitFailed   VectorProviderBootstrapErrorCode = "provider_init_failed"
	VectorProviderBootstrapCodeDisabledMissingAPIKey VectorProviderBootstrapErrorCode = "disabled_missing_api_key"
)

type VectorProviderBootstrapError struct {
	Code              VectorProviderBootstrapErrorCode
	Provider          string
	ObjectStorageMode string
	Cause             error
}

func (e *VectorProviderBootstrapError) Error() string {
	if e == nil {
		return "vector provider bootstrap failed"
	}
	return fmt.Sprintf(
		"vector provider bootstrap failed (code=%s provider=%q object_storage_mode=%q): %v",
		e.Code,
		e.Provider,
		e.ObjectStorageMode,
		e.Cause,
	)
}

func (e *VectorProviderBootstrapError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func resolveVectorStoreProvider(
	log *logger.Logger,
	cfg Config,
) (pinecone.Client, pinecone.VectorStore, error) {
	mode := strings.TrimSpace(strings.ToLower(cfg.ObjectStorageMode))
	provider := strings.TrimSpace(strings.ToLower(cfg.VectorProvider))
	modeSource := strings.TrimSpace(cfg.VectorProviderModeSource)
	if modeSource == "" {
		modeSource = "object_storage_mode_default"
	}

	metrics := observability.Current()
	if metrics != nil {
		metrics.SetVectorStoreProviderActive(provider)
	}

	switch provider {
	case string(VectorProviderQdrant):
		log.Info(
			"Selecting vector store provider",
			"provider", provider,
			"object_storage_mode", mode,
			"provider_mode_source", modeSource,
			"qdrant_url", cfg.QdrantURL,
			"qdrant_collection", cfg.QdrantCollection,
			"qdrant_namespace_prefix", cfg.QdrantNamespacePrefix,
			"qdrant_vector_dim", cfg.QdrantVectorDim,
		)

		vs, err := newQdrantVectorStore(log, qdrant.Config{
			URL:             strings.TrimSpace(cfg.QdrantURL),
			Collection:      strings.TrimSpace(cfg.QdrantCollection),
			NamespacePrefix: strings.TrimSpace(cfg.QdrantNamespacePrefix),
			VectorDim:       cfg.QdrantVectorDim,
		})
		if err != nil {
			classified := classifyVectorProviderBootstrapError(provider, mode, err)
			code := vectorProviderBootstrapErrorCode(classified)
			if metrics != nil {
				metrics.ObserveVectorStoreProviderBootstrap(provider, "error", string(code))
			}
			log.Error(
				"Vector store provider bootstrap failed",
				"provider", provider,
				"object_storage_mode", mode,
				"provider_mode_source", modeSource,
				"error_code", code,
				"error", classified,
			)
			return nil, nil, classified
		}
		if metrics != nil {
			metrics.ObserveVectorStoreProviderBootstrap(provider, "success", "none")
		}
		return nil, instrumentVectorStore(provider, vs), nil

	case string(VectorProviderPinecone):
		log.Info(
			"Selecting vector store provider",
			"provider", provider,
			"object_storage_mode", mode,
			"provider_mode_source", modeSource,
		)

		if strings.TrimSpace(os.Getenv("PINECONE_API_KEY")) == "" {
			log.Warn("PINECONE_API_KEY not set; vector search disabled")
			if metrics != nil {
				metrics.SetVectorStoreProviderActive("disabled")
				metrics.ObserveVectorStoreProviderBootstrap(
					provider,
					"degraded",
					string(VectorProviderBootstrapCodeDisabledMissingAPIKey),
				)
			}
			return nil, nil, nil
		}

		pc, err := newPineconeClient(log, pinecone.Config{
			APIKey:     strings.TrimSpace(os.Getenv("PINECONE_API_KEY")),
			APIVersion: strings.TrimSpace(os.Getenv("PINECONE_API_VERSION")),
			BaseURL:    strings.TrimSpace(os.Getenv("PINECONE_BASE_URL")),
			Timeout:    30 * time.Second,
		})
		if err != nil {
			classified := classifyVectorProviderBootstrapError(provider, mode, err)
			code := vectorProviderBootstrapErrorCode(classified)
			if metrics != nil {
				metrics.ObserveVectorStoreProviderBootstrap(provider, "error", string(code))
			}
			log.Error(
				"Vector store provider bootstrap failed",
				"provider", provider,
				"object_storage_mode", mode,
				"provider_mode_source", modeSource,
				"error_code", code,
				"error", classified,
			)
			return nil, nil, classified
		}

		vs, err := newPineconeVectorStore(log, pc)
		if err != nil {
			classified := classifyVectorProviderBootstrapError(provider, mode, err)
			code := vectorProviderBootstrapErrorCode(classified)
			if metrics != nil {
				metrics.ObserveVectorStoreProviderBootstrap(provider, "error", string(code))
			}
			log.Error(
				"Vector store provider bootstrap failed",
				"provider", provider,
				"object_storage_mode", mode,
				"provider_mode_source", modeSource,
				"error_code", code,
				"error", classified,
			)
			return nil, nil, classified
		}

		if metrics != nil {
			metrics.ObserveVectorStoreProviderBootstrap(provider, "success", "none")
		}
		return pc, instrumentVectorStore(provider, vs), nil

	default:
		err := &VectorProviderBootstrapError{
			Code:              VectorProviderBootstrapErrorInvalidProvider,
			Provider:          provider,
			ObjectStorageMode: mode,
			Cause:             fmt.Errorf("unsupported vector provider %q", provider),
		}
		if metrics != nil {
			metrics.ObserveVectorStoreProviderBootstrap(provider, "error", string(err.Code))
		}
		log.Error(
			"Vector store provider selection failed",
			"provider", provider,
			"object_storage_mode", mode,
			"provider_mode_source", modeSource,
			"error_code", err.Code,
			"error", err,
		)
		return nil, nil, err
	}
}

func classifyVectorProviderBootstrapError(provider, objectStorageMode string, err error) error {
	var urlErr *neturl.Error
	if errors.As(err, &urlErr) {
		return &VectorProviderBootstrapError{
			Code:              VectorProviderBootstrapErrorConnectFailed,
			Provider:          provider,
			ObjectStorageMode: objectStorageMode,
			Cause:             err,
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return &VectorProviderBootstrapError{
			Code:              VectorProviderBootstrapErrorConnectFailed,
			Provider:          provider,
			ObjectStorageMode: objectStorageMode,
			Cause:             err,
		}
	}
	errLower := strings.ToLower(err.Error())
	if strings.Contains(errLower, "ready check failed") || strings.Contains(errLower, "connection refused") {
		return &VectorProviderBootstrapError{
			Code:              VectorProviderBootstrapErrorConnectFailed,
			Provider:          provider,
			ObjectStorageMode: objectStorageMode,
			Cause:             err,
		}
	}

	var cfgErr *qdrant.ConfigError
	if errors.As(err, &cfgErr) {
		switch cfgErr.Code {
		case qdrant.ConfigErrorMissingURL:
			return &VectorProviderBootstrapError{
				Code:              VectorProviderBootstrapErrorMissingQdrantURL,
				Provider:          provider,
				ObjectStorageMode: objectStorageMode,
				Cause:             err,
			}
		case qdrant.ConfigErrorInvalidURL:
			return &VectorProviderBootstrapError{
				Code:              VectorProviderBootstrapErrorInvalidQdrantURL,
				Provider:          provider,
				ObjectStorageMode: objectStorageMode,
				Cause:             err,
			}
		case qdrant.ConfigErrorMissingCollection:
			return &VectorProviderBootstrapError{
				Code:              VectorProviderBootstrapErrorMissingQdrantColl,
				Provider:          provider,
				ObjectStorageMode: objectStorageMode,
				Cause:             err,
			}
		case qdrant.ConfigErrorMissingVectorDim:
			return &VectorProviderBootstrapError{
				Code:              VectorProviderBootstrapErrorMissingQdrantVector,
				Provider:          provider,
				ObjectStorageMode: objectStorageMode,
				Cause:             err,
			}
		case qdrant.ConfigErrorInvalidVectorDim:
			return &VectorProviderBootstrapError{
				Code:              VectorProviderBootstrapErrorInvalidQdrantVector,
				Provider:          provider,
				ObjectStorageMode: objectStorageMode,
				Cause:             err,
			}
		default:
			return &VectorProviderBootstrapError{
				Code:              VectorProviderBootstrapErrorQdrantConfigFailed,
				Provider:          provider,
				ObjectStorageMode: objectStorageMode,
				Cause:             err,
			}
		}
	}

	return &VectorProviderBootstrapError{
		Code:              VectorProviderBootstrapErrorProviderInitFailed,
		Provider:          provider,
		ObjectStorageMode: objectStorageMode,
		Cause:             err,
	}
}

func vectorProviderBootstrapErrorCode(err error) VectorProviderBootstrapErrorCode {
	var bootstrapErr *VectorProviderBootstrapError
	if errors.As(err, &bootstrapErr) {
		if bootstrapErr.Code != "" {
			return bootstrapErr.Code
		}
	}
	return VectorProviderBootstrapErrorConnectFailed
}

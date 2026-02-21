package qdrant

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	URL             string
	Collection      string
	NamespacePrefix string
	VectorDim       int
}

type ConfigErrorCode string

const (
	ConfigErrorMissingURL        ConfigErrorCode = "missing_url"
	ConfigErrorInvalidURL        ConfigErrorCode = "invalid_url"
	ConfigErrorMissingCollection ConfigErrorCode = "missing_collection"
	ConfigErrorMissingVectorDim  ConfigErrorCode = "missing_vector_dim"
	ConfigErrorInvalidVectorDim  ConfigErrorCode = "invalid_vector_dim"
)

type ConfigError struct {
	Code  ConfigErrorCode
	Value string
	Cause error
}

func (e *ConfigError) Error() string {
	if e == nil {
		return "invalid qdrant config"
	}
	switch e.Code {
	case ConfigErrorMissingURL:
		return "QDRANT_URL is required"
	case ConfigErrorInvalidURL:
		return fmt.Sprintf(
			"invalid QDRANT_URL=%q; expected absolute URL like http://qdrant:6333",
			e.Value,
		)
	case ConfigErrorMissingCollection:
		return "QDRANT_COLLECTION is required"
	case ConfigErrorMissingVectorDim:
		return "QDRANT_VECTOR_DIM is required and must be a positive integer"
	case ConfigErrorInvalidVectorDim:
		return fmt.Sprintf(
			"invalid QDRANT_VECTOR_DIM=%q; expected positive integer",
			e.Value,
		)
	default:
		return "invalid qdrant config"
	}
}

func (e *ConfigError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func ResolveConfigFromEnv() (Config, error) {
	rawDim := strings.TrimSpace(os.Getenv("QDRANT_VECTOR_DIM"))
	dim := 0
	if rawDim != "" {
		parsed, err := strconv.Atoi(rawDim)
		if err != nil {
			return Config{}, &ConfigError{
				Code:  ConfigErrorInvalidVectorDim,
				Value: rawDim,
				Cause: err,
			}
		}
		dim = parsed
	}

	cfg := Config{
		URL:             strings.TrimSpace(os.Getenv("QDRANT_URL")),
		Collection:      strings.TrimSpace(os.Getenv("QDRANT_COLLECTION")),
		NamespacePrefix: strings.TrimSpace(os.Getenv("QDRANT_NAMESPACE_PREFIX")),
		VectorDim:       dim,
	}
	if cfg.NamespacePrefix == "" {
		cfg.NamespacePrefix = "nb"
	}

	if err := ValidateConfig(cfg, rawDim != ""); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// ValidateConfig validates a Qdrant config.
// Pass hasRawVectorDim=false when QDRANT_VECTOR_DIM is unset so missing vs invalid can be reported separately.
func ValidateConfig(cfg Config, hasRawVectorDim bool) error {
	if cfg.URL == "" {
		return &ConfigError{Code: ConfigErrorMissingURL}
	}
	parsed, err := url.Parse(cfg.URL)
	if err != nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
		return &ConfigError{
			Code:  ConfigErrorInvalidURL,
			Value: cfg.URL,
			Cause: err,
		}
	}
	if strings.TrimSpace(cfg.Collection) == "" {
		return &ConfigError{Code: ConfigErrorMissingCollection}
	}
	if !hasRawVectorDim && cfg.VectorDim == 0 {
		return &ConfigError{Code: ConfigErrorMissingVectorDim}
	}
	if cfg.VectorDim <= 0 {
		return &ConfigError{
			Code:  ConfigErrorInvalidVectorDim,
			Value: strconv.Itoa(cfg.VectorDim),
		}
	}
	return nil
}

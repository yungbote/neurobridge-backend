package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (d *Duration) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "" || s == "null" {
		d.Duration = 0
		return nil
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		u, err := strconv.Unquote(s)
		if err != nil {
			return err
		}
		if strings.TrimSpace(u) == "" {
			d.Duration = 0
			return nil
		}
		dd, err := time.ParseDuration(u)
		if err != nil {
			return err
		}
		d.Duration = dd
		return nil
	}

	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("duration must be a JSON string like \"5s\" or an int nanoseconds: %w", err)
	}
	d.Duration = time.Duration(n)
	return nil
}

func defaultConfig() *Config {
	return &Config{
		Env: "development",
		HTTP: HTTPConfig{
			Addr:              ":8080",
			ReadHeaderTimeout: Duration{Duration: 5 * time.Second},
			IdleTimeout:       Duration{Duration: 2 * time.Minute},
			ShutdownTimeout:   Duration{Duration: 15 * time.Second},
			MaxRequestBytes:   10 << 20,
			EnableOAICompat:   false,
		},
		Models: []ModelConfig{
			{ID: "mock-1", Engine: EngineConfig{Type: "mock"}},
		},
	}
}

func Load() (*Config, error) {
	cfg := defaultConfig()

	cfgPath := strings.TrimSpace(os.Getenv("NB_CONFIG_PATH"))
	if cfgPath == "" {
		if wd, err := os.Getwd(); err == nil {
			p := filepath.Join(wd, "config", "config.json")
			if _, err := os.Stat(p); err == nil {
				cfgPath = p
			}
		}
	}

	if cfgPath != "" {
		b, err := os.ReadFile(cfgPath)
		if err != nil {
			return nil, err
		}
		var loaded Config
		if err := json.Unmarshal(b, &loaded); err != nil {
			return nil, err
		}
		*cfg = loaded
	}

	if v := strings.TrimSpace(os.Getenv("LOG_MODE")); v != "" {
		cfg.Env = v
	}
	if v := strings.TrimSpace(os.Getenv("NB_HTTP_ADDR")); v != "" {
		cfg.HTTP.Addr = v
	}
	if v := strings.TrimSpace(os.Getenv("NB_ENABLE_OAI_COMPAT")); v != "" {
		cfg.HTTP.EnableOAICompat = parseBool(v)
	}

	if cfg.Env == "" {
		cfg.Env = "development"
	}
	if strings.TrimSpace(cfg.HTTP.Addr) == "" {
		cfg.HTTP.Addr = ":8080"
	}
	if cfg.HTTP.MaxRequestBytes <= 0 {
		cfg.HTTP.MaxRequestBytes = 10 << 20
	}
	if len(cfg.Models) == 0 {
		return nil, errors.New("config must define at least one model")
	}
	for i := range cfg.Models {
		m := &cfg.Models[i]
		if strings.TrimSpace(m.ID) == "" {
			return nil, errors.New("model id is required")
		}
		if strings.TrimSpace(m.Engine.Type) == "" {
			return nil, fmt.Errorf("model %q missing engine.type", m.ID)
		}

		if strings.TrimSpace(m.UpstreamModel) == "" {
			m.UpstreamModel = strings.TrimSpace(m.ID)
		}

		m.Engine.Type = strings.TrimSpace(m.Engine.Type)
		m.Engine.BaseURL = strings.TrimRight(strings.TrimSpace(m.Engine.BaseURL), "/")
		m.Engine.ChatCompletionsPath = strings.TrimSpace(m.Engine.ChatCompletionsPath)
		m.Engine.EmbeddingsPath = strings.TrimSpace(m.Engine.EmbeddingsPath)

		// Defaults for OpenAI-compatible HTTP engines.
		if strings.EqualFold(m.Engine.Type, "openai_http") || strings.EqualFold(m.Engine.Type, "oai_http") {
			// Normalize type (avoid implying OpenAI-as-provider).
			m.Engine.Type = "oai_http"
			if m.Engine.BaseURL == "" {
				return nil, fmt.Errorf("model %q (oai_http) missing engine.base_url", m.ID)
			}
			if m.Engine.ChatCompletionsPath == "" {
				m.Engine.ChatCompletionsPath = "/v1/chat/completions"
			}
			if m.Engine.EmbeddingsPath == "" {
				m.Engine.EmbeddingsPath = "/v1/embeddings"
			}
			if m.Engine.Timeout.Duration <= 0 {
				m.Engine.Timeout = Duration{Duration: 60 * time.Second}
			}
			if m.Engine.StreamTimeout.Duration < 0 {
				return nil, fmt.Errorf("model %q invalid engine.stream_timeout", m.ID)
			}

			m.Engine.JSONSchema.Mode = strings.ToLower(strings.TrimSpace(m.Engine.JSONSchema.Mode))
			switch m.Engine.JSONSchema.Mode {
			case "", "auto":
				m.Engine.JSONSchema.Mode = "auto"
			case "none", "guided_json", "prompt":
			default:
				return nil, fmt.Errorf("model %q invalid engine.json_schema.mode=%q", m.ID, m.Engine.JSONSchema.Mode)
			}

			if m.Engine.JSONSchema.MaxRetries < 0 {
				return nil, fmt.Errorf("model %q invalid engine.json_schema.max_retries", m.ID)
			}
			if m.Engine.JSONSchema.MaxRetries == 0 {
				m.Engine.JSONSchema.MaxRetries = 2
			}

			if m.Engine.JSONSchema.MaxPromptBytes < 0 {
				return nil, fmt.Errorf("model %q invalid engine.json_schema.max_prompt_bytes", m.ID)
			}
			if m.Engine.JSONSchema.MaxPromptBytes == 0 {
				m.Engine.JSONSchema.MaxPromptBytes = 64 << 10
			}
		}
	}

	return cfg, nil
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}

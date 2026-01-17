package config

import "time"

type Duration struct {
	Duration time.Duration
}

type HTTPConfig struct {
	Addr              string   `json:"addr"`
	ReadHeaderTimeout Duration `json:"read_header_timeout"`
	IdleTimeout       Duration `json:"idle_timeout"`
	ShutdownTimeout   Duration `json:"shutdown_timeout"`
	MaxRequestBytes   int64    `json:"max_request_bytes"`

	// EnableOAICompat exposes an OpenAI-protocol compatibility surface under `/compat/oai/*`.
	// This is intended for debugging and transitional migrations; Neurobridge's own API is `/v1/*`.
	EnableOAICompat bool `json:"enable_oai_compat,omitempty"`
}

type JSONSchemaConfig struct {
	// Mode controls how the gateway enforces/requests JSON schema outputs from upstream engines.
	// - "none": ignore schema hints (best-effort)
	// - "guided_json": send guided decoding fields to the upstream OpenAI-compatible server (vLLM-style)
	// - "prompt": append a system instruction with the schema text and retry on invalid JSON
	// - "auto": try guided_json, then fall back to prompt
	Mode string `json:"mode,omitempty"`

	// MaxRetries is the number of additional attempts when strict JSON is requested and output is invalid.
	// Total attempts = 1 + MaxRetries.
	MaxRetries int `json:"max_retries,omitempty"`

	// MaxPromptBytes caps how much schema JSON can be injected into a prompt when Mode includes "prompt".
	MaxPromptBytes int `json:"max_prompt_bytes,omitempty"`
}

type EngineConfig struct {
	Type string `json:"type"`

	// BaseURL is the upstream engine base URL (for "oai_http" engines).
	BaseURL string `json:"base_url,omitempty"`

	// APIKey is optional; when set, the gateway sends `Authorization: Bearer <api_key>` to the upstream.
	APIKey string `json:"api_key,omitempty"`

	// OpenAI-compatible endpoint paths (defaults are used if empty).
	ChatCompletionsPath string `json:"chat_completions_path,omitempty"`
	EmbeddingsPath      string `json:"embeddings_path,omitempty"`

	// Default upstream timeouts. Streaming requests should rely on client cancellation.
	Timeout       Duration `json:"timeout,omitempty"`
	StreamTimeout Duration `json:"stream_timeout,omitempty"`

	JSONSchema JSONSchemaConfig `json:"json_schema,omitempty"`
}

type ModelConfig struct {
	ID string `json:"id"`

	// UpstreamModel overrides the model name sent to the engine. Defaults to ID.
	UpstreamModel string `json:"upstream_model,omitempty"`

	Engine EngineConfig `json:"engine"`
}

type Config struct {
	Env    string        `json:"env"`
	HTTP   HTTPConfig    `json:"http"`
	Models []ModelConfig `json:"models"`
}

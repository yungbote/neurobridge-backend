package pinecone

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type Client interface {
	DescribeIndex(ctx context.Context, indexName string) (*IndexDescription, error)
	UpsertVectors(ctx context.Context, host string, req UpsertRequest) (*UpsertResponse, error)
	Query(ctx context.Context, host string, req QueryRequest) (*QueryResponse, error)
}

type Config struct {
	APIKey     string
	APIVersion string
	BaseURL    string
	Timeout    time.Duration
}

type client struct {
	log  *logger.Logger
	cfg  Config
	http *http.Client
}

func New(log *logger.Logger, cfg Config) (Client, error) {
	if log == nil {
		return nil, fmt.Errorf("logger required")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("missing Pinecone API key")
	}
	if strings.TrimSpace(cfg.APIVersion) == "" {
		cfg.APIVersion = "2025-10"
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = "https://api.pinecone.io"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &client{
		log:  log.With("client", "PineconeClient"),
		cfg:  cfg,
		http: &http.Client{Timeout: cfg.Timeout},
	}, nil
}

// -------------------- Control plane --------------------

type IndexDescription struct {
	Name      string `json:"name"`
	Host      string `json:"host"`
	Dimension int    `json:"dimension"`
	Metric    string `json:"metric"`
	Status    struct {
		Ready bool   `json:"ready"`
		State string `json:"state"`
	} `json:"status"`
}

func (c *client) DescribeIndex(ctx context.Context, indexName string) (*IndexDescription, error) {
	indexName = strings.TrimSpace(indexName)
	if indexName == "" {
		return nil, fmt.Errorf("indexName required")
	}

	u := strings.TrimRight(c.cfg.BaseURL, "/") + "/indexes/" + indexName
	req, err := http.NewRequestWithContext(defaultCtx(ctx), "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Api-Key", c.cfg.APIKey)
	req.Header.Set("X-Pinecone-Api-Version", c.cfg.APIVersion)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pinecone describe_index http %d: %s", resp.StatusCode, string(raw))
	}

	var out IndexDescription
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("pinecone describe_index decode: %w", err)
	}
	if strings.TrimSpace(out.Host) == "" {
		return nil, fmt.Errorf("pinecone describe_index returned empty host")
	}
	return &out, nil
}

// -------------------- Data plane --------------------

type Vector struct {
	ID       string         `json:"id"`
	Values   []float32      `json:"values"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type UpsertRequest struct {
	Vectors   []Vector `json:"vectors"`
	Namespace string   `json:"namespace,omitempty"`
}

type UpsertResponse struct {
	UpsertedCount int64 `json:"upsertedCount"`
}

func (c *client) UpsertVectors(ctx context.Context, host string, req UpsertRequest) (*UpsertResponse, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, fmt.Errorf("host required")
	}
	if len(req.Vectors) == 0 {
		return &UpsertResponse{UpsertedCount: 0}, nil
	}
	u := "https://" + host + "/vectors/upsert"
	return doJSON[UpsertResponse](c, ctx, "POST", u, req)
}

type QueryRequest struct {
	Namespace       string         `json:"namespace,omitempty"`
	Vector          []float32      `json:"vector,omitempty"`
	TopK            int            `json:"topK"`
	Filter          map[string]any `json:"filter,omitempty"`
	IncludeValues   bool           `json:"includeValues,omitempty"`
	IncludeMetadata bool           `json:"includeMetadata,omitempty"`
}

type QueryMatch struct {
	ID       string         `json:"id"`
	Score    float64        `json:"score"`
	Values   []float32      `json:"values,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type QueryResponse struct {
	Matches []QueryMatch `json:"matches"`
}

func (c *client) Query(ctx context.Context, host string, req QueryRequest) (*QueryResponse, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, fmt.Errorf("host required")
	}
	if req.TopK <= 0 {
		req.TopK = 10
	}
	if len(req.Vector) == 0 {
		return nil, fmt.Errorf("query vector required")
	}
	u := "https://" + host + "/query"
	return doJSON[QueryResponse](c, ctx, "POST", u, req)
}

// -------------------- helpers --------------------

func doJSON[T any](c *client, ctx context.Context, method, url string, body any) (*T, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequestWithContext(defaultCtx(ctx), method, url, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Api-Key", c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Pinecone-Api-Version", c.cfg.APIVersion)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pinecone http %d: %s", resp.StatusCode, string(raw))
	}

	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("pinecone decode error: %w; raw=%s", err, string(raw))
	}
	return &out, nil
}

func defaultCtx(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

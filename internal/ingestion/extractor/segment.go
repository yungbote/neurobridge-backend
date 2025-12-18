package extractor

import (
	"context"
	"time"

	"github.com/google/uuid"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

// Segment now lives in internal/domain (and gcp/openai clients use it).
type Segment = types.Segment

type ExtractionSummary struct {
	MaterialFileID uuid.UUID      `json:"material_file_id"`
	StorageKey     string         `json:"storage_key"`
	Kind           string         `json:"kind"` // pdf|docx|pptx|image|video|audio|unknown
	PrimaryTextLen int            `json:"primary_text_len"`
	Segments       []Segment      `json:"segments,omitempty"`
	Assets         []AssetRef     `json:"assets,omitempty"`
	Warnings       []string       `json:"warnings,omitempty"`
	Diagnostics    map[string]any `json:"diagnostics,omitempty"`
	StartedAt      time.Time      `json:"started_at"`
	FinishedAt     time.Time      `json:"finished_at"`
}

// AssetRef describes derived assets stored in GCS.
type AssetRef struct {
	Kind     string         `json:"kind"` // original|pdf_page|ppt_slide|frame|audio
	Key      string         `json:"key"`  // GCS object key (bucket relative)
	URL      string         `json:"url"`  // public url (or CDN url)
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Exported wrappers (so pipeline can call into extractor without duplicating logic).
func DefaultCtx(ctx context.Context) context.Context { return defaultCtx(ctx) }
func MergeDiag(dst, src map[string]any)              { mergeDiag(dst, src) }
func EnsureGSPrefix(s string) string                 { return ensureGSPrefix(s) }
func MinInt(a, b int) int                            { return minInt(a, b) }

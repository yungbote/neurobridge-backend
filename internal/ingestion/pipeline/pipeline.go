package pipeline

import (
	"context"
	"io"
	"path/filepath"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/gcp"
	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	"github.com/yungbote/neurobridge-backend/internal/clients/localmedia"
	"github.com/yungbote/neurobridge-backend/internal/ingestion/extractor"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/repos"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

type Segment = extractor.Segment
type AssetRef = extractor.AssetRef
type ExtractionSummary = extractor.ExtractionSummary

type ContentExtractionService interface {
	ExtractAndPersist(ctx context.Context, tx *gorm.DB, mf *types.MaterialFile) (*ExtractionSummary, error)
}

type service struct {
	ex *extractor.Extractor
}

func NewContentExtractionService(
	db *gorm.DB,
	log *logger.Logger,
	materialChunkRepo repos.MaterialChunkRepo,
	materialFileRepo repos.MaterialFileRepo,
	bucket gcp.BucketService,
	media localmedia.MediaToolsService,
	docai gcp.Document,
	vision gcp.Vision,
	speech gcp.Speech,
	videoAI gcp.Video,
	caption openai.Caption,
) ContentExtractionService {
	ex := extractor.New(
		db, log,
		materialChunkRepo,
		materialFileRepo,
		bucket,
		media,
		docai,
		vision,
		speech,
		videoAI,
		caption,
	)
	return &service{ex: ex}
}

func (s *service) ExtractAndPersist(ctx context.Context, tx *gorm.DB, mf *types.MaterialFile) (*ExtractionSummary, error) {
	ctx = extractor.DefaultCtx(ctx)

	if mf == nil || mf.ID == uuid.Nil || mf.StorageKey == "" || mf.MaterialSetID == uuid.Nil {
		return nil, fmt.Errorf("invalid material file")
	}
	if s.ex == nil || s.ex.DB == nil {
		return nil, fmt.Errorf("db required")
	}

	started := time.Now()
	summary := &ExtractionSummary{
		MaterialFileID: mf.ID,
		StorageKey:     mf.StorageKey,
		StartedAt:      started,
		Diagnostics:    map[string]any{},
	}

	if s.ex.Media != nil {
		if err := s.ex.Media.AssertReady(ctx); err != nil {
			return nil, fmt.Errorf("media tools not ready: %w", err)
		}
	}

	origPath, cleanupOrig, origBytes, err := s.ex.DownloadMaterialToTemp(ctx, mf)
	if err != nil {
		return nil, err
	}
	defer cleanupOrig()

	kind := extractor.ClassifyKind(mf.OriginalName, mf.MimeType, origBytes, origPath)
	summary.Kind = kind

	assets := []AssetRef{{
		Kind: "original",
		Key:  mf.StorageKey,
		URL:  s.ex.Bucket.GetPublicURL(gcp.BucketCategoryMaterial, mf.StorageKey),
		Metadata: map[string]any{
			"mime":       mf.MimeType,
			"name":       mf.OriginalName,
			"size_bytes": mf.SizeBytes,
		},
	}}

	var (
		allSegments []Segment
		warnings    []string
	)

	switch kind {
	case "pdf":
		segs, derivedAssets, warns, diag, err := s.handlePDF(ctx, mf, origPath)
		if err != nil {
			warnings = append(warnings, "pdf extraction error: "+err.Error())
		}
		allSegments = append(allSegments, segs...)
		assets = append(assets, derivedAssets...)
		extractor.MergeDiag(summary.Diagnostics, diag)
		warnings = append(warnings, warns...)

	case "docx", "pptx":
		segs, derivedAssets, warns, diag, err := s.handleOffice(ctx, mf, origPath, kind)
		if err != nil {
			warnings = append(warnings, "office extraction error: "+err.Error())
		}
		allSegments = append(allSegments, segs...)
		assets = append(assets, derivedAssets...)
		extractor.MergeDiag(summary.Diagnostics, diag)
		warnings = append(warnings, warns...)

	case "image":
		segs, derivedAssets, warns, diag, err := s.handleImage(ctx, mf, origBytes, origPath)
		if err != nil {
			warnings = append(warnings, "image extraction error: "+err.Error())
		}
		allSegments = append(allSegments, segs...)
		assets = append(assets, derivedAssets...)
		extractor.MergeDiag(summary.Diagnostics, diag)
		warnings = append(warnings, warns...)

	case "video":
		segs, derivedAssets, warns, diag, err := s.handleVideo(ctx, mf, origPath)
		if err != nil {
			warnings = append(warnings, "video extraction error: "+err.Error())
		}
		allSegments = append(allSegments, segs...)
		assets = append(assets, derivedAssets...)
		extractor.MergeDiag(summary.Diagnostics, diag)
		warnings = append(warnings, warns...)

	case "audio":
		segs, derivedAssets, warns, diag, err := s.handleAudio(ctx, mf, origPath)
		if err != nil {
			warnings = append(warnings, "audio extraction error: "+err.Error())
		}
		allSegments = append(allSegments, segs...)
		assets = append(assets, derivedAssets...)
		extractor.MergeDiag(summary.Diagnostics, diag)
		warnings = append(warnings, warns...)

	default:
		text, warn, diag := s.ex.BestEffortNativeText(mf.OriginalName, mf.MimeType, origBytes)
		extractor.MergeDiag(summary.Diagnostics, diag)
		if warn != "" {
			warnings = append(warnings, warn)
		}
		if strings.TrimSpace(text) != "" {
			allSegments = append(allSegments, Segment{
				Text:     text,
				Metadata: map[string]any{"kind": "native_text"},
			})
		} else {
			allSegments = append(allSegments, Segment{
				Text: "No extractable content detected for this file type.",
				Metadata: map[string]any{
					"kind": "unextractable",
					"mime": mf.MimeType,
				},
			})
		}
	}

	allSegments = extractor.NormalizeSegments(allSegments)

	if len(allSegments) == 0 {
		allSegments = append(allSegments, Segment{
			Text: "No extractable signals were produced. This may require manual review or updated extraction capabilities.",
			Metadata: map[string]any{
				"kind": "unextractable",
				"mime": mf.MimeType,
				"name": mf.OriginalName,
			},
		})
		warnings = append(warnings, "no segments produced; wrote explicit unextractable segment")
	}

	if err := s.ex.PersistSegmentsAsChunks(ctx, tx, mf, allSegments); err != nil {
		return nil, err
	}

	if err := s.ex.UpdateMaterialFileExtractionStatus(ctx, tx, mf, kind, warnings, summary.Diagnostics); err != nil {
		return nil, err
	}

	summary.Segments = allSegments
	summary.Assets = assets
	summary.Warnings = warnings
	summary.PrimaryTextLen = len(extractor.JoinSegmentsText(allSegments))
	summary.FinishedAt = time.Now()

	return summary, nil
}

// ---- Caption helpers (identical behavior to old file) ----
func (s *service) captionAssetToSegments(
	ctx context.Context,
	task string,
	asset AssetRef,
	page int,
	startSec *float64,
	endSec *float64,
) ([]Segment, string, error) {
	if s.ex.Caption == nil {
		return nil, "caption provider unavailable", nil
	}

	// 1) Try URL first (fast path)
	var (
		res *openai.CaptionResult
		err error
	)

	if strings.TrimSpace(asset.URL) != "" {
		res, err = s.ex.Caption.DescribeImage(ctx, openai.CaptionRequest{
			Task:      task,
			Prompt:    "",
			ImageURL:   asset.URL,
			Detail:    "high",
			MaxTokens: 1200,
		})
	}

	// 2) If URL path failed (or URL missing), fall back to downloading bytes from GCS and send bytes to OpenAI.
	if err != nil || res == nil {
		if s.ex.Bucket != nil && strings.TrimSpace(asset.Key) != "" {
			rc, derr := s.ex.Bucket.DownloadFile(ctx, gcp.BucketCategoryMaterial, asset.Key)
			if derr != nil {
				// keep original error if it exists, otherwise return download error
				if err == nil {
					err = fmt.Errorf("download asset from bucket: %w", derr)
				}
			} else if rc != nil {
				b, rerr := io.ReadAll(rc)
				_ = rc.Close()
				if rerr != nil {
					if err == nil {
						err = fmt.Errorf("read downloaded asset bytes: %w", rerr)
					}
				} else if len(b) > 0 {
					res, err = s.ex.Caption.DescribeImage(ctx, openai.CaptionRequest{
						Task:       task,
						Prompt:     "",
						ImageBytes: b,
						ImageMime:  mimeFromKey(asset.Key),
						Detail:     "high",
						MaxTokens:  1200,
					})
				}
			}
		}
	}

	if err != nil {
		return nil, "", err
	}
	if res == nil {
		return nil, "caption produced no result", nil
	}

	// Build text from result (same style as your existing code)
	var b strings.Builder
	b.WriteString(res.Summary)
	if len(res.KeyTakeaways) > 0 {
		b.WriteString("\n\nKey takeaways:\n- ")
		b.WriteString(strings.Join(res.KeyTakeaways, "\n- "))
	}
	if len(res.Relationships) > 0 {
		b.WriteString("\n\nRelationships:\n- ")
		b.WriteString(strings.Join(res.Relationships, "\n- "))
	}
	if len(res.TextInImage) > 0 {
		b.WriteString("\n\nText in image:\n- ")
		b.WriteString(strings.Join(res.TextInImage, "\n- "))
	}

	txt := strings.TrimSpace(b.String())
	if txt == "" {
		return nil, "caption produced empty text", nil
	}

	md := map[string]any{
		"kind":      task,
		"asset_key": asset.Key,
		"provider":  "openai_caption",
	}
	if page > 0 {
		md["page"] = page
	}
	if startSec != nil {
		md["start_sec"] = *startSec
	}
	if endSec != nil {
		md["end_sec"] = *endSec
	}

	seg := Segment{Text: txt, Metadata: md}
	if page > 0 {
		p := page
		seg.Page = &p
	}
	if startSec != nil {
		seg.StartSec = startSec
	}
	if endSec != nil {
		seg.EndSec = endSec
	}

	return []Segment{seg}, "", nil
}

func mimeFromKey(key string) string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(key)))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "image/png"
	}
}


func (s *service) captionBytesToSegments(
	ctx context.Context,
	task string,
	assetKey string,
	imageMime string,
	imageBytes []byte,
	page int,
) ([]Segment, string, error) {
	if s.ex.Caption == nil {
		return nil, "caption provider unavailable", nil
	}
	if len(imageBytes) == 0 {
		return nil, "caption skipped: empty image bytes", nil
	}
	if strings.TrimSpace(imageMime) == "" {
		imageMime = "image/jpeg"
	}

	req := openai.CaptionRequest{
		Task:       task,
		Prompt:     "",
		ImageBytes: imageBytes,
		ImageMime:  imageMime,
		Detail:     "high",
		MaxTokens:  1200,
	}

	res, err := s.ex.Caption.DescribeImage(ctx, req)
	if err != nil {
		return nil, "", err
	}

	var b strings.Builder
	b.WriteString(res.Summary)
	if len(res.KeyTakeaways) > 0 {
		b.WriteString("\n\nKey takeaways:\n- ")
		b.WriteString(strings.Join(res.KeyTakeaways, "\n- "))
	}
	if len(res.Relationships) > 0 {
		b.WriteString("\n\nRelationships:\n- ")
		b.WriteString(strings.Join(res.Relationships, "\n- "))
	}
	if len(res.TextInImage) > 0 {
		b.WriteString("\n\nText in image:\n- ")
		b.WriteString(strings.Join(res.TextInImage, "\n- "))
	}

	txt := strings.TrimSpace(b.String())
	if txt == "" {
		return nil, "caption produced empty text", nil
	}

	md := map[string]any{
		"kind":      task,
		"asset_key": assetKey,
		"provider":  "openai_caption",
		"source":    "bytes",
	}
	if page > 0 {
		md["page"] = page
	}

	seg := Segment{Text: txt, Metadata: md}
	if page > 0 {
		p := page
		seg.Page = &p
	}

	return []Segment{seg}, "", nil
}











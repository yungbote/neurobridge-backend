package services

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/documentai/apiv1/documentaipb"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/repos"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

// ContentExtractionService is the orchestration layer that guarantees:
// - Multi-modal coverage (docs+tables+diagrams, images, audio/video speech+onscreen)
// - Provenance-rich canonical segments
// - Persisted assets + chunks + diagnostics
//
// IMPORTANT: Run this in your worker image (needs ffmpeg/libreoffice/pdftoppm).
type ContentExtractionService interface {
	ExtractAndPersist(ctx context.Context, tx *gorm.DB, mf *types.MaterialFile) (*ExtractionSummary, error)
}

type ExtractionSummary struct {
	MaterialFileID uuid.UUID          `json:"material_file_id"`
	StorageKey     string             `json:"storage_key"`
	Kind           string             `json:"kind"` // pdf|docx|pptx|image|video|audio|unknown
	PrimaryTextLen int                `json:"primary_text_len"`
	Segments       []Segment          `json:"segments,omitempty"`
	Assets         []AssetRef         `json:"assets,omitempty"`
	Warnings       []string           `json:"warnings,omitempty"`
	Diagnostics    map[string]any     `json:"diagnostics,omitempty"`
	StartedAt      time.Time          `json:"started_at"`
	FinishedAt     time.Time          `json:"finished_at"`
}

// AssetRef describes derived assets stored in GCS.
type AssetRef struct {
	Kind     string         `json:"kind"`     // original|pdf_page|ppt_slide|frame|audio
	Key      string         `json:"key"`      // GCS object key (bucket relative)
	URL      string         `json:"url"`      // public url (or CDN url)
	Metadata map[string]any `json:"metadata,omitempty"`
}

type contentExtractionService struct {
	db *gorm.DB
	log *logger.Logger

	materialChunkRepo repos.MaterialChunkRepo
	materialFileRepo  repos.MaterialFileRepo

	bucket BucketService

	media   MediaToolsService
	docai   DocumentProviderService
	vision  VisionProviderService
	speech  SpeechProviderService
	videoAI VideoIntelligenceProviderService
	caption CaptionProviderService

	// env-backed settings
	materialBucketName string // for gs:// URIs
	visionOutputPrefix string // gs://bucket/prefix/
	docaiProjectID     string
	docaiLocation      string
	docaiProcessorID   string
	docaiProcessorVer  string

	// hard caps
	maxBytesDownload         int64
	maxPDFPagesRender        int
	maxPDFPagesCaption       int
	maxFramesVideo           int
	maxFramesCaption         int
	maxSecondsAudioTranscribe int
	maxImageBytesDataURL     int64

	chunkSize int
	chunkOverlap int

	// video frame extraction
	videoFrameIntervalSec float64
	videoSceneThreshold   float64
}

func NewContentExtractionService(
	db *gorm.DB,
	log *logger.Logger,
	materialChunkRepo repos.MaterialChunkRepo,
	materialFileRepo repos.MaterialFileRepo,
	bucket BucketService,
	media MediaToolsService,
	docai DocumentProviderService,
	vision VisionProviderService,
	speech SpeechProviderService,
	videoAI VideoIntelligenceProviderService,
	caption CaptionProviderService,
) ContentExtractionService {
	s := &contentExtractionService{
		db: db,
		log: log.With("service", "ContentExtractionService"),

		materialChunkRepo: materialChunkRepo,
		materialFileRepo:  materialFileRepo,
		bucket:            bucket,

		media:   media,
		docai:   docai,
		vision:  vision,
		speech:  speech,
		videoAI: videoAI,
		caption: caption,

		// defaults (tuned for production safety)
		materialBucketName: osGet("MATERIAL_GCS_BUCKET_NAME", ""),
		visionOutputPrefix: osGet("VISION_OCR_OUTPUT_PREFIX", ""),
		docaiProjectID:     osGet("GCP_PROJECT_ID", ""),
		docaiLocation:      osGet("DOCUMENTAI_LOCATION", "us"),
		docaiProcessorID:   osGet("DOCUMENTAI_PROCESSOR_ID", ""),
		docaiProcessorVer:  osGet("DOCUMENTAI_PROCESSOR_VERSION", ""),

		maxBytesDownload:         1024 * 1024 * 1024, // 1GB (streamed to disk)
		maxPDFPagesRender:        200,
		maxPDFPagesCaption:       60,
		maxFramesVideo:           200,
		maxFramesCaption:         60,
		maxSecondsAudioTranscribe: 4 * 60 * 60, // 4 hours cap
		maxImageBytesDataURL:     3 * 1024 * 1024, // 3MB data URL cap; else use public URL

		chunkSize:    1200,
		chunkOverlap: 200,

		videoFrameIntervalSec: 2.0,
		videoSceneThreshold:   0.0, // set >0 to enable scene-change selection
	}
	return s
}

func (s *contentExtractionService) ExtractAndPersist(ctx context.Context, tx *gorm.DB, mf *types.MaterialFile) (*ExtractionSummary, error) {
	ctx = defaultCtx(ctx)

	if mf == nil || mf.ID == uuid.Nil || mf.StorageKey == "" || mf.MaterialSetID == uuid.Nil {
		return nil, fmt.Errorf("invalid material file")
	}
	if s.db == nil {
		return nil, fmt.Errorf("db required")
	}

	started := time.Now()
	summary := &ExtractionSummary{
		MaterialFileID: mf.ID,
		StorageKey:     mf.StorageKey,
		StartedAt:      started,
		Diagnostics:    map[string]any{},
	}

	// Always ensure binaries exist if we might need them.
	// (Even for “text-only” docs, Office/video pipelines require media tools.)
	if s.media != nil {
		if err := s.media.AssertReady(ctx); err != nil {
			// This is fatal for “full coverage” because Office/video/diagram rendering depends on it.
			return nil, fmt.Errorf("media tools not ready: %w", err)
		}
	}

	// Download original file to disk (streamed), keep path + bytes for small files.
	origPath, cleanupOrig, origBytes, err := s.downloadMaterialToTemp(ctx, mf)
	if err != nil {
		return nil, err
	}
	defer cleanupOrig()

	// Determine kind (pdf/docx/pptx/image/video/audio/text/unknown)
	kind := classifyKind(mf.OriginalName, mf.MimeType, origBytes, origPath)
	summary.Kind = kind

	// Always record original as an asset reference
	assets := []AssetRef{{
		Kind: "original",
		Key:  mf.StorageKey,
		URL:  s.bucket.GetPublicURL(BucketCategoryMaterial, mf.StorageKey),
		Metadata: map[string]any{
			"mime": mf.MimeType,
			"name": mf.OriginalName,
			"size_bytes": mf.SizeBytes,
		},
	}}

	var (
		allSegments []Segment
		warnings []string
	)

	// -------------- ROUTING --------------
	switch kind {

	case "pdf":
		segs, derivedAssets, warns, diag, err := s.handlePDF(ctx, mf, origPath)
		if err != nil {
			// Fatal only if storage/db will be inconsistent. Otherwise we continue with warnings.
			warnings = append(warnings, "pdf extraction error: "+err.Error())
		}
		allSegments = append(allSegments, segs...)
		assets = append(assets, derivedAssets...)
		mergeDiag(summary.Diagnostics, diag)
		warnings = append(warnings, warns...)

	case "docx", "pptx":
		segs, derivedAssets, warns, diag, err := s.handleOffice(ctx, mf, origPath, kind)
		if err != nil {
			warnings = append(warnings, "office extraction error: "+err.Error())
		}
		allSegments = append(allSegments, segs...)
		assets = append(assets, derivedAssets...)
		mergeDiag(summary.Diagnostics, diag)
		warnings = append(warnings, warns...)

	case "image":
		segs, derivedAssets, warns, diag, err := s.handleImage(ctx, mf, origBytes, origPath)
		if err != nil {
			warnings = append(warnings, "image extraction error: "+err.Error())
		}
		allSegments = append(allSegments, segs...)
		assets = append(assets, derivedAssets...)
		mergeDiag(summary.Diagnostics, diag)
		warnings = append(warnings, warns...)

	case "video":
		segs, derivedAssets, warns, diag, err := s.handleVideo(ctx, mf, origPath)
		if err != nil {
			warnings = append(warnings, "video extraction error: "+err.Error())
		}
		allSegments = append(allSegments, segs...)
		assets = append(assets, derivedAssets...)
		mergeDiag(summary.Diagnostics, diag)
		warnings = append(warnings, warns...)

	case "audio":
		segs, derivedAssets, warns, diag, err := s.handleAudio(ctx, mf, origPath)
		if err != nil {
			warnings = append(warnings, "audio extraction error: "+err.Error())
		}
		allSegments = append(allSegments, segs...)
		assets = append(assets, derivedAssets...)
		mergeDiag(summary.Diagnostics, diag)
		warnings = append(warnings, warns...)

	default:
		// best-effort native text extraction
		text, warn, diag := s.bestEffortNativeText(mf.OriginalName, mf.MimeType, origBytes)
		mergeDiag(summary.Diagnostics, diag)
		if warn != "" {
			warnings = append(warnings, warn)
		}
		if strings.TrimSpace(text) != "" {
			allSegments = append(allSegments, Segment{
				Text:     text,
				Metadata: map[string]any{"kind": "native_text"},
			})
		} else {
			// Explicitly record missing signal
			allSegments = append(allSegments, Segment{
				Text: "No extractable content detected for this file type.",
				Metadata: map[string]any{
					"kind": "unextractable",
					"mime": mf.MimeType,
				},
			})
		}
	}

	// Normalize + de-dup + prune empty
	allSegments = normalizeSegments(allSegments)

	// Hard requirement: never silently “miss” — if nothing extracted, record explicit failure segment.
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

	// Persist segments -> material_chunk rows (chunk each segment text; preserve metadata including page/time/kind/provider/asset_key)
	if err := s.persistSegmentsAsChunks(ctx, tx, mf, allSegments); err != nil {
		return nil, err
	}

	// Update material_file.ai_type + ai_topics (diagnostics), mark as "extracted"
	if err := s.updateMaterialFileExtractionStatus(ctx, tx, mf, kind, warnings, summary.Diagnostics); err != nil {
		return nil, err
	}

	summary.Segments = allSegments
	summary.Assets = assets
	summary.Warnings = warnings
	summary.PrimaryTextLen = len(joinSegmentsText(allSegments))
	summary.FinishedAt = time.Now()

	return summary, nil
}

// -------------------- PDF --------------------

func (s *contentExtractionService) handlePDF(ctx context.Context, mf *types.MaterialFile, pdfPath string) ([]Segment, []AssetRef, []string, map[string]any, error) {
	diag := map[string]any{"pipeline": "pdf"}
	var warnings []string
	var assets []AssetRef
	var segs []Segment

	// (1) Document AI primary (tables/layout)
	docAIRes, docAIErr := s.tryDocAI(ctx, mf, "application/pdf", mf.StorageKey, mf.OriginalName)
	if docAIErr != nil {
		warnings = append(warnings, "docai failed: "+docAIErr.Error())
		diag["docai_error"] = docAIErr.Error()
	} else {
		diag["docai_primary_text_len"] = len(docAIRes.PrimaryText)
		segs = append(segs, tagSegments(docAIRes.Segments, map[string]any{"kind": "docai_page_text"})...)
		segs = append(segs, tagSegments(docAIRes.Tables, map[string]any{"kind": "table_text"})...)
		segs = append(segs, tagSegments(docAIRes.Forms, map[string]any{"kind": "form_text"})...)
	}

	// (2) Vision OCR fallback if docai empty/weak
	if textSignalWeak(segs) {
		if s.visionOutputPrefix == "" || s.materialBucketName == "" {
			warnings = append(warnings, "vision OCR fallback skipped (missing VISION_OCR_OUTPUT_PREFIX or MATERIAL_GCS_BUCKET_NAME)")
		} else if s.vision != nil {
			gcsURI := fmt.Sprintf("gs://%s/%s", s.materialBucketName, mf.StorageKey)
			outPrefix := fmt.Sprintf("%s%s/%s/", ensureGSPrefix(s.visionOutputPrefix), mf.MaterialSetID.String(), mf.ID.String())
			vres, err := s.vision.OCRFileInGCS(ctx, gcsURI, "application/pdf", outPrefix, s.maxPDFPagesRender)
			if err != nil {
				warnings = append(warnings, "vision OCR failed: "+err.Error())
				diag["vision_error"] = err.Error()
			} else {
				diag["vision_primary_text_len"] = len(vres.PrimaryText)
				// Each page as ocr_text
				for _, sg := range vres.Segments {
					if sg.Metadata == nil {
						sg.Metadata = map[string]any{}
					}
					sg.Metadata["kind"] = "ocr_text"
					sg.Metadata["provider"] = "gcp_vision_async"
					segs = append(segs, sg)
				}
			}
		}
	}

	// (3) Render pages to images (required for diagram capture)
	pageImages, pageAssets, renderWarn, renderDiag := s.renderPDFPagesToGCS(ctx, mf, pdfPath)
	assets = append(assets, pageAssets...)
	warnings = append(warnings, renderWarn...)
	mergeDiag(diag, renderDiag)

	// (4) Caption pages into figure_notes (no missed diagrams)
	// Hard way: caption pages up to maxPDFPagesCaption (or all rendered if smaller)
	if s.caption != nil && len(pageImages) > 0 {
		capN := minInt(len(pageImages), s.maxPDFPagesCaption)
		for i := 0; i < capN; i++ {
			page := i + 1
			imgAsset := pageImages[i]
			noteSegs, warn, err := s.captionAssetToSegments(ctx, "figure_notes", imgAsset, page, nil, nil)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("caption page %d failed: %v", page, err))
				continue
			}
			if warn != "" {
				warnings = append(warnings, warn)
			}
			segs = append(segs, noteSegs...)
		}
	} else {
		warnings = append(warnings, "caption provider unavailable; figure_notes skipped")
	}

	return segs, assets, warnings, diag, nil
}

func (s *contentExtractionService) renderPDFPagesToGCS(ctx context.Context, mf *types.MaterialFile, pdfPath string) ([]AssetRef, []AssetRef, []string, map[string]any) {
	diag := map[string]any{"render": "pdftoppm"}
	var warnings []string

	if s.media == nil {
		return nil, nil, []string{"media tools unavailable; cannot render pdf pages"}, diag
	}

	tmpDir, err := os.MkdirTemp("", "nb_pdf_pages_*")
	if err != nil {
		return nil, nil, []string{"temp dir error: " + err.Error()}, diag
	}
	defer os.RemoveAll(tmpDir)

	paths, err := s.media.RenderPDFToImages(ctx, pdfPath, tmpDir, PDFRenderOptions{
		DPI:    200,
		Format: "png",
		FirstPage: 0,
		LastPage:  0,
	})
	if err != nil {
		return nil, nil, []string{"pdf render failed: " + err.Error()}, diag
	}

	if len(paths) > s.maxPDFPagesRender {
		warnings = append(warnings, fmt.Sprintf("pdf pages truncated: rendered %d capped to %d", len(paths), s.maxPDFPagesRender))
		paths = paths[:s.maxPDFPagesRender]
	}

	// Upload each page image to GCS
	pageAssets := make([]AssetRef, 0, len(paths))
	for i, pth := range paths {
		pageNum := i + 1
		key := fmt.Sprintf("%s/derived/pages/page_%04d.png", mf.StorageKey, pageNum)
		if err := s.uploadLocalToGCS(ctx, nil, key, pth); err != nil {
			warnings = append(warnings, fmt.Sprintf("upload page %d failed: %v", pageNum, err))
			continue
		}
		pageAssets = append(pageAssets, AssetRef{
			Kind: "pdf_page",
			Key:  key,
			URL:  s.bucket.GetPublicURL(BucketCategoryMaterial, key),
			Metadata: map[string]any{
				"page": pageNum,
				"format": "png",
			},
		})
	}

	diag["pages_rendered"] = len(pageAssets)
	return pageAssets, pageAssets, warnings, diag
}

// -------------------- Office (DOCX/PPTX) --------------------

func (s *contentExtractionService) handleOffice(ctx context.Context, mf *types.MaterialFile, officePath string, kind string) ([]Segment, []AssetRef, []string, map[string]any, error) {
	diag := map[string]any{"pipeline": "office", "kind": kind}
	var warnings []string
	var assets []AssetRef
	var segs []Segment

	// (1) Native extraction for text (doesn’t capture diagrams, but captures notes)
	text, warn, nd := s.bestEffortNativeText(mf.OriginalName, mf.MimeType, nil)
	mergeDiag(diag, nd)
	if warn != "" {
		warnings = append(warnings, warn)
	}
	if strings.TrimSpace(text) != "" {
		segs = append(segs, Segment{
			Text: text,
			Metadata: map[string]any{
				"kind": "native_text",
				"source": kind,
			},
		})
	}

	// (2) Convert Office -> PDF (captures slide visuals)
	if s.media == nil {
		return segs, assets, append(warnings, "media tools missing: cannot convert office to pdf"), diag, nil
	}

	tmpDir, err := os.MkdirTemp("", "nb_office_pdf_*")
	if err != nil {
		return segs, assets, append(warnings, "temp dir err: "+err.Error()), diag, nil
	}
	defer os.RemoveAll(tmpDir)

	pdfPath, err := s.media.ConvertOfficeToPDF(ctx, officePath, tmpDir)
	if err != nil {
		return segs, assets, append(warnings, "office->pdf failed: "+err.Error()), diag, nil
	}

	// Treat PDF as primary: DocAI + Vision OCR fallback + render pages + caption pages.
	pdfSegs, pdfAssets, pdfWarn, pdfDiag, _ := s.handlePDF(ctx, mf, pdfPath)
	segs = append(segs, pdfSegs...)
	assets = append(assets, pdfAssets...)
	warnings = append(warnings, pdfWarn...)
	mergeDiag(diag, pdfDiag)

	return segs, assets, warnings, diag, nil
}

// -------------------- Image --------------------

func (s *contentExtractionService) handleImage(ctx context.Context, mf *types.MaterialFile, imgBytes []byte, imgPath string) ([]Segment, []AssetRef, []string, map[string]any, error) {
	diag := map[string]any{"pipeline": "image"}
	var warnings []string
	var assets []AssetRef
	var segs []Segment

	// OCR (Vision)
	if s.vision != nil {
		ocr, err := s.vision.OCRImageBytes(ctx, imgBytes, mf.MimeType)
		if err != nil {
			warnings = append(warnings, "vision image ocr failed: "+err.Error())
			diag["vision_error"] = err.Error()
		} else {
			diag["vision_primary_text_len"] = len(ocr.PrimaryText)
			// keep as a single segment
			if strings.TrimSpace(ocr.PrimaryText) != "" {
				segs = append(segs, Segment{
					Text: ocr.PrimaryText,
					Metadata: map[string]any{
						"kind": "ocr_text",
						"provider": "gcp_vision",
					},
				})
			}
		}
	} else {
		warnings = append(warnings, "vision provider unavailable; image OCR skipped")
	}

	// Caption image (meaning)
	// Upload derived copy (optional) — but we already have original. We caption the original asset URL if possible.
	origAsset := AssetRef{
		Kind: "image",
		Key:  mf.StorageKey,
		URL:  s.bucket.GetPublicURL(BucketCategoryMaterial, mf.StorageKey),
	}
	if s.caption != nil {
		capSegs, warn, err := s.captionAssetToSegments(ctx, "image_notes", origAsset, 1, nil, nil)
		if err != nil {
			warnings = append(warnings, "caption image failed: "+err.Error())
		} else {
			if warn != "" {
				warnings = append(warnings, warn)
			}
			segs = append(segs, capSegs...)
		}
	} else {
		warnings = append(warnings, "caption provider unavailable; image_notes skipped")
	}

	return segs, assets, warnings, diag, nil
}

// -------------------- Audio --------------------

func (s *contentExtractionService) handleAudio(ctx context.Context, mf *types.MaterialFile, audioPath string) ([]Segment, []AssetRef, []string, map[string]any, error) {
	diag := map[string]any{"pipeline": "audio"}
	var warnings []string
	var assets []AssetRef
	var segs []Segment

	if s.speech == nil {
		return nil, assets, []string{"speech provider unavailable"}, diag, nil
	}

	// Upload audio to GCS and transcribe via GCS (more scalable)
	key := fmt.Sprintf("%s/derived/audio/audio.wav", mf.StorageKey)
	if err := s.uploadLocalToGCS(ctx, nil, key, audioPath); err != nil {
		warnings = append(warnings, "upload audio failed: "+err.Error())
	} else {
		assets = append(assets, AssetRef{
			Kind: "audio",
			Key:  key,
			URL:  s.bucket.GetPublicURL(BucketCategoryMaterial, key),
		})
	}

	gcsURI := ""
	if s.materialBucketName != "" {
		gcsURI = fmt.Sprintf("gs://%s/%s", s.materialBucketName, key)
	}

	cfg := SpeechConfig{
		LanguageCode: "en-US",
		EnableAutomaticPunctuation: true,
		EnableWordTimeOffsets: true,
		EnableSpeakerDiarization: true,
		MinSpeakerCount: 1,
		MaxSpeakerCount: 6,
	}

	var res *SpeechResult
	var err error
	if gcsURI != "" {
		res, err = s.speech.TranscribeAudioGCS(ctx, gcsURI, cfg)
	} else {
		// fallback: bytes
		b, readErr := os.ReadFile(audioPath)
		if readErr != nil {
			return nil, assets, append(warnings, "read audio bytes failed: "+readErr.Error()), diag, nil
		}
		res, err = s.speech.TranscribeAudioBytes(ctx, b, "audio/wav", cfg)
	}

	if err != nil {
		warnings = append(warnings, "speech transcription failed: "+err.Error())
		diag["speech_error"] = err.Error()
	} else {
		diag["speech_primary_text_len"] = len(res.PrimaryText)
		for _, sg := range res.Segments {
			if sg.Metadata == nil {
				sg.Metadata = map[string]any{}
			}
			sg.Metadata["kind"] = "transcript"
			sg.Metadata["provider"] = "gcp_speech"
			segs = append(segs, sg)
		}
	}

	return segs, assets, warnings, diag, nil
}

// -------------------- Video --------------------

func (s *contentExtractionService) handleVideo(ctx context.Context, mf *types.MaterialFile, videoPath string) ([]Segment, []AssetRef, []string, map[string]any, error) {
	diag := map[string]any{"pipeline": "video"}
	var warnings []string
	var assets []AssetRef
	var segs []Segment

	// Optionally run Video Intelligence directly on the original video in GCS (extra signals)
	if s.videoAI != nil && s.materialBucketName != "" {
		gcsURI := fmt.Sprintf("gs://%s/%s", s.materialBucketName, mf.StorageKey)
		vres, err := s.videoAI.AnnotateVideoGCS(ctx, gcsURI, VideoAIConfig{
			LanguageCode: "en-US",
			Model: "video",
			EnableAutomaticPunctuation: true,
			EnableSpeakerDiarization: true,
			EnableSpeechTranscription: true,
			EnableTextDetection: true,
			EnableShotChangeDetection: true,
		})
		if err != nil {
			warnings = append(warnings, "video intelligence failed: "+err.Error())
			diag["videoai_error"] = err.Error()
		} else {
			diag["videoai_primary_text_len"] = len(vres.PrimaryText)
			// include as additional segments (do not replace our own pipeline)
			segs = append(segs, vres.TranscriptSegments...)
			segs = append(segs, vres.TextSegments...)
		}
	} else {
		warnings = append(warnings, "video intelligence unavailable or missing MATERIAL_GCS_BUCKET_NAME; skipping")
	}

	// (1) Extract audio with ffmpeg -> WAV/FLAC
	if s.media == nil {
		return segs, assets, append(warnings, "media tools missing: cannot extract audio/frames"), diag, nil
	}

	tmpDir, err := os.MkdirTemp("", "nb_video_*")
	if err != nil {
		return segs, assets, append(warnings, "temp dir error: "+err.Error()), diag, nil
	}
	defer os.RemoveAll(tmpDir)

	audioPath := filepath.Join(tmpDir, "audio.wav")
	_, err = s.media.ExtractAudioFromVideo(ctx, videoPath, audioPath, AudioExtractOptions{
		SampleRateHz: 16000,
		Channels: 1,
		Format: "wav",
	})
	if err != nil {
		warnings = append(warnings, "extract audio failed: "+err.Error())
	} else {
		// transcribe audio (Speech)
		aSegs, aAssets, aWarn, aDiag, _ := s.handleAudio(ctx, mf, audioPath)
		segs = append(segs, aSegs...)
		assets = append(assets, aAssets...)
		warnings = append(warnings, aWarn...)
		mergeDiag(diag, aDiag)
	}

	// (2) Extract keyframes
	framesDir := filepath.Join(tmpDir, "frames")
	frames, err := s.media.ExtractKeyframes(ctx, videoPath, framesDir, KeyframeOptions{
		IntervalSeconds: s.videoFrameIntervalSec,
		SceneThreshold:  s.videoSceneThreshold,
		Width: 1280,
		MaxFrames: s.maxFramesVideo,
		Format: "jpg",
		JPEGQuality: 3,
	})
	if err != nil {
		warnings = append(warnings, "extract keyframes failed: "+err.Error())
		return segs, assets, warnings, diag, nil
	}

	if len(frames) > s.maxFramesVideo {
		warnings = append(warnings, fmt.Sprintf("frames truncated: %d -> %d", len(frames), s.maxFramesVideo))
		frames = frames[:s.maxFramesVideo]
	}

	// Upload frames + OCR + caption
	frameAssets := make([]AssetRef, 0, len(frames))
	for i, fp := range frames {
		frameIdx := i + 1
		key := fmt.Sprintf("%s/derived/frames/frame_%06d.jpg", mf.StorageKey, frameIdx)
		if err := s.uploadLocalToGCS(ctx, nil, key, fp); err != nil {
			warnings = append(warnings, fmt.Sprintf("upload frame %d failed: %v", frameIdx, err))
			continue
		}
		frameAssets = append(frameAssets, AssetRef{
			Kind: "frame",
			Key:  key,
			URL:  s.bucket.GetPublicURL(BucketCategoryMaterial, key),
			Metadata: map[string]any{
				"frame_index": frameIdx,
			},
		})
	}
	assets = append(assets, frameAssets...)

	// OCR frames (Vision) + caption frames (CaptionProvider)
	if s.vision != nil {
		for i, a := range frameAssets {
			// Read frame bytes for OCR and (if small enough) caption
			localPath := frames[i] // aligned by index if uploads succeeded for that frame; but uploads might fail.
			// safer: read from disk if exists, else skip OCR.
			b, readErr := os.ReadFile(localPath)
			if readErr != nil {
				continue
			}

			ocr, err := s.vision.OCRImageBytes(ctx, b, "image/jpeg")
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("frame ocr failed (%s): %v", a.Key, err))
			} else if strings.TrimSpace(ocr.PrimaryText) != "" {
				// no timestamps yet (we’d need frame time mapping); store as frame index metadata
				segs = append(segs, Segment{
					Text: ocr.PrimaryText,
					Metadata: map[string]any{
						"kind": "frame_ocr",
						"asset_key": a.Key,
						"frame_index": a.Metadata["frame_index"],
						"provider": "gcp_vision",
					},
				})
			}

			// Caption frames (frame_notes)
			if s.caption != nil {
				// We do not know timestamp per frame without ffprobe; still valuable as notes.
				noteSegs, warn, err := s.captionAssetToSegments(ctx, "frame_notes", a, 0, nil, nil)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("frame caption failed (%s): %v", a.Key, err))
				} else {
					if warn != "" {
						warnings = append(warnings, warn)
					}
					segs = append(segs, noteSegs...)
				}
			}
			if i+1 >= s.maxFramesCaption {
				warnings = append(warnings, fmt.Sprintf("frame caption capped at %d frames", s.maxFramesCaption))
				break
			}
		}
	} else {
		warnings = append(warnings, "vision provider unavailable; frame OCR skipped")
	}

	return segs, assets, warnings, diag, nil
}

// -------------------- Caption helper --------------------

func (s *contentExtractionService) captionAssetToSegments(
	ctx context.Context,
	task string,
	asset AssetRef,
	page int,
	startSec *float64,
	endSec *float64,
) ([]Segment, string, error) {
	if s.caption == nil {
		return nil, "caption provider unavailable", nil
	}

	// Use URL if available; else try data URL fallback (download bytes)
	req := CaptionRequest{
		Task: task,
		Prompt: "",
		ImageURL: asset.URL,
		Detail: "high",
		MaxTokens: 1200,
	}
	// If URL empty or we want deterministic access, use bytes (data URL) if small enough
	if strings.TrimSpace(req.ImageURL) == "" {
		// cannot caption
		return nil, "caption skipped: missing asset url", nil
	}

	res, err := s.caption.DescribeImage(ctx, req)
	if err != nil {
		return nil, "", err
	}

	// Convert CaptionResult into one or more segments
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
		"kind": task,
		"asset_key": asset.Key,
		"provider": "openai_caption",
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

	seg := Segment{
		Text: txt,
		Metadata: md,
	}
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

// -------------------- DocAI helper --------------------

func (s *contentExtractionService) tryDocAI(ctx context.Context, mf *types.MaterialFile, mimeType, storageKey, originalName string) (*DocAIResult, error) {
	if s.docai == nil {
		return nil, fmt.Errorf("docai provider nil")
	}
	if s.docaiProjectID == "" || s.docaiProcessorID == "" || s.docaiLocation == "" {
		return nil, fmt.Errorf("missing docai env (GCP_PROJECT_ID, DOCUMENTAI_LOCATION, DOCUMENTAI_PROCESSOR_ID)")
	}

	// Online bytes is safest for single file, but can hit size limits.
	// We call bytes for maximum determinism (and because we already downloaded to disk).
	// For very large PDFs you can add batch mode later; this service supports batch through DocumentProviderService.
	data, err := os.ReadFile(s.safeOriginalTempPath(mf))
	_ = err
	// We do not have direct access to orig bytes here; caller passes file path.
	// In this implementation we use GCS online processing to avoid re-reading local path.
	// Using gs:// URI is robust and avoids request size limits.
	if s.materialBucketName == "" {
		return nil, fmt.Errorf("missing MATERIAL_GCS_BUCKET_NAME for docai gs:// calls")
	}
	gcsURI := fmt.Sprintf("gs://%s/%s", s.materialBucketName, storageKey)

	return s.docai.ProcessGCSOnline(ctx, DocAIProcessGCSRequest{
		ProjectID: s.docaiProjectID,
		Location:  s.docaiLocation,
		ProcessorID: s.docaiProcessorID,
		ProcessorVersion: s.docaiProcessorVer,
		MimeType: mimeType,
		GCSURI:   gcsURI,
		FieldMask: nil,
	})
}

// safeOriginalTempPath is a placeholder; we avoid it by using gs:// in tryDocAI.
// Kept to avoid compilation warnings if you adapt later.
func (s *contentExtractionService) safeOriginalTempPath(mf *types.MaterialFile) string {
	return ""
}

// -------------------- Native extraction fallback --------------------

func (s *contentExtractionService) bestEffortNativeText(name, mime string, data []byte) (string, string, map[string]any) {
	diag := map[string]any{"native": true}
	if len(data) == 0 {
		diag["empty"] = true
		return "", "native extraction skipped (no bytes)", diag
	}
	txt, err := ExtractTextStrict(name, mime, data)
	if err != nil {
		diag["err"] = err.Error()
		return "", "native extraction failed: " + err.Error(), diag
	}
	txt = collapseWhitespace(txt)
	if strings.TrimSpace(txt) == "" {
		return "", "native extraction produced empty text", diag
	}
	return txt, "", diag
}

// -------------------- Download + temp --------------------

func (s *contentExtractionService) downloadMaterialToTemp(ctx context.Context, mf *types.MaterialFile) (string, func(), []byte, error) {
	rc, err := s.bucket.DownloadFile(ctx, BucketCategoryMaterial, mf.StorageKey)
	if err != nil {
		return "", func() {}, nil, fmt.Errorf("download gcs object: %w", err)
	}
	defer rc.Close()

	tmpDir, err := os.MkdirTemp("", "nb_material_*")
	if err != nil {
		return "", func() {}, nil, fmt.Errorf("temp dir: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(mf.OriginalName))
	if ext == "" {
		ext = ".bin"
	}
	path := filepath.Join(tmpDir, "original"+ext)

	f, err := os.Create(path)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", func() {}, nil, fmt.Errorf("create temp file: %w", err)
	}
	defer f.Close()

	// Stream with limit
	var buf bytes.Buffer
	bw := bufio.NewWriterSize(f, 1024*1024)
	tee := io.TeeReader(io.LimitReader(rc, s.maxBytesDownload), &buf)

	_, copyErr := io.Copy(bw, tee)
	_ = bw.Flush()

	if copyErr != nil && !errors.Is(copyErr, io.EOF) {
		_ = os.RemoveAll(tmpDir)
		return "", func() {}, nil, fmt.Errorf("write temp: %w", copyErr)
	}

	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	// Keep bytes for “small enough” files to avoid re-reading.
	b := buf.Bytes()
	if int64(len(b)) > 10*1024*1024 { // >10MB, don't keep in memory
		b = nil
	}
	return path, cleanup, b, nil
}

// -------------------- Persist segments as chunks --------------------

func (s *contentExtractionService) persistSegmentsAsChunks(ctx context.Context, tx *gorm.DB, mf *types.MaterialFile, segs []Segment) error {
	transaction := tx
	if transaction == nil {
		transaction = s.db
	}

	// Convert segments into chunk rows (chunk long texts while preserving provenance)
	now := time.Now()
	chunks := make([]*types.MaterialChunk, 0, len(segs)*2)

	idx := 0
	for _, seg := range segs {
		text := strings.TrimSpace(seg.Text)
		if text == "" {
			continue
		}

		meta := map[string]any{}
		for k, v := range seg.Metadata {
			meta[k] = v
		}
		if seg.Page != nil {
			meta["page"] = *seg.Page
		}
		if seg.StartSec != nil {
			meta["start_sec"] = *seg.StartSec
		}
		if seg.EndSec != nil {
			meta["end_sec"] = *seg.EndSec
		}
		if seg.Confidence != nil {
			meta["confidence"] = *seg.Confidence
		}
		if seg.SpeakerTag != nil {
			meta["speaker_tag"] = *seg.SpeakerTag
		}

		parts := splitIntoChunks(text, s.chunkSize, s.chunkOverlap)
		for _, ptxt := range parts {
			chunks = append(chunks, &types.MaterialChunk{
				ID:             uuid.New(),
				MaterialFileID: mf.ID,
				Index:          idx,
				Text:           ptxt,
				Embedding:      datatypes.JSON([]byte(`[]`)),
				Metadata:       datatypes.JSON(mustJSON(meta)),
				CreatedAt:      now,
				UpdatedAt:      now,
			})
			idx++
		}
	}

	if len(chunks) == 0 {
		// Always persist at least one explicit chunk so the job doesn't break downstream.
		chunks = append(chunks, &types.MaterialChunk{
			ID:             uuid.New(),
			MaterialFileID: mf.ID,
			Index:          0,
			Text:           "No extractable content was produced for this file.",
			Embedding:      datatypes.JSON([]byte(`[]`)),
			Metadata:       datatypes.JSON(mustJSON(map[string]any{"kind": "unextractable"})),
			CreatedAt:      now,
			UpdatedAt:      now,
		})
	}

	// Insert. We do not delete existing chunks here; caller should ensure idempotency by checking before calling.
	if _, err := s.materialChunkRepo.Create(ctx, transaction, chunks); err != nil {
		return fmt.Errorf("create material chunks: %w", err)
	}
	return nil
}

func (s *contentExtractionService) updateMaterialFileExtractionStatus(ctx context.Context, tx *gorm.DB, mf *types.MaterialFile, kind string, warnings []string, diag map[string]any) error {
	transaction := tx
	if transaction == nil {
		transaction = s.db
	}

	payload := map[string]any{
		"kind": kind,
		"warnings": warnings,
		"diagnostics": diag,
		"extracted_at": time.Now().UTC().Format(time.RFC3339),
	}
	b, _ := json.Marshal(payload)

	updates := map[string]any{
		"ai_type":     kind,
		"ai_topics":   datatypes.JSON(b),
		"updated_at":  time.Now(),
	}
	if err := transaction.WithContext(ctx).Model(&types.MaterialFile{}).
		Where("id = ?", mf.ID).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("update material_file extraction status: %w", err)
	}
	mf.AIType = kind
	mf.AITopics = datatypes.JSON(b)
	return nil
}

// -------------------- Upload helpers --------------------

func (s *contentExtractionService) uploadLocalToGCS(ctx context.Context, tx *gorm.DB, key string, localPath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return s.bucket.UploadFile(ctx, tx, BucketCategoryMaterial, key, f)
}

// -------------------- Segment utilities --------------------

func normalizeSegments(in []Segment) []Segment {
	out := make([]Segment, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		t := strings.TrimSpace(s.Text)
		if t == "" {
			continue
		}
		// simple de-dupe by hash of text + key provenance (page/start/end/kind)
		key := segmentKey(s)
		if seen[key] {
			continue
		}
		seen[key] = true
		s.Text = t
		out = append(out, s)
	}
	return out
}

func segmentKey(s Segment) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(s.Text))
	b.WriteString("|")
	if s.Page != nil {
		b.WriteString(fmt.Sprintf("p=%d", *s.Page))
	}
	if s.StartSec != nil {
		b.WriteString(fmt.Sprintf("s=%.3f", *s.StartSec))
	}
	if s.EndSec != nil {
		b.WriteString(fmt.Sprintf("e=%.3f", *s.EndSec))
	}
	if s.Metadata != nil {
		if k, ok := s.Metadata["kind"]; ok {
			b.WriteString(fmt.Sprintf("|k=%v", k))
		}
		if ak, ok := s.Metadata["asset_key"]; ok {
			b.WriteString(fmt.Sprintf("|a=%v", ak))
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func joinSegmentsText(segs []Segment) string {
	var b strings.Builder
	for _, s := range segs {
		t := strings.TrimSpace(s.Text)
		if t == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(t)
	}
	return b.String()
}

func tagSegments(in []Segment, extra map[string]any) []Segment {
	out := make([]Segment, 0, len(in))
	for _, s := range in {
		if s.Metadata == nil {
			s.Metadata = map[string]any{}
		}
		for k, v := range extra {
			s.Metadata[k] = v
		}
		out = append(out, s)
	}
	return out
}

func textSignalWeak(segs []Segment) bool {
	// If we have less than ~500 chars of doc text, treat as weak.
	total := 0
	for _, s := range segs {
		k := ""
		if s.Metadata != nil {
			if vv, ok := s.Metadata["kind"].(string); ok {
				k = vv
			}
		}
		if k == "table_text" || k == "form_text" || k == "docai_page_text" || k == "ocr_text" {
			total += len(s.Text)
		}
	}
	return total < 500
}

func splitIntoChunks(text string, chunkSize int, overlap int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if chunkSize < 200 {
		chunkSize = 200
	}
	if overlap < 0 {
		overlap = 0
	}
	step := chunkSize - overlap
	if step <= 0 {
		step = chunkSize
	}

	out := []string{}
	for start := 0; start < len(text); start += step {
		end := start + chunkSize
		if end > len(text) {
			end = len(text)
		}
		p := strings.TrimSpace(text[start:end])
		if p != "" {
			out = append(out, p)
		}
		if end == len(text) {
			break
		}
	}
	return out
}

func mergeDiag(dst map[string]any, src map[string]any) {
	if dst == nil || src == nil {
		return
	}
	for k, v := range src {
		dst[k] = v
	}
}

func ensureGSPrefix(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if !strings.HasSuffix(s, "/") {
		s += "/"
	}
	return s
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// classifyKind uses mime/ext/path to route.
// For strictness, we trust mime first, then extension.
func classifyKind(name, mime string, smallBytes []byte, path string) string {
	m := strings.ToLower(strings.TrimSpace(mime))
	ext := strings.ToLower(filepath.Ext(name))

	if strings.HasPrefix(m, "video/") || ext == ".mp4" || ext == ".mov" || ext == ".webm" || ext == ".mkv" {
		return "video"
	}
	if strings.HasPrefix(m, "audio/") || ext == ".mp3" || ext == ".wav" || ext == ".m4a" || ext == ".flac" || ext == ".ogg" || ext == ".opus" {
		return "audio"
	}
	if strings.HasPrefix(m, "image/") || ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp" {
		return "image"
	}
	if m == "application/pdf" || ext == ".pdf" || isPDFHeader(smallBytes) {
		return "pdf"
	}
	if ext == ".docx" || strings.Contains(m, "wordprocessingml") {
		return "docx"
	}
	if ext == ".pptx" || strings.Contains(m, "presentationml") {
		return "pptx"
	}
	if strings.HasPrefix(m, "text/") || ext == ".txt" || ext == ".md" || ext == ".html" {
		return "text"
	}
	return "unknown"
}

func isPDFHeader(b []byte) bool {
	if len(b) < 5 {
		return false
	}
	return string(b[:5]) == "%PDF-"
}

func osGet(k, def string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	return v
}

func defaultCtx(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}











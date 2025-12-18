package extractor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/clients/gcp"
	"github.com/yungbote/neurobridge-backend/internal/clients/localmedia"
	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type Extractor struct {
	DB  *gorm.DB
	Log *logger.Logger

	MaterialChunkRepo repos.MaterialChunkRepo
	MaterialFileRepo  repos.MaterialFileRepo

	Bucket gcp.BucketService
	Media  localmedia.MediaToolsService

	DocAI   gcp.Document
	Vision  gcp.Vision
	Speech  gcp.Speech
	VideoAI gcp.Video
	Caption openai.Caption

	// env-backed settings
	MaterialBucketName string
	VisionOutputPrefix string
	DocAIProjectID     string
	DocAILocation      string
	DocAIProcessorID   string
	DocAIProcessorVer  string

	// hard caps (preserved)
	MaxBytesDownload          int64
	MaxPDFPagesRender         int
	MaxPDFPagesCaption        int
	MaxFramesVideo            int
	MaxFramesCaption          int
	MaxSecondsAudioTranscribe int
	MaxImageBytesDataURL      int64

	ChunkSize    int
	ChunkOverlap int

	VideoFrameIntervalSec float64
	VideoSceneThreshold   float64
}

func New(
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
) *Extractor {
	return &Extractor{
		DB:  db,
		Log: log.With("component", "IngestionExtractor"),

		MaterialChunkRepo: materialChunkRepo,
		MaterialFileRepo:  materialFileRepo,

		Bucket: bucket,
		Media:  media,

		DocAI:   docai,
		Vision:  vision,
		Speech:  speech,
		VideoAI: videoAI,
		Caption: caption,

		MaterialBucketName: strings.TrimSpace(os.Getenv("MATERIAL_GCS_BUCKET_NAME")),
		VisionOutputPrefix: strings.TrimSpace(os.Getenv("VISION_OCR_OUTPUT_PREFIX")),
		DocAIProjectID:     strings.TrimSpace(os.Getenv("GCP_PROJECT_ID")),
		DocAILocation:      strings.TrimSpace(os.Getenv("DOCUMENTAI_LOCATION")),
		DocAIProcessorID:   strings.TrimSpace(os.Getenv("DOCUMENTAI_PROCESSOR_ID")),
		DocAIProcessorVer:  strings.TrimSpace(os.Getenv("DOCUMENTAI_PROCESSOR_VERSION")),

		MaxBytesDownload:          1024 * 1024 * 1024,
		MaxPDFPagesRender:         200,
		MaxPDFPagesCaption:        60,
		MaxFramesVideo:            200,
		MaxFramesCaption:          60,
		MaxSecondsAudioTranscribe: 4 * 60 * 60,
		MaxImageBytesDataURL:      3 * 1024 * 1024,

		ChunkSize:    1200,
		ChunkOverlap: 200,

		VideoFrameIntervalSec: 2.0,
		VideoSceneThreshold:   0.0,
	}
}

func (e *Extractor) BestEffortNativeText(name, mime string, data []byte) (string, string, map[string]any) {
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

func (e *Extractor) TryDocAI(ctx context.Context, mimeType, storageKey string) (*gcp.DocAIResult, error) {
	if e.DocAI == nil {
		return nil, fmt.Errorf("docai provider nil")
	}
	if e.DocAIProjectID == "" || e.DocAIProcessorID == "" || e.DocAILocation == "" {
		return nil, fmt.Errorf("missing docai env (GCP_PROJECT_ID, DOCUMENTAI_LOCATION, DOCUMENTAI_PROCESSOR_ID)")
	}
	if e.MaterialBucketName == "" {
		return nil, fmt.Errorf("missing MATERIAL_GCS_BUCKET_NAME for docai gs:// calls")
	}

	gcsURI := fmt.Sprintf("gs://%s/%s", e.MaterialBucketName, storageKey)

	return e.DocAI.ProcessGCSOnline(ctx, gcp.DocAIProcessGCSRequest{
		ProjectID:        e.DocAIProjectID,
		Location:         e.DocAILocation,
		ProcessorID:      e.DocAIProcessorID,
		ProcessorVersion: e.DocAIProcessorVer,
		MimeType:         mimeType,
		GCSURI:           gcsURI,
		FieldMask:        nil,
	})
}

func (e *Extractor) DownloadMaterialToTemp(ctx context.Context, mf *types.MaterialFile) (string, func(), []byte, error) {
	rc, err := e.Bucket.DownloadFile(ctx, gcp.BucketCategoryMaterial, mf.StorageKey)
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

	var buf bytes.Buffer
	bw := bufio.NewWriterSize(f, 1024*1024)
	tee := io.TeeReader(io.LimitReader(rc, e.MaxBytesDownload), &buf)

	_, copyErr := io.Copy(bw, tee)
	_ = bw.Flush()

	if copyErr != nil && !errors.Is(copyErr, io.EOF) {
		_ = os.RemoveAll(tmpDir)
		return "", func() {}, nil, fmt.Errorf("write temp: %w", copyErr)
	}

	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	b := buf.Bytes()
	if int64(len(b)) > 10*1024*1024 {
		b = nil
	}
	return path, cleanup, b, nil
}

func (e *Extractor) UploadLocalToGCS(ctx context.Context, tx *gorm.DB, key string, localPath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return e.Bucket.UploadFile(ctx, tx, gcp.BucketCategoryMaterial, key, f)
}

func (e *Extractor) PersistSegmentsAsChunks(ctx context.Context, tx *gorm.DB, mf *types.MaterialFile, segs []Segment) error {
	transaction := tx
	if transaction == nil {
		transaction = e.DB
	}

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

		parts := SplitIntoChunks(text, e.ChunkSize, e.ChunkOverlap)

		for _, ptxt := range parts {
			ptxt = strings.TrimSpace(ptxt)
			if ptxt == "" {
				continue
			}
			ptxt = sanitizeUTF8(ptxt)
			chunks = append(chunks, &types.MaterialChunk{
				ID:             uuid.New(),
				MaterialFileID: mf.ID,
				Index:          idx,
				Text:           ptxt,
				Embedding:      datatypes.JSON(nil),
				Metadata:       datatypes.JSON(mustJSON(meta)),
				CreatedAt:      now,
				UpdatedAt:      now,
			})
			idx++
		}
	}

	if len(chunks) == 0 {
		chunks = append(chunks, &types.MaterialChunk{
			ID:             uuid.New(),
			MaterialFileID: mf.ID,
			Index:          0,
			Text:           "No extractable content was produced for this file.",
			Embedding:      datatypes.JSON(nil),
			Metadata:       datatypes.JSON(mustJSON(map[string]any{"kind": "unextractable"})),
			CreatedAt:      now,
			UpdatedAt:      now,
		})
	}

	if _, err := e.MaterialChunkRepo.Create(ctx, transaction, chunks); err != nil {
		return fmt.Errorf("create material chunks: %w", err)
	}
	return nil
}

func (e *Extractor) UpdateMaterialFileExtractionStatus(ctx context.Context, tx *gorm.DB, mf *types.MaterialFile, kind string, warnings []string, diag map[string]any) error {
	transaction := tx
	if transaction == nil {
		transaction = e.DB
	}

	payload := map[string]any{
		"kind":         kind,
		"warnings":     warnings,
		"diagnostics":  diag,
		"extracted_at": time.Now().UTC().Format(time.RFC3339),
	}
	b, _ := json.Marshal(payload)

	updates := map[string]any{
		"ai_type":    kind,
		"ai_topics":  datatypes.JSON(b),
		"updated_at": time.Now(),
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

// Placeholder preserved from old file (kept to avoid API churn if you later use it).
func (e *Extractor) SafeOriginalTempPath(mf *types.MaterialFile) string { return "" }

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
	"strconv"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/localmedia"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
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
	ObjectStorageMode  gcp.ObjectStorageMode
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
	storageMode := gcp.ObjectStorageModeGCS
	if storageCfg, err := gcp.ResolveObjectStorageConfigFromEnv(); err == nil {
		storageMode = storageCfg.Mode
	}
	maxPDFPagesRender := envIntAllowZero("INGEST_PDF_MAX_PAGES_RENDER", 200)
	if maxPDFPagesRender < 0 {
		maxPDFPagesRender = 0
	}
	maxPDFPagesCaption := envIntAllowZero("INGEST_PDF_MAX_PAGES_CAPTION", 60)
	if maxPDFPagesCaption < 0 {
		maxPDFPagesCaption = 0
	}
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

		ObjectStorageMode:  storageMode,
		MaterialBucketName: strings.TrimSpace(os.Getenv("MATERIAL_GCS_BUCKET_NAME")),
		VisionOutputPrefix: strings.TrimSpace(os.Getenv("VISION_OCR_OUTPUT_PREFIX")),
		DocAIProjectID:     strings.TrimSpace(os.Getenv("GCP_PROJECT_ID")),
		DocAILocation:      strings.TrimSpace(os.Getenv("DOCUMENTAI_LOCATION")),
		DocAIProcessorID:   strings.TrimSpace(os.Getenv("DOCUMENTAI_PROCESSOR_ID")),
		DocAIProcessorVer:  strings.TrimSpace(os.Getenv("DOCUMENTAI_PROCESSOR_VERSION")),

		// TODO: Get rid of infile hardcode values
		MaxBytesDownload:          1024 * 1024 * 1024,
		MaxPDFPagesRender:         maxPDFPagesRender,
		MaxPDFPagesCaption:        maxPDFPagesCaption,
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

func (e *Extractor) IsObjectStorageEmulatorMode() bool {
	return e != nil && gcp.IsEmulatorObjectStorageMode(e.ObjectStorageMode)
}

func (e *Extractor) ShouldAttemptVideoAIGCS() bool {
	if e == nil || e.VideoAI == nil {
		return false
	}
	return true
}

func envIntAllowZero(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
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
	if e.IsObjectStorageEmulatorMode() {
		if e.Bucket == nil {
			return nil, fmt.Errorf("bucket service nil; required for docai bytes fallback in emulator mode")
		}
		rc, err := e.Bucket.DownloadFile(ctx, gcp.BucketCategoryMaterial, storageKey)
		if err != nil {
			return nil, fmt.Errorf("docai bytes fallback download failed: %w", err)
		}
		defer rc.Close()
		b, err := io.ReadAll(rc)
		if err != nil {
			return nil, fmt.Errorf("docai bytes fallback read failed: %w", err)
		}
		return e.DocAI.ProcessBytes(ctx, gcp.DocAIProcessBytesRequest{
			ProjectID:        e.DocAIProjectID,
			Location:         e.DocAILocation,
			ProcessorID:      e.DocAIProcessorID,
			ProcessorVersion: e.DocAIProcessorVer,
			MimeType:         mimeType,
			Data:             b,
			FieldMask:        nil,
		})
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

func (e *Extractor) UploadLocalToGCS(dbc dbctx.Context, key string, localPath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return e.Bucket.UploadFile(dbc, gcp.BucketCategoryMaterial, key, f)
}

func (e *Extractor) PersistSegmentsAsChunks(dbc dbctx.Context, mf *types.MaterialFile, segs []Segment) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = e.DB
	}

	now := time.Now()
	chunks := make([]*types.MaterialChunk, 0, len(segs)*2)
	chunkRefRows := make([]*types.MaterialChunkReference, 0, 32)

	var refRows []*types.MaterialReference
	refByLabel := map[string]*types.MaterialReference{}
	refByAuthorYear := map[string]*types.MaterialReference{}
	labelSet := map[string]bool{}
	authorYearSet := map[string]bool{}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("BIBLIOGRAPHY_PARSE_ENABLED")), "") || strings.EqualFold(strings.TrimSpace(os.Getenv("BIBLIOGRAPHY_PARSE_ENABLED")), "true") {
		bibEntries := ParseBibliography(JoinSegmentsText(segs))
		if len(bibEntries) > 0 {
			for i, e := range bibEntries {
				label := strings.TrimSpace(e.Label)
				if label == "" {
					label = fmt.Sprintf("ref_%d", i+1)
				}
				if refByLabel[label] != nil {
					continue
				}
				authorsJSON, _ := json.Marshal(dedupeStrings(e.Authors))
				metaJSON, _ := json.Marshal(map[string]any{"source": "bibliography_parse"})
				row := &types.MaterialReference{
					ID:             uuid.New(),
					MaterialFileID: mf.ID,
					Label:          label,
					Raw:            strings.TrimSpace(e.Raw),
					Authors:        datatypes.JSON(authorsJSON),
					Title:          strings.TrimSpace(e.Title),
					Year:           e.Year,
					DOI:            strings.TrimSpace(e.DOI),
					Metadata:       datatypes.JSON(metaJSON),
					CreatedAt:      now,
					UpdatedAt:      now,
				}
				refRows = append(refRows, row)
				refByLabel[label] = row
				labelSet[label] = true
				if key := authorYearKey(e); key != "" {
					refByAuthorYear[key] = row
					authorYearSet[key] = true
				}
			}
		}
	}

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

		cleanText, eqs := ExtractLatexEquations(text)
		parts := SplitIntoChunks(cleanText, e.ChunkSize, e.ChunkOverlap)

		for _, ptxt := range parts {
			ptxt = strings.TrimSpace(ptxt)
			if ptxt == "" {
				continue
			}
			ptxt = sanitizeUTF8(ptxt)
			localMeta := meta
			if len(eqs) > 0 {
				chEqs := equationsForChunk(ptxt, eqs)
				if len(chEqs) > 0 {
					// Copy meta to avoid mutating the shared map between chunks.
					tmp := map[string]any{}
					for k, v := range meta {
						tmp[k] = v
					}
					eqList := make([]map[string]any, 0, len(chEqs))
					latexList := make([]string, 0, len(chEqs))
					placeholders := make([]string, 0, len(chEqs))
					for _, eq := range chEqs {
						if strings.TrimSpace(eq.Latex) == "" {
							continue
						}
						eqList = append(eqList, map[string]any{
							"placeholder": eq.Placeholder,
							"latex":       eq.Latex,
							"display":     eq.Display,
						})
						latexList = append(latexList, eq.Latex)
						placeholders = append(placeholders, eq.Placeholder)
					}
					if len(eqList) > 0 {
						tmp["equations"] = eqList
						tmp["equation_latex"] = dedupeStrings(latexList)
						tmp["equation_placeholders"] = dedupeStrings(placeholders)
						tmp["equation_source"] = "latex_delimiters"
						localMeta = tmp
					}
				}
			}
			var page *int
			if seg.Page != nil {
				p := *seg.Page
				page = &p
			}
			chunk := &types.MaterialChunk{
				ID:             uuid.New(),
				MaterialFileID: mf.ID,
				Index:          idx,
				Text:           ptxt,
				Embedding:      datatypes.JSON(nil),
				Page:           page,
				Metadata:       datatypes.JSON(mustJSON(localMeta)),
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			if len(refRows) > 0 {
				links := ExtractCitationLinks(ptxt, labelSet, authorYearSet)
				if len(links) > 0 {
					// Attach lightweight citation links to chunk metadata.
					tmp := map[string]any{}
					for k, v := range localMeta {
						tmp[k] = v
					}
					linkMeta := make([]map[string]any, 0, len(links))
					for _, link := range links {
						ref := refByLabel[link.Label]
						if link.Kind == "author_year" {
							ref = refByAuthorYear[link.Key]
						}
						if ref == nil {
							continue
						}
						linkMeta = append(linkMeta, map[string]any{
							"ref_id":           ref.ID.String(),
							"label":            ref.Label,
							"kind":             link.Kind,
							"match":            link.Match,
							"doi":              ref.DOI,
							"year":             ref.Year,
							"title":            ref.Title,
							"material_file_id": mf.ID.String(),
						})
						chunkRefRows = append(chunkRefRows, &types.MaterialChunkReference{
							ID:                  uuid.New(),
							MaterialChunkID:     chunk.ID,
							MaterialReferenceID: ref.ID,
							CitationText:        strings.TrimSpace(link.Match),
							CitationKind:        strings.TrimSpace(link.Kind),
							Metadata:            datatypes.JSON(mustJSON(map[string]any{"label": ref.Label})),
							CreatedAt:           now,
							UpdatedAt:           now,
						})
					}
					if len(linkMeta) > 0 {
						tmp["citation_refs"] = linkMeta
						chunk.Metadata = datatypes.JSON(mustJSON(tmp))
					}
				}
			}
			chunks = append(chunks, chunk)
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

	if len(refRows) > 0 {
		if err := transaction.WithContext(dbc.Ctx).Create(refRows).Error; err != nil {
			return fmt.Errorf("create material references: %w", err)
		}
	}
	if _, err := e.MaterialChunkRepo.Create(dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}, chunks); err != nil {
		return fmt.Errorf("create material chunks: %w", err)
	}
	if len(chunkRefRows) > 0 {
		if err := transaction.WithContext(dbc.Ctx).Create(chunkRefRows).Error; err != nil {
			return fmt.Errorf("create chunk reference joins: %w", err)
		}
	}
	return nil
}

func (e *Extractor) UpdateMaterialFileExtractionStatus(dbc dbctx.Context, mf *types.MaterialFile, kind string, warnings []string, diag map[string]any) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = e.DB
	}

	now := time.Now().UTC()
	updates := map[string]any{
		"ai_type":                kind,
		"ai_topics":              datatypes.JSON(mustJSON(map[string]any{"kind": kind, "warnings": warnings})),
		"extracted_kind":         kind,
		"extracted_at":           now,
		"extraction_warnings":    datatypes.JSON(mustJSON(warnings)),
		"extraction_diagnostics": datatypes.JSON(mustJSON(diag)),
		"updated_at":             now,
	}
	if err := transaction.WithContext(dbc.Ctx).Model(&types.MaterialFile{}).
		Where("id = ?", mf.ID).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("update material_file extraction status: %w", err)
	}
	mf.AIType = kind
	mf.AITopics = datatypes.JSON(mustJSON(map[string]any{"kind": kind, "warnings": warnings}))
	mf.ExtractedKind = kind
	mf.ExtractedAt = &now
	mf.ExtractionWarnings = datatypes.JSON(mustJSON(warnings))
	mf.ExtractionDiagnostics = datatypes.JSON(mustJSON(diag))
	return nil
}

// Placeholder preserved from old file (kept to avoid API churn if you later use it).
func (e *Extractor) SafeOriginalTempPath(mf *types.MaterialFile) string { return "" }

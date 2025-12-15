package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	vision "cloud.google.com/go/vision/apiv1"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
  visionpb "cloud.google.com/go/vision/v2/apiv1/visionpb"
	"github.com/yungbote/neurobridge-backend/internal/logger"
)

type VisionProviderService interface {
	// OCR on raw image bytes (DOCUMENT_TEXT_DETECTION) with structure + confidence.
	OCRImageBytes(ctx context.Context, img []byte, mimeType string) (*VisionOCRResult, error)

	// OCR a PDF/TIFF stored in GCS using async batch annotate. Results written to a GCS prefix,
	// then this method reads/parses them back into pages/segments.
	OCRFileInGCS(ctx context.Context, gcsSourceURI string, mimeType string, gcsOutputPrefix string, maxPages int) (*VisionOCRResult, error)

	Close() error
}

type VisionOCRResult struct {
	Provider      string         `json:"provider"` // "gcp_vision"
	SourceURI     string         `json:"source_uri,omitempty"`
	MimeType      string         `json:"mime_type,omitempty"`
	PrimaryText   string         `json:"primary_text"`
	Pages         []VisionOCRPage `json:"pages,omitempty"`
	Segments      []Segment      `json:"segments,omitempty"`
	OutputObjects []string       `json:"output_objects,omitempty"` // JSON files produced by async OCR
	Warnings      []string       `json:"warnings,omitempty"`
}

type VisionOCRPage struct {
	PageNumber int            `json:"page_number"`
	Text       string         `json:"text"`
	Confidence float64        `json:"confidence"`
	Blocks     []VisionBlock  `json:"blocks,omitempty"`
}

type VisionBlock struct {
	Confidence float64        `json:"confidence"`
	Bounding   *VisionBBox    `json:"bounding_box,omitempty"`
}

type VisionBBox struct {
	// normalized vertices (0..1) when available, else pixels may appear depending on response
	Vertices [][2]float64 `json:"vertices,omitempty"`
}

type visionProviderService struct {
	log *logger.Logger

	visionClient *vision.ImageAnnotatorClient
	storage      *storage.Client

	// hygiene options
	cleanOutputPrefix bool
	listRetry         int
	listRetryDelay    time.Duration
}

func NewVisionProviderService(log *logger.Logger) (VisionProviderService, error) {
	if log == nil {
		return nil, fmt.Errorf("logger required")
	}
	slog := log.With("service", "VisionProviderService")

	creds := strings.TrimSpace(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS_JSON"))
	if creds == "" {
		creds = strings.TrimSpace(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"))
	}

	ctx := context.Background()

	var (
		vClient *vision.ImageAnnotatorClient
		sClient *storage.Client
		err     error
	)

	if creds != "" {
		vClient, err = vision.NewImageAnnotatorClient(ctx, option.WithCredentialsFile(creds))
		if err != nil {
			return nil, fmt.Errorf("vision client: %w", err)
		}
		sClient, err = storage.NewClient(ctx, option.WithCredentialsFile(creds))
		if err != nil {
			_ = vClient.Close()
			return nil, fmt.Errorf("storage client: %w", err)
		}
	} else {
		// ADC (GKE/Cloud Run w/ attached SA)
		vClient, err = vision.NewImageAnnotatorClient(ctx)
		if err != nil {
			return nil, fmt.Errorf("vision client: %w", err)
		}
		sClient, err = storage.NewClient(ctx)
		if err != nil {
			_ = vClient.Close()
			return nil, fmt.Errorf("storage client: %w", err)
		}
	}

	return &visionProviderService{
		log:              slog,
		visionClient:      vClient,
		storage:           sClient,
		cleanOutputPrefix: true,               // prevents mixing old outputs
		listRetry:         12,                 // robust list/read loop
		listRetryDelay:    750 * time.Millisecond,
	}, nil
}

func (s *visionProviderService) Close() error {
	if s == nil {
		return nil
	}
	if s.visionClient != nil {
		_ = s.visionClient.Close()
	}
	if s.storage != nil {
		_ = s.storage.Close()
	}
	return nil
}

func (s *visionProviderService) OCRImageBytes(ctx context.Context, img []byte, mimeType string) (*VisionOCRResult, error) {
	if len(img) == 0 {
		return &VisionOCRResult{Provider: "gcp_vision", MimeType: mimeType, PrimaryText: ""}, nil
	}

	ctx = defaultCtx(ctx)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	vimg, err := vision.NewImageFromReader(bytes.NewReader(img))
	if err != nil {
		return nil, fmt.Errorf("vision NewImageFromReader: %w", err)
	}

	doc, err := s.visionClient.DetectDocumentText(ctx, vimg, nil)
	if err != nil {
		return nil, fmt.Errorf("vision DetectDocumentText: %w", err)
	}
	if doc == nil || strings.TrimSpace(doc.Text) == "" {
		return &VisionOCRResult{Provider: "gcp_vision", MimeType: mimeType, PrimaryText: ""}, nil
	}

	// Vision "Document" has pages/blocks confidence. Compute page confidence.
	pages := make([]VisionOCRPage, 0, len(doc.Pages))
	segs := make([]Segment, 0, len(doc.Pages))

	primary := collapseWhitespace(doc.Text)

	if len(doc.Pages) == 0 {
		// single page
		p := VisionOCRPage{
			PageNumber: 1,
			Text:       primary,
			Confidence: 0,
		}
		pages = append(pages, p)
		page := 1
		segs = append(segs, Segment{Text: primary, Page: &page, Metadata: map[string]any{"kind": "ocr_text", "provider": "gcp_vision"}})
		return &VisionOCRResult{
			Provider:    "gcp_vision",
			MimeType:    mimeType,
			PrimaryText: primary,
			Pages:       pages,
			Segments:    segs,
		}, nil
	}

	for i, pg := range doc.Pages {
		pageNum := i + 1
		pageText := primary // Vision doc.Text is full doc; we can't perfectly slice without anchors here
		conf := avgBlockConfidence(pg.Blocks)
		blocks := make([]VisionBlock, 0, len(pg.Blocks))
		for _, b := range pg.Blocks {
			blocks = append(blocks, VisionBlock{
				Confidence: b.Confidence,
				Bounding:   bboxFromVertices(b.BoundingBox.Vertices),
			})
		}
		pages = append(pages, VisionOCRPage{
			PageNumber: pageNum,
			Text:       pageText,
			Confidence: conf,
			Blocks:     blocks,
		})
		p := pageNum
		segs = append(segs, Segment{
			Text:       pageText,
			Page:       &p,
			Confidence: ptrFloat(conf),
			Metadata:   map[string]any{"kind": "ocr_text", "provider": "gcp_vision"},
		})
	}

	return &VisionOCRResult{
		Provider:    "gcp_vision",
		MimeType:    mimeType,
		PrimaryText: primary,
		Pages:       pages,
		Segments:    segs,
	}, nil
}

// OCRFileInGCS uses Vision asyncBatchAnnotate for PDF/TIFF in GCS and parses output JSON.
// docs describe `context.pageNumber` in responses. :contentReference[oaicite:6]{index=6}
func (s *visionProviderService) OCRFileInGCS(ctx context.Context, gcsSourceURI string, mimeType string, gcsOutputPrefix string, maxPages int) (*VisionOCRResult, error) {
	ctx = defaultCtx(ctx)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()

	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if mimeType == "" {
		mimeType = "application/pdf"
	}

	if !strings.HasPrefix(gcsSourceURI, "gs://") {
		return nil, fmt.Errorf("gcsSourceURI must be gs://... got %q", gcsSourceURI)
	}
	if !strings.HasPrefix(gcsOutputPrefix, "gs://") {
		return nil, fmt.Errorf("gcsOutputPrefix must be gs://... got %q", gcsOutputPrefix)
	}
	if !strings.HasSuffix(gcsOutputPrefix, "/") {
		gcsOutputPrefix += "/"
	}

	if maxPages <= 0 {
		maxPages = 200
	}

	// Hygiene: delete old outputs under prefix if enabled
	if s.cleanOutputPrefix {
		_ = s.deletePrefixBestEffort(ctx, gcsOutputPrefix)
	}

	req := &visionpb.AsyncBatchAnnotateFilesRequest{
		Requests: []*visionpb.AsyncAnnotateFileRequest{
			{
				Features: []*visionpb.Feature{
					{Type: visionpb.Feature_DOCUMENT_TEXT_DETECTION},
				},
				InputConfig: &visionpb.InputConfig{
					GcsSource: &visionpb.GcsSource{Uri: gcsSourceURI},
					MimeType:  mimeType, // "application/pdf" or "image/tiff"
				},
				OutputConfig: &visionpb.OutputConfig{
					GcsDestination: &visionpb.GcsDestination{Uri: gcsOutputPrefix},
					BatchSize:      10, // pages per output json
				},
			},
		},
	}

	op, err := s.visionClient.AsyncBatchAnnotateFiles(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("vision AsyncBatchAnnotateFiles: %w", err)
	}

	_, err = op.Wait(ctx)
	if err != nil {
		return nil, fmt.Errorf("vision operation wait: %w", err)
	}

	// List JSON outputs and parse.
	outBucket, outPrefix, err := parseGCSURI(gcsOutputPrefix)
	if err != nil {
		return nil, err
	}

	jsonKeys, err := s.listJSONWithRetry(ctx, outBucket, outPrefix)
	if err != nil {
		return nil, err
	}
	if len(jsonKeys) == 0 {
		return &VisionOCRResult{
			Provider:    "gcp_vision",
			SourceURI:   gcsSourceURI,
			MimeType:    mimeType,
			PrimaryText: "",
			Warnings:    []string{"no vision output JSON files found under output prefix"},
		}, nil
	}

	pages := make([]VisionOCRPage, 0, minInt(maxPages, 256))
	segs := make([]Segment, 0, minInt(maxPages, 256))

	var primary strings.Builder
	seen := 0

	for _, key := range jsonKeys {
		if seen >= maxPages {
			break
		}
		b, err := s.readObject(ctx, outBucket, key)
		if err != nil {
			return nil, fmt.Errorf("read vision output %s: %w", key, err)
		}

		pageObjs := parseVisionAsyncJSON(b, maxPages-seen)

		for _, po := range pageObjs {
			if seen >= maxPages {
				break
			}
			seen++
			pages = append(pages, po.Page)

			txt := strings.TrimSpace(po.Page.Text)
			if txt != "" {
				if primary.Len() > 0 {
					primary.WriteString("\n\n")
				}
				primary.WriteString(txt)
			}
			pn := po.Page.PageNumber
			conf := po.Page.Confidence
			segs = append(segs, Segment{
				Text:       txt,
				Page:       &pn,
				Confidence: ptrFloat(conf),
				Metadata: map[string]any{
					"kind":        "ocr_text",
					"provider":    "gcp_vision_async",
					"output_json": key,
				},
			})
		}
	}

	out := &VisionOCRResult{
		Provider:      "gcp_vision",
		SourceURI:     gcsSourceURI,
		MimeType:      mimeType,
		PrimaryText:   collapseWhitespace(primary.String()),
		Pages:         pages,
		Segments:      segs,
		OutputObjects: jsonKeys,
	}

	return out, nil
}

// ---------- internal helpers ----------

func defaultCtx(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func ptrFloat(v float64) *float64 { return &v }

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func avgBlockConfidence(blocks []vision.Block) float64 {
	if len(blocks) == 0 {
		return 0
	}
	var sum float64
	n := 0
	for _, b := range blocks {
		if b.Confidence > 0 {
			sum += float64(b.Confidence)
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func bboxFromVertices(vs []vision.Vertex) *VisionBBox {
	if len(vs) == 0 {
		return nil
	}
	out := make([][2]float64, 0, len(vs))
	for _, v := range vs {
		out = append(out, [2]float64{float64(v.X), float64(v.Y)})
	}
	return &VisionBBox{Vertices: out}
}

type parsedVisionPage struct {
	Page VisionOCRPage
}

// Parses Vision async output JSON (one output JSON file contains responses[] for multiple pages).
func parseVisionAsyncJSON(b []byte, maxPages int) []parsedVisionPage {
	if maxPages <= 0 {
		return nil
	}

	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return nil
	}

	respAny, ok := root["responses"].([]any)
	if !ok || len(respAny) == 0 {
		return nil
	}

	out := make([]parsedVisionPage, 0, minInt(maxPages, len(respAny)))

	for _, r := range respAny {
		if maxPages <= 0 {
			break
		}
		rm, _ := r.(map[string]any)
		if rm == nil {
			continue
		}

		// error field in response
		if e, ok := rm["error"].(map[string]any); ok && e != nil {
			// skip but don’t crash
			maxPages--
			continue
		}

		pageNum := 0
		if ctxAny, ok := rm["context"].(map[string]any); ok && ctxAny != nil {
			if pn, ok := ctxAny["pageNumber"]; ok {
				pageNum = int(anyToFloat64(pn, 0))
			}
		}
		if pageNum <= 0 {
			pageNum = len(out) + 1
		}

		txt := ""
		conf := 0.0

		if fta, ok := rm["fullTextAnnotation"].(map[string]any); ok && fta != nil {
			if t, ok := fta["text"].(string); ok {
				txt = collapseWhitespace(t)
			}
			// Try to compute confidence from pages[0].blocks[].confidence if present
			if pages, ok := fta["pages"].([]any); ok && len(pages) > 0 {
				if p0, ok := pages[0].(map[string]any); ok && p0 != nil {
					conf = avgVisionJSONBlocksConfidence(p0)
				}
			}
		}

		out = append(out, parsedVisionPage{
			Page: VisionOCRPage{
				PageNumber: pageNum,
				Text:       txt,
				Confidence: conf,
			},
		})
		maxPages--
	}

	return out
}

func anyToFloat64(v any, def float64) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	default:
		return def
	}
}

func avgVisionJSONBlocksConfidence(page map[string]any) float64 {
	blocksAny, ok := page["blocks"].([]any)
	if !ok || len(blocksAny) == 0 {
		return 0
	}
	var sum float64
	n := 0
	for _, b := range blocksAny {
		bm, _ := b.(map[string]any)
		if bm == nil {
			continue
		}
		c := anyToFloat64(bm["confidence"], 0)
		if c > 0 {
			sum += c
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return math.Max(0, math.Min(1, sum/float64(n)))
}

func (s *visionProviderService) listJSONWithRetry(ctx context.Context, bucket, prefix string) ([]string, error) {
	var lastErr error
	for attempt := 0; attempt < s.listRetry; attempt++ {
		keys, err := s.listObjects(ctx, bucket, prefix)
		if err == nil {
			jsonKeys := make([]string, 0, len(keys))
			for _, k := range keys {
				if strings.HasSuffix(strings.ToLower(k), ".json") {
					jsonKeys = append(jsonKeys, k)
				}
			}
			sort.Strings(jsonKeys)
			if len(jsonKeys) > 0 {
				return jsonKeys, nil
			}
			lastErr = fmt.Errorf("no json objects found yet under %s/%s", bucket, prefix)
		} else {
			lastErr = err
		}
		time.Sleep(s.listRetryDelay)
	}
	return nil, lastErr
}

func (s *visionProviderService) listObjects(ctx context.Context, bucket, prefix string) ([]string, error) {
	it := s.storage.Bucket(bucket).Objects(ctx, &storage.Query{Prefix: prefix})
	out := []string{}
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		out = append(out, attrs.Name)
	}
	return out, nil
}

func (s *visionProviderService) readObject(ctx context.Context, bucket, key string) ([]byte, error) {
	rc, err := s.storage.Bucket(bucket).Object(key).NewReader(ctx)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func (s *visionProviderService) deletePrefixBestEffort(ctx context.Context, gcsPrefixURI string) error {
	bucket, prefix, err := parseGCSURI(gcsPrefixURI)
	if err != nil {
		return err
	}
	keys, err := s.listObjects(ctx, bucket, prefix)
	if err != nil {
		return err
	}
	for _, k := range keys {
		_ = s.storage.Bucket(bucket).Object(k).Delete(ctx)
	}
	return nil
}

func parseGCSURI(uri string) (bucket, key string, err error) {
	if !strings.HasPrefix(uri, "gs://") {
		return "", "", fmt.Errorf("invalid gs uri: %q", uri)
	}
	trim := strings.TrimPrefix(uri, "gs://")
	parts := strings.SplitN(trim, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		return "", "", fmt.Errorf("invalid gs uri: %q", uri)
	}
	bucket = parts[0]
	if len(parts) == 1 {
		return bucket, "", nil
	}
	key = parts[1]
	return bucket, key, nil
}











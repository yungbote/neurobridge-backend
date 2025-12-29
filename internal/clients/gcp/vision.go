package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	vision "cloud.google.com/go/vision/v2/apiv1"
	visionpb "cloud.google.com/go/vision/v2/apiv1/visionpb"
	"google.golang.org/api/iterator"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type Vision interface {
	OCRImageBytes(ctx context.Context, img []byte, mimeType string) (*VisionOCRResult, error)
	OCRFileInGCS(ctx context.Context, gcsSourceURI string, mimeType string, gcsOutputPrefix string, maxPages int) (*VisionOCRResult, error)
	Close() error
}

type VisionOCRResult struct {
	Provider      string          `json:"provider"`
	SourceURI     string          `json:"source_uri,omitempty"`
	MimeType      string          `json:"mime_type,omitempty"`
	PrimaryText   string          `json:"primary_text"`
	Pages         []VisionOCRPage `json:"pages,omitempty"`
	Segments      []types.Segment `json:"segments,omitempty"`
	OutputObjects []string        `json:"output_objects,omitempty"`
	Warnings      []string        `json:"warnings,omitempty"`
}

type VisionOCRPage struct {
	PageNumber int           `json:"page_number"`
	Text       string        `json:"text"`
	Confidence float64       `json:"confidence"`
	Blocks     []VisionBlock `json:"blocks,omitempty"`
}

type VisionBlock struct {
	Confidence float64     `json:"confidence"`
	Bounding   *VisionBBox `json:"bounding_box,omitempty"`
}

type VisionBBox struct {
	Vertices [][2]float64 `json:"vertices,omitempty"`
}

type visionService struct {
	log *logger.Logger

	visionClient *vision.ImageAnnotatorClient
	storage      *storage.Client

	cleanOutputPrefix bool
	listRetry         int
	listRetryDelay    time.Duration
}

func NewVision(log *logger.Logger) (Vision, error) {
	if log == nil {
		return nil, fmt.Errorf("logger required")
	}
	slog := log.With("service", "gcp.Vision")

	ctx := context.Background()
	opts := ClientOptionsFromEnv()

	vClient, err := vision.NewImageAnnotatorClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("vision client: %w", err)
	}
	sClient, err := storage.NewClient(ctx, opts...)
	if err != nil {
		_ = vClient.Close()
		return nil, fmt.Errorf("storage client: %w", err)
	}

	return &visionService{
		log:               slog,
		visionClient:      vClient,
		storage:           sClient,
		cleanOutputPrefix: true,
		listRetry:         12,
		listRetryDelay:    750 * time.Millisecond,
	}, nil
}

func (s *visionService) Close() error {
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

func (s *visionService) OCRImageBytes(ctx context.Context, img []byte, mimeType string) (*VisionOCRResult, error) {
	if len(img) == 0 {
		return &VisionOCRResult{Provider: "gcp_vision", MimeType: mimeType, PrimaryText: ""}, nil
	}

	ctx = ctxutil.Default(ctx)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req := &visionpb.AnnotateImageRequest{
		Image: &visionpb.Image{Content: img},
		Features: []*visionpb.Feature{
			{Type: visionpb.Feature_DOCUMENT_TEXT_DETECTION},
		},
	}

	br := &visionpb.BatchAnnotateImagesRequest{Requests: []*visionpb.AnnotateImageRequest{req}}
	resp, err := s.visionClient.BatchAnnotateImages(ctx, br)
	if err != nil {
		return nil, fmt.Errorf("vision BatchAnnotateImages: %w", err)
	}
	if resp == nil || len(resp.Responses) == 0 || resp.Responses[0] == nil {
		return &VisionOCRResult{Provider: "gcp_vision", MimeType: mimeType, PrimaryText: ""}, nil
	}

	r0 := resp.Responses[0]
	if r0.Error != nil && r0.Error.Message != "" {
		return nil, fmt.Errorf("vision annotate error: %s", r0.Error.Message)
	}

	fta := r0.FullTextAnnotation
	if fta == nil || strings.TrimSpace(fta.Text) == "" {
		return &VisionOCRResult{Provider: "gcp_vision", MimeType: mimeType, PrimaryText: ""}, nil
	}

	primary := collapseWhitespace(fta.Text)

	pages := make([]VisionOCRPage, 0, len(fta.Pages))
	segs := make([]types.Segment, 0, len(fta.Pages))

	if len(fta.Pages) == 0 {
		pages = append(pages, VisionOCRPage{PageNumber: 1, Text: primary, Confidence: 0})
		p := 1
		segs = append(segs, types.Segment{
			Text:     primary,
			Page:     &p,
			Metadata: map[string]any{"kind": "ocr_text", "provider": "gcp_vision"},
		})
		return &VisionOCRResult{
			Provider:    "gcp_vision",
			MimeType:    mimeType,
			PrimaryText: primary,
			Pages:       pages,
			Segments:    segs,
		}, nil
	}

	for i, pg := range fta.Pages {
		if pg == nil {
			continue
		}
		pageNum := i + 1
		pageText := primary // no per-page slicing here

		conf := avgBlockConfidence(pg.Blocks)
		blocks := make([]VisionBlock, 0, len(pg.Blocks))
		for _, b := range pg.Blocks {
			if b == nil {
				continue
			}
			blocks = append(blocks, VisionBlock{
				Confidence: float64(b.Confidence),
				Bounding:   bboxFromBoundingPoly(b.BoundingBox),
			})
		}

		pages = append(pages, VisionOCRPage{
			PageNumber: pageNum,
			Text:       pageText,
			Confidence: conf,
			Blocks:     blocks,
		})

		p := pageNum
		segs = append(segs, types.Segment{
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

func (s *visionService) OCRFileInGCS(ctx context.Context, gcsSourceURI string, mimeType string, gcsOutputPrefix string, maxPages int) (*VisionOCRResult, error) {
	ctx = ctxutil.Default(ctx)
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
					MimeType:  mimeType,
				},
				OutputConfig: &visionpb.OutputConfig{
					GcsDestination: &visionpb.GcsDestination{Uri: gcsOutputPrefix},
					BatchSize:      10,
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
	segs := make([]types.Segment, 0, minInt(maxPages, 256))

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
			segs = append(segs, types.Segment{
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

	return &VisionOCRResult{
		Provider:      "gcp_vision",
		SourceURI:     gcsSourceURI,
		MimeType:      mimeType,
		PrimaryText:   collapseWhitespace(primary.String()),
		Pages:         pages,
		Segments:      segs,
		OutputObjects: jsonKeys,
	}, nil
}

// ---------- helpers ----------

func avgBlockConfidence(blocks []*visionpb.Block) float64 {
	if len(blocks) == 0 {
		return 0
	}
	var sum float64
	n := 0
	for _, b := range blocks {
		if b == nil {
			continue
		}
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

func bboxFromBoundingPoly(bp *visionpb.BoundingPoly) *VisionBBox {
	if bp == nil {
		return nil
	}
	if len(bp.NormalizedVertices) > 0 {
		out := make([][2]float64, 0, len(bp.NormalizedVertices))
		for _, v := range bp.NormalizedVertices {
			if v == nil {
				continue
			}
			out = append(out, [2]float64{float64(v.X), float64(v.Y)})
		}
		if len(out) == 0 {
			return nil
		}
		return &VisionBBox{Vertices: out}
	}
	if len(bp.Vertices) > 0 {
		out := make([][2]float64, 0, len(bp.Vertices))
		for _, v := range bp.Vertices {
			if v == nil {
				continue
			}
			out = append(out, [2]float64{float64(v.X), float64(v.Y)})
		}
		if len(out) == 0 {
			return nil
		}
		return &VisionBBox{Vertices: out}
	}
	return nil
}

type parsedVisionPage struct {
	Page VisionOCRPage
}

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

		if e, ok := rm["error"].(map[string]any); ok && e != nil {
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
			if pages, ok := fta["pages"].([]any); ok && len(pages) > 0 {
				if p0, ok := pages[0].(map[string]any); ok && p0 != nil {
					conf = avgVisionJSONBlocksConfidence(p0)
				}
			}
		}

		out = append(out, parsedVisionPage{
			Page: VisionOCRPage{PageNumber: pageNum, Text: txt, Confidence: conf},
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

func (s *visionService) listJSONWithRetry(ctx context.Context, bucket, prefix string) ([]string, error) {
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

func (s *visionService) listObjects(ctx context.Context, bucket, prefix string) ([]string, error) {
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

func (s *visionService) readObject(ctx context.Context, bucket, key string) ([]byte, error) {
	rc, err := s.storage.Bucket(bucket).Object(key).NewReader(ctx)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func (s *visionService) deletePrefixBestEffort(ctx context.Context, gcsPrefixURI string) error {
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

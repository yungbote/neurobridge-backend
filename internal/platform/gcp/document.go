package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/documentai/apiv1"
	"cloud.google.com/go/documentai/apiv1/documentaipb"
	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type Document interface {
	ProcessBytes(ctx context.Context, req DocAIProcessBytesRequest) (*DocAIResult, error)
	ProcessGCSOnline(ctx context.Context, req DocAIProcessGCSRequest) (*DocAIResult, error)
	BatchProcessGCS(ctx context.Context, req DocAIBatchRequest) (*DocAIBatchResult, error)
	Close() error
}

type DocAIProcessBytesRequest struct {
	ProjectID        string
	Location         string
	ProcessorID      string
	ProcessorVersion string
	MimeType         string
	Data             []byte
	FieldMask        []string
}

type DocAIProcessGCSRequest struct {
	ProjectID        string
	Location         string
	ProcessorID      string
	ProcessorVersion string
	MimeType         string
	GCSURI           string
	FieldMask        []string
}

type DocAIBatchRequest struct {
	ProjectID        string
	Location         string
	ProcessorID      string
	ProcessorVersion string
	MimeType         string

	InputGCSURI  string
	OutputGCSURI string
}

type DocAIResult struct {
	Provider     string          `json:"provider"`
	Processor    string          `json:"processor"`
	MimeType     string          `json:"mime_type"`
	PrimaryText  string          `json:"primary_text"`
	Segments     []types.Segment `json:"segments,omitempty"`
	Tables       []types.Segment `json:"tables,omitempty"`
	Forms        []types.Segment `json:"forms,omitempty"`
	DocumentJSON []byte          `json:"document_json,omitempty"`
	Warnings     []string        `json:"warnings,omitempty"`
}

type DocAIBatchResult struct {
	Provider      string   `json:"provider"`
	OperationName string   `json:"operation_name"`
	OutputObjects []string `json:"output_objects"`
	Documents     int      `json:"documents"`
}

type documentService struct {
	log *logger.Logger

	docClient *documentai.DocumentProcessorClient
	storage   *storage.Client

	listRetry      int
	listRetryDelay time.Duration
}

func NewDocument(log *logger.Logger) (Document, error) {
	if log == nil {
		return nil, fmt.Errorf("logger required")
	}
	slog := log.With("service", "gcp.Document")

	ctx := context.Background()

	location := strings.TrimSpace(os.Getenv("DOCUMENTAI_LOCATION"))
	if location == "" {
		location = "us"
	}
	endpoint := fmt.Sprintf("%s-documentai.googleapis.com:443", location)

	// DocumentAI needs endpoint; Storage does NOT.
	docOpts := append([]option.ClientOption{option.WithEndpoint(endpoint)}, ClientOptionsFromEnv()...)
	c, err := documentai.NewDocumentProcessorClient(ctx, docOpts...)
	if err != nil {
		return nil, fmt.Errorf("documentai client: %w", err)
	}

	stOpts := ClientOptionsFromEnv()
	st, err := storage.NewClient(ctx, stOpts...)
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("storage client: %w", err)
	}

	slog.Info("Document AI initialized", "endpoint", endpoint)

	return &documentService{
		log:            slog,
		docClient:      c,
		storage:        st,
		listRetry:      12,
		listRetryDelay: 750 * time.Millisecond,
	}, nil
}

func (s *documentService) Close() error {
	if s == nil {
		return nil
	}
	if s.docClient != nil {
		_ = s.docClient.Close()
	}
	if s.storage != nil {
		_ = s.storage.Close()
	}
	return nil
}

func (s *documentService) ProcessBytes(ctx context.Context, req DocAIProcessBytesRequest) (*DocAIResult, error) {
	ctx = ctxutil.Default(ctx)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	if len(req.Data) == 0 {
		return &DocAIResult{Provider: "gcp_documentai", MimeType: req.MimeType, PrimaryText: ""}, nil
	}
	if req.MimeType == "" {
		req.MimeType = "application/pdf"
	}

	name := processorName(req.ProjectID, req.Location, req.ProcessorID, req.ProcessorVersion)

	r := &documentaipb.ProcessRequest{
		Name: name,
		Source: &documentaipb.ProcessRequest_RawDocument{
			RawDocument: &documentaipb.RawDocument{
				Content:  req.Data,
				MimeType: req.MimeType,
			},
		},
	}
	if len(req.FieldMask) > 0 {
		r.FieldMask = &fieldmaskpb.FieldMask{Paths: req.FieldMask}
	}

	resp, err := s.docClient.ProcessDocument(ctx, r)
	if err != nil {
		return nil, fmt.Errorf("documentai ProcessDocument: %w", err)
	}
	if resp == nil || resp.Document == nil {
		return &DocAIResult{Provider: "gcp_documentai", Processor: name, MimeType: req.MimeType, PrimaryText: ""}, nil
	}

	return buildDocAIResult(resp.Document, name, req.MimeType), nil
}

func (s *documentService) ProcessGCSOnline(ctx context.Context, req DocAIProcessGCSRequest) (*DocAIResult, error) {
	ctx = ctxutil.Default(ctx)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	if req.MimeType == "" {
		req.MimeType = "application/pdf"
	}

	name := processorName(req.ProjectID, req.Location, req.ProcessorID, req.ProcessorVersion)

	r := &documentaipb.ProcessRequest{
		Name: name,
		Source: &documentaipb.ProcessRequest_GcsDocument{
			GcsDocument: &documentaipb.GcsDocument{
				GcsUri:   req.GCSURI,
				MimeType: req.MimeType,
			},
		},
	}
	if len(req.FieldMask) > 0 {
		r.FieldMask = &fieldmaskpb.FieldMask{Paths: req.FieldMask}
	}

	resp, err := s.docClient.ProcessDocument(ctx, r)
	if err != nil {
		return nil, fmt.Errorf("documentai ProcessDocument (gcs): %w", err)
	}
	if resp == nil || resp.Document == nil {
		return &DocAIResult{Provider: "gcp_documentai", Processor: name, MimeType: req.MimeType, PrimaryText: ""}, nil
	}

	return buildDocAIResult(resp.Document, name, req.MimeType), nil
}

func (s *documentService) BatchProcessGCS(ctx context.Context, req DocAIBatchRequest) (*DocAIBatchResult, error) {
	ctx = ctxutil.Default(ctx)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	if req.MimeType == "" {
		req.MimeType = "application/pdf"
	}

	name := processorName(req.ProjectID, req.Location, req.ProcessorID, req.ProcessorVersion)

	inBucket, inPrefix, err := parseGCSURI(req.InputGCSURI)
	if err != nil {
		return nil, err
	}
	if inPrefix != "" && !strings.HasSuffix(inPrefix, "/") {
		inPrefix += "/"
	}

	outBucket, outPrefix, err := parseGCSURI(req.OutputGCSURI)
	if err != nil {
		return nil, err
	}
	if outPrefix != "" && !strings.HasSuffix(outPrefix, "/") {
		outPrefix += "/"
	}

	br := &documentaipb.BatchProcessRequest{
		Name: name,
		InputDocuments: &documentaipb.BatchDocumentsInputConfig{
			Source: &documentaipb.BatchDocumentsInputConfig_GcsPrefix{
				GcsPrefix: &documentaipb.GcsPrefix{
					GcsUriPrefix: fmt.Sprintf("gs://%s/%s", inBucket, inPrefix),
				},
			},
		},
		DocumentOutputConfig: &documentaipb.DocumentOutputConfig{
			Destination: &documentaipb.DocumentOutputConfig_GcsOutputConfig_{
				GcsOutputConfig: &documentaipb.DocumentOutputConfig_GcsOutputConfig{
					GcsUri: fmt.Sprintf("gs://%s/%s", outBucket, outPrefix),
				},
			},
		},
	}

	op, err := s.docClient.BatchProcessDocuments(ctx, br)
	if err != nil {
		return nil, fmt.Errorf("documentai BatchProcessDocuments: %w", err)
	}

	_, err = op.Wait(ctx)
	if err != nil {
		return nil, fmt.Errorf("documentai batch wait: %w", err)
	}

	keys, err := s.listObjectsWithRetry(ctx, outBucket, outPrefix)
	if err != nil {
		return nil, err
	}
	jsonKeys := make([]string, 0, len(keys))
	for _, k := range keys {
		if strings.HasSuffix(strings.ToLower(k), ".json") {
			jsonKeys = append(jsonKeys, k)
		}
	}
	sort.Strings(jsonKeys)

	return &DocAIBatchResult{
		Provider:      "gcp_documentai",
		OperationName: op.Name(),
		OutputObjects: jsonKeys,
		Documents:     len(jsonKeys),
	}, nil
}

// ---------- parsing into segments ----------

func buildDocAIResult(doc *documentaipb.Document, processor string, mimeType string) *DocAIResult {
	out := &DocAIResult{
		Provider:  "gcp_documentai",
		Processor: processor,
		MimeType:  mimeType,
	}
	if doc == nil {
		return out
	}

	out.PrimaryText = strings.TrimSpace(doc.Text)

	segs := []types.Segment{}
	tableSegs := []types.Segment{}
	formSegs := []types.Segment{}

	for _, p := range doc.Pages {
		if p == nil {
			continue
		}
		pageNum := int(p.PageNumber)

		var pageText strings.Builder
		for _, para := range p.Paragraphs {
			if para == nil || para.Layout == nil || para.Layout.TextAnchor == nil {
				continue
			}
			t := strings.TrimSpace(textFromAnchor(doc.Text, para.Layout.TextAnchor))
			if t == "" {
				continue
			}
			pageText.WriteString(t)
			pageText.WriteString("\n")
		}

		pt := strings.TrimSpace(pageText.String())
		if pt != "" {
			pn := pageNum
			segs = append(segs, types.Segment{
				Text: pt,
				Page: &pn,
				Metadata: map[string]any{
					"kind":     "docai_page_text",
					"provider": "gcp_documentai",
				},
			})
		}

		for ti, table := range p.Tables {
			md := strings.TrimSpace(tableToMarkdown(doc.Text, table))
			if md == "" {
				continue
			}
			pn := pageNum
			tableSegs = append(tableSegs, types.Segment{
				Text: md,
				Page: &pn,
				Metadata: map[string]any{
					"kind":        "table_text",
					"provider":    "gcp_documentai",
					"table_index": ti,
				},
			})
		}

		for fi, ff := range p.FormFields {
			if ff == nil {
				continue
			}
			k := ""
			v := ""
			if ff.FieldName != nil && ff.FieldName.TextAnchor != nil {
				k = strings.TrimSpace(textFromAnchor(doc.Text, ff.FieldName.TextAnchor))
			}
			if ff.FieldValue != nil && ff.FieldValue.TextAnchor != nil {
				v = strings.TrimSpace(textFromAnchor(doc.Text, ff.FieldValue.TextAnchor))
			}
			if k == "" && v == "" {
				continue
			}
			line := strings.TrimSpace(fmt.Sprintf("%s: %s", k, v))
			pn := pageNum
			formSegs = append(formSegs, types.Segment{
				Text: line,
				Page: &pn,
				Metadata: map[string]any{
					"kind":        "form_text",
					"provider":    "gcp_documentai",
					"field_index": fi,
				},
			})
		}
	}

	out.Segments = segs
	out.Tables = tableSegs
	out.Forms = formSegs

	// Some processors may populate doc.Text but omit structured page paragraphs.
	// Ensure callers still get usable text segments.
	if len(out.Segments) == 0 && len(out.Tables) == 0 && len(out.Forms) == 0 && strings.TrimSpace(out.PrimaryText) != "" {
		out.Segments = append(out.Segments, types.Segment{
			Text: out.PrimaryText,
			Metadata: map[string]any{
				"kind":     "docai_primary_text",
				"provider": "gcp_documentai",
			},
		})
	}

	if b, err := json.Marshal(doc); err == nil {
		out.DocumentJSON = b
	}
	return out
}

func textFromAnchor(full string, anchor *documentaipb.Document_TextAnchor) string {
	if anchor == nil || len(anchor.TextSegments) == 0 || full == "" {
		return ""
	}
	var b strings.Builder
	for _, seg := range anchor.TextSegments {
		if seg == nil {
			continue
		}
		start := int(seg.StartIndex)
		end := int(seg.EndIndex)
		if start < 0 {
			start = 0
		}
		if end > len(full) {
			end = len(full)
		}
		if start >= end {
			continue
		}
		b.WriteString(full[start:end])
	}
	return b.String()
}

func tableToMarkdown(full string, t *documentaipb.Document_Page_Table) string {
	if t == nil {
		return ""
	}

	rows := [][]string{}
	header := []string{}
	if len(t.HeaderRows) > 0 && t.HeaderRows[0] != nil {
		header = tableRowToCells(full, t.HeaderRows[0])
	}
	bodyRows := append([]*documentaipb.Document_Page_Table_TableRow{}, t.BodyRows...)

	if len(header) == 0 && len(bodyRows) > 0 && bodyRows[0] != nil {
		header = tableRowToCells(full, bodyRows[0])
		bodyRows = bodyRows[1:]
	}
	if len(header) == 0 {
		return ""
	}

	rows = append(rows, header)
	for _, r := range bodyRows {
		if r == nil {
			continue
		}
		rows = append(rows, tableRowToCells(full, r))
	}
	if len(rows) == 0 {
		return ""
	}

	maxCols := 0
	for _, r := range rows {
		if len(r) > maxCols {
			maxCols = len(r)
		}
	}
	if maxCols == 0 {
		return ""
	}
	for i := range rows {
		for len(rows[i]) < maxCols {
			rows[i] = append(rows[i], "")
		}
	}

	var out strings.Builder
	out.WriteString("| ")
	out.WriteString(strings.Join(escapePipes(rows[0]), " | "))
	out.WriteString(" |\n| ")
	sep := make([]string, maxCols)
	for i := 0; i < maxCols; i++ {
		sep[i] = "---"
	}
	out.WriteString(strings.Join(sep, " | "))
	out.WriteString(" |\n")

	for i := 1; i < len(rows); i++ {
		out.WriteString("| ")
		out.WriteString(strings.Join(escapePipes(rows[i]), " | "))
		out.WriteString(" |\n")
	}
	return out.String()
}

func tableRowToCells(full string, r *documentaipb.Document_Page_Table_TableRow) []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.Cells))
	for _, c := range r.Cells {
		if c == nil || c.Layout == nil || c.Layout.TextAnchor == nil {
			out = append(out, "")
			continue
		}
		out = append(out, strings.TrimSpace(textFromAnchor(full, c.Layout.TextAnchor)))
	}
	return out
}

func escapePipes(row []string) []string {
	out := make([]string, len(row))
	for i, s := range row {
		out[i] = strings.ReplaceAll(s, "|", "\\|")
	}
	return out
}

func processorName(project, location, processorID, version string) string {
	project = strings.TrimSpace(project)
	location = strings.TrimSpace(location)
	processorID = strings.TrimSpace(processorID)
	version = strings.TrimSpace(version)

	if project == "" || location == "" || processorID == "" {
		return ""
	}
	base := fmt.Sprintf("projects/%s/locations/%s/processors/%s", project, location, processorID)
	if version != "" {
		return base + "/processorVersions/" + version
	}
	return base
}

func (s *documentService) listObjectsWithRetry(ctx context.Context, bucket, prefix string) ([]string, error) {
	var lastErr error
	for attempt := 0; attempt < s.listRetry; attempt++ {
		keys, err := s.listObjects(ctx, bucket, prefix)
		if err == nil {
			if len(keys) > 0 {
				return keys, nil
			}
			lastErr = fmt.Errorf("no objects found yet under %s/%s", bucket, prefix)
		} else {
			lastErr = err
		}
		time.Sleep(s.listRetryDelay)
	}
	return nil, lastErr
}

func (s *documentService) listObjects(ctx context.Context, bucket, prefix string) ([]string, error) {
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

func (s *documentService) readObject(ctx context.Context, bucket, key string) ([]byte, error) {
	rc, err := s.storage.Bucket(bucket).Object(key).NewReader(ctx)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

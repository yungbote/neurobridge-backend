package extractor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// -------------------- small shared helpers --------------------

func defaultCtx(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		// Do NOT panic inside pipelines.
		return []byte(`{}`)
	}
	return b
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

func collapseWhitespace(s string) string {
	s = strings.ReplaceAll(s, "\u00a0", " ")
	return strings.Join(strings.Fields(s), " ")
}

func sanitizeUTF8(s string) string {
	if s == "" {
		return s
	}
	if utf8.ValidString(s) {
		return s
	}
	// Replace invalid byte sequences with a space (keeps words separated)
	return strings.ToValidUTF8(s, " ")
}

// -------------------- Segment utilities --------------------

func NormalizeSegments(in []Segment) []Segment {
	out := make([]Segment, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		t := strings.TrimSpace(s.Text)
		if t == "" {
			continue
		}
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

func JoinSegmentsText(segs []Segment) string {
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

func TagSegments(in []Segment, extra map[string]any) []Segment {
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

func TextSignalWeak(segs []Segment) bool {
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

// -------------------- Kind detection --------------------

func ClassifyKind(name, mime string, smallBytes []byte, path string) string {
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

// -------------------- Strict native fallback --------------------

func ExtractTextStrict(name, mime string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("no data")
	}

	m := strings.ToLower(strings.TrimSpace(mime))
	ext := strings.ToLower(filepath.Ext(name))

	if strings.HasPrefix(m, "text/") || m == "application/json" || m == "application/xml" ||
		ext == ".txt" || ext == ".md" || ext == ".csv" || ext == ".log" || ext == ".json" ||
		ext == ".yaml" || ext == ".yml" || ext == ".xml" || ext == ".html" || ext == ".htm" {

		s := string(data)
		if m == "text/html" || ext == ".html" || ext == ".htm" {
			re := regexp.MustCompile(`(?s)<[^>]*>`)
			s = re.ReplaceAllString(s, " ")
		}
		return s, nil
	}

	// heuristic: if it "looks like text", return it rather than erroring
	printable := 0
	total := 0
	for _, r := range string(data) {
		total++
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
			printable++
			continue
		}
		if r >= 32 && r != 127 {
			printable++
		}
	}
	if total > 0 && float64(printable)/float64(total) > 0.90 {
		return string(data), nil
	}

	return "", fmt.Errorf("strict extraction unsupported for mime=%q ext=%q", mime, ext)
}

// -------------------- preserved (even if currently unused) --------------------

type limitedBufferWriter struct {
	max int64
	n   int64
	buf *bytes.Buffer
}

func (w *limitedBufferWriter) Write(p []byte) (int, error) {
	if w.buf == nil || w.max <= 0 {
		return len(p), nil
	}
	remain := w.max - w.n
	if remain > 0 {
		if int64(len(p)) <= remain {
			_, _ = w.buf.Write(p)
			w.n += int64(len(p))
		} else {
			_, _ = w.buf.Write(p[:remain])
			w.n += remain
		}
	}
	// IMPORTANT: claim we wrote everything so writers don't fail
	return len(p), nil
}











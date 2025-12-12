package services

import (
  "archive/zip"
  "bytes"
  "encoding/xml"
  "fmt"
  "io"
  "path/filepath"
  "regexp"
  "strings"

  pdf "github.com/ledongthuc/pdf"
)

// ExtractText tries hard to determine true file type from bytes (sniffing),
// then extracts text accordingly.
// Supported: PDF, DOCX, PPTX, TXT/MD, HTML (strip tags).
func ExtractText(originalName string, mimeType string, data []byte) (string, error) {
  ext := strings.ToLower(filepath.Ext(originalName))
  mt := strings.ToLower(strings.TrimSpace(mimeType))

  if len(data) == 0 {
    return "", fmt.Errorf("empty file: name=%s mime=%s", originalName, mimeType)
  }

  // 1) Sniff by magic bytes first (most reliable)
  if isPDF(data) {
    return extractPDF(data)
  }
  if isZip(data) {
    // Could be docx/pptx/xlsx/other zip. Detect by entries.
    kind, err := detectOpenXMLKind(data)
    if err != nil {
      return "", fmt.Errorf("zip/openxml detect failed: %w", err)
    }
    switch kind {
    case "docx":
      return extractDOCX(data)
    case "pptx":
      return extractPPTX(data)
    default:
      return "", fmt.Errorf("unsupported zip/openxml kind=%s name=%s mime=%s", kind, originalName, mimeType)
    }
  }

  // 2) Sniff as HTML
  if looksLikeHTML(data) || mt == "text/html" || ext == ".html" || ext == ".htm" {
    return extractHTML(string(data)), nil
  }

  // 3) Sniff as plaintext (very common for .md/.txt)
  if isProbablyText(data) || mt == "text/plain" || ext == ".txt" || ext == ".md" || ext == ".markdown" {
    return collapseWhitespace(string(data)), nil
  }

  // 4) If mime/ext claim something, attempt in a safe order (no blind PDF parse)
  // If it claims pdf but isn't actually pdf, return a helpful error.
  if mt == "application/pdf" || ext == ".pdf" {
    head := firstBytesHex(data, 16)
    return "", fmt.Errorf("file claims pdf but missing %%PDF header. name=%s mime=%s head=%s", originalName, mimeType, head)
  }

  // DOCX / PPTX by mime/ext as fallback (in case magic sniff missed zip, unlikely)
  if mt == "application/vnd.openxmlformats-officedocument.wordprocessingml.document" || ext == ".docx" {
    // docx is a zip; if we got here, it's not zip => corrupted
    return "", fmt.Errorf("file claims docx but is not a valid zip container: name=%s mime=%s", originalName, mimeType)
  }
  if mt == "application/vnd.openxmlformats-officedocument.presentationml.presentation" || ext == ".pptx" {
    return "", fmt.Errorf("file claims pptx but is not a valid zip container: name=%s mime=%s", originalName, mimeType)
  }

  // 5) Unknown binary
  return "", fmt.Errorf("unsupported file type: name=%s ext=%s mime=%s head=%s", originalName, ext, mimeType, firstBytesHex(data, 16))
}

// ------------------------
// Sniff helpers
// ------------------------

func isPDF(b []byte) bool {
  // PDF starts with "%PDF-"
  return len(b) >= 5 && string(b[:5]) == "%PDF-"
}

func isZip(b []byte) bool {
  // ZIP local file header: PK\x03\x04
  return len(b) >= 4 && b[0] == 'P' && b[1] == 'K' && b[2] == 3 && b[3] == 4
}

func looksLikeHTML(b []byte) bool {
  // cheap heuristic: starts with "<" or contains "<html" in early bytes
  s := strings.ToLower(string(b[:min(len(b), 2048)]))
  if strings.HasPrefix(strings.TrimSpace(s), "<!doctype") {
    return true
  }
  if strings.HasPrefix(strings.TrimSpace(s), "<html") {
    return true
  }
  // also catch saved error pages
  if strings.Contains(s, "<html") && strings.Contains(s, "</html>") {
    return true
  }
  return false
}

func isProbablyText(b []byte) bool {
  // Heuristic: if most bytes are printable / whitespace and no NULs.
  sample := b[:min(len(b), 4096)]
  nul := 0
  good := 0
  for _, c := range sample {
    if c == 0x00 {
      nul++
      continue
    }
    if c == '\n' || c == '\r' || c == '\t' || (c >= 0x20 && c <= 0x7E) || c >= 0x80 {
      good++
    }
  }
  if nul > 0 {
    return false
  }
  // allow some binary noise
  return float64(good)/float64(len(sample)) > 0.9
}

func firstBytesHex(b []byte, n int) string {
  n = min(len(b), n)
  // minimal hex without importing encoding/hex
  const hexdigits = "0123456789abcdef"
  out := make([]byte, 0, n*2)
  for i := 0; i < n; i++ {
    out = append(out, hexdigits[b[i]>>4], hexdigits[b[i]&0x0f])
  }
  return string(out)
}

func min(a, b int) int {
  if a < b {
    return a
  }
  return b
}

// ------------------------
// Extractors
// ------------------------

func extractPDF(data []byte) (string, error) {
  r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
  if err != nil {
    return "", fmt.Errorf("pdf reader: %w", err)
  }
  plain, err := r.GetPlainText()
  if err != nil {
    return "", fmt.Errorf("pdf plaintext: %w", err)
  }
  b, err := io.ReadAll(plain)
  if err != nil {
    return "", fmt.Errorf("pdf read: %w", err)
  }
  return collapseWhitespace(string(b)), nil
}

func detectOpenXMLKind(zipBytes []byte) (string, error) {
  zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
  if err != nil {
    return "", err
  }
  // detect by presence of key parts
  hasWord := false
  hasPpt := false
  for _, f := range zr.File {
    name := f.Name
    if strings.HasPrefix(name, "word/") {
      hasWord = true
    }
    if strings.HasPrefix(name, "ppt/") {
      hasPpt = true
    }
  }
  switch {
  case hasWord && !hasPpt:
    return "docx", nil
  case hasPpt && !hasWord:
    return "pptx", nil
  case hasWord && hasPpt:
    return "unknown", fmt.Errorf("zip contains both word/ and ppt/ parts")
  default:
    return "unknown", fmt.Errorf("zip does not look like docx or pptx")
  }
}

func extractDOCX(zipBytes []byte) (string, error) {
  // DOCX: extract from word/document.xml, gather <w:t>
  return extractOpenXMLText(zipBytes, []string{"word/document.xml"}, []xmlTag{{Local: "t"}})
}

func extractPPTX(zipBytes []byte) (string, error) {
  // PPTX: scan ppt/slides/*.xml, gather <a:t>
  return extractOpenXMLTextByPrefix(zipBytes, "ppt/slides/", ".xml", []xmlTag{{Local: "t"}})
}

type xmlTag struct{ Local string }

func extractOpenXMLText(zipBytes []byte, files []string, tags []xmlTag) (string, error) {
  zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
  if err != nil {
    return "", err
  }
  var out strings.Builder
  for _, target := range files {
    f := findZipFile(zr, target)
    if f == nil {
      continue
    }
    rc, err := f.Open()
    if err != nil {
      return "", err
    }
    b, _ := io.ReadAll(rc)
    _ = rc.Close()
    out.WriteString(extractTextFromXML(b, tags))
    out.WriteString("\n")
  }
  s := collapseWhitespace(out.String())
  if s == "" {
    return "", fmt.Errorf("no text extracted from openxml")
  }
  return s, nil
}

func extractOpenXMLTextByPrefix(zipBytes []byte, prefix string, suffix string, tags []xmlTag) (string, error) {
  zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
  if err != nil {
    return "", err
  }
  var out strings.Builder
  for _, f := range zr.File {
    if strings.HasPrefix(f.Name, prefix) && strings.HasSuffix(f.Name, suffix) {
      rc, err := f.Open()
      if err != nil {
        return "", err
      }
      b, _ := io.ReadAll(rc)
      _ = rc.Close()
      out.WriteString(extractTextFromXML(b, tags))
      out.WriteString("\n")
    }
  }
  s := collapseWhitespace(out.String())
  if s == "" {
    return "", fmt.Errorf("no text extracted from openxml prefix %s", prefix)
  }
  return s, nil
}

func findZipFile(zr *zip.Reader, name string) *zip.File {
  for _, f := range zr.File {
    if f.Name == name {
      return f
    }
  }
  return nil
}

func extractTextFromXML(xmlBytes []byte, tags []xmlTag) string {
  dec := xml.NewDecoder(bytes.NewReader(xmlBytes))
  var out strings.Builder
  for {
    tok, err := dec.Token()
    if err != nil {
      break
    }
    se, ok := tok.(xml.StartElement)
    if !ok {
      continue
    }
    local := se.Name.Local
    want := false
    for _, t := range tags {
      if t.Local == local {
        want = true
        break
      }
    }
    if !want {
      continue
    }
    var v string
    _ = dec.DecodeElement(&v, &se)
    if v != "" {
      out.WriteString(v)
      out.WriteString(" ")
    }
  }
  return out.String()
}

func extractHTML(s string) string {
  re := regexp.MustCompile(`(?s)<[^>]*>`)
  s = re.ReplaceAllString(s, " ")
  s = strings.ReplaceAll(s, "&nbsp;", " ")
  s = strings.ReplaceAll(s, "&amp;", "&")
  return collapseWhitespace(s)
}

func collapseWhitespace(s string) string {
  s = strings.ReplaceAll(s, "\u00a0", " ")
  fields := strings.Fields(s)
  return strings.Join(fields, " ")
}











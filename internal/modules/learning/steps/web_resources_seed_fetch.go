package steps

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func newWebHTTPClient() *http.Client {
	timeout := 25 * time.Second
	if v := strings.TrimSpace(os.Getenv("WEB_RESOURCES_HTTP_TIMEOUT_SECONDS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = time.Duration(n) * time.Second
		}
	}
	c := &http.Client{Timeout: timeout}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 6 {
			return fmt.Errorf("too many redirects")
		}
		if req == nil || req.URL == nil {
			return fmt.Errorf("redirect missing url")
		}
		if !isAllowedWebURL(context.Background(), req.URL.String()) {
			return fmt.Errorf("redirect blocked: %s", req.URL.String())
		}
		return nil
	}
	return c
}

func fetchURL(ctx context.Context, client *http.Client, rawURL string, maxBytes int64) ([]byte, string, string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	u := strings.TrimSpace(rawURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", "", err
	}
	req.Header.Set("User-Agent", "NeurobridgeBot/1.0 (learning path builder)")
	req.Header.Set("Accept", "text/html, text/plain, application/pdf;q=0.9, */*;q=0.1")

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", "", fmt.Errorf("http %d", resp.StatusCode)
	}

	ctype := strings.TrimSpace(resp.Header.Get("Content-Type"))
	mediaType := ""
	if ctype != "" {
		if mt, _, err := mime.ParseMediaType(ctype); err == nil {
			mediaType = mt
		}
	}

	limited := io.LimitReader(resp.Body, maxBytes+1)
	b, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", "", err
	}
	if int64(len(b)) > maxBytes {
		return nil, "", "", fmt.Errorf("response too large (%d > %d)", len(b), maxBytes)
	}
	if mediaType == "" && len(b) > 0 {
		mediaType = http.DetectContentType(b[:min(512, len(b))])
	}
	finalURL := u
	if resp.Request != nil && resp.Request.URL != nil && strings.TrimSpace(resp.Request.URL.String()) != "" {
		finalURL = strings.TrimSpace(resp.Request.URL.String())
	}
	return b, mediaType, finalURL, nil
}

func normalizeFetchedNameAndMime(r webResourceItemV1, finalURL string, contentType string) (string, string) {
	u := strings.TrimSpace(finalURL)
	title := strings.TrimSpace(r.Title)
	ct := strings.ToLower(strings.TrimSpace(contentType))

	ext := ""
	switch {
	case strings.Contains(ct, "application/pdf") || strings.HasSuffix(strings.ToLower(u), ".pdf"):
		ext = ".pdf"
		ct = "application/pdf"
	case strings.Contains(ct, "text/html"):
		ext = ".html"
		ct = "text/html"
	case strings.Contains(ct, "text/plain"):
		ext = ".txt"
		ct = "text/plain"
	default:
		// default to html-ish extraction
		ext = ".html"
		if ct == "" {
			ct = "text/html"
		}
	}

	slug := slugify(title)
	if slug == "" {
		slug = "resource"
	}
	host := safeHostForName(u)
	name := fmt.Sprintf("web_%s_%s%s", host, slug, ext)
	// Avoid pathological lengths / weird extensions.
	name = strings.TrimSpace(name)
	if len(name) > 120 {
		name = truncateUTF8(name, 120)
	}
	if filepath.Ext(name) == "" {
		name += ext
	}
	return name, ct
}

func safeHostForName(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil {
		return "site"
	}
	h := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if h == "" {
		return "site"
	}
	h = strings.TrimPrefix(h, "www.")
	h = strings.ReplaceAll(h, ".", "_")
	if len(h) > 40 {
		h = truncateUTF8(h, 40)
	}
	return h
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == ' ' || r == '-' || r == '_' || r == '/':
			return '_'
		default:
			return -1
		}
	}, s)
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}
	s = strings.Trim(s, "_")
	if len(s) > 48 {
		s = truncateUTF8(s, 48)
	}
	return s
}

func isAllowedWebURL(ctx context.Context, raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil {
		return false
	}
	if strings.ToLower(u.Scheme) != "https" {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" {
		return false
	}
	if host == "localhost" || strings.HasSuffix(host, ".local") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !isPrivateIP(ip)
	}

	// Best-effort: resolve and block private IPs (SSRF hardening).
	resCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(resCtx, "ip", host)
	if err != nil || len(ips) == 0 {
		// If we can't resolve, treat as blocked (safer default).
		return false
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return false
		}
	}
	return true
}

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	ip = ip.To4()
	if ip == nil {
		// IPv6: conservatively treat as private unless explicitly global unicast.
		return true
	}
	if ip.IsLoopback() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() {
		return true
	}
	// 10.0.0.0/8
	if ip[0] == 10 {
		return true
	}
	// 172.16.0.0/12
	if ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31 {
		return true
	}
	// 192.168.0.0/16
	if ip[0] == 192 && ip[1] == 168 {
		return true
	}
	// 127.0.0.0/8
	if ip[0] == 127 {
		return true
	}
	// 169.254.0.0/16 (link local)
	if ip[0] == 169 && ip[1] == 254 {
		return true
	}
	return false
}

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	v = strings.ToLower(v)
	if v == "1" || v == "true" || v == "yes" || v == "y" || v == "on" {
		return true
	}
	if v == "0" || v == "false" || v == "no" || v == "n" || v == "off" {
		return false
	}
	return def
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Ensure deterministic ordering for any later uses (debuggability only).
func (p *webResourcePlanV1) sort() {
	if p == nil || len(p.Resources) == 0 {
		return
	}
	sort.Slice(p.Resources, func(i, j int) bool {
		return strings.ToLower(strings.TrimSpace(p.Resources[i].URL)) < strings.ToLower(strings.TrimSpace(p.Resources[j].URL))
	})
}

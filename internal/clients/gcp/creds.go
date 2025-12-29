package gcp

import (
	"google.golang.org/api/option"
	"os"
	"strings"
)

func ClientOptionsFromEnv() []option.ClientOption {
	creds := strings.TrimSpace(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS_JSON"))
	if creds == "" {
		creds = strings.TrimSpace(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"))
	}
	opts := []option.ClientOption{}
	if creds == "" {
		return opts
	}
	if strings.HasPrefix(creds, "{") {
		opts = append(opts, option.WithCredentialsJSON([]byte(creds)))
	} else {
		opts = append(opts, option.WithCredentialsFile(creds))
	}
	return opts
}

// ---------- shared helpers (package-wide) ----------
func ptrFloat(v float64) *float64 { return &v }
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func collapseWhitespace(s string) string {
	// cheap, fast: Fields collapses all whitespace sequences to single spaces
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\u00a0", " ")), " ")
}

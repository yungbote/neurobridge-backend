package gcp

import (
	"os"
	"strings"

	"google.golang.org/api/option"
)

func ClientOptionsFromEnv() []option.ClientOption {
	creds := strings.TrimSpace(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS_JSON"))
	if creds == "" {
		creds = strings.TrimSpace(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"))
	}
	if creds == "" {
		return nil
	}
	if strings.HasPrefix(creds, "{") {
		return []option.ClientOption{option.WithCredentialsJSON([]byte(creds))}
	}
	return []option.ClientOption{option.WithCredentialsFile(creds)}
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

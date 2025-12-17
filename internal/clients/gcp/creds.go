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











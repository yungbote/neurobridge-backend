package temporalx

import (
	"os"
	"strings"
)

type Config struct {
	Address   string
	Namespace string
	TaskQueue string

	ClientCertPath string
	ClientKeyPath  string
	ClientCAPath   string
}

func LoadConfig() Config {
	return Config{
		Address:   strings.TrimSpace(os.Getenv("TEMPORAL_ADDRESS")),
		Namespace: stringsOr(strings.TrimSpace(os.Getenv("TEMPORAL_NAMESPACE")), "neurobridge"),
		TaskQueue: stringsOr(strings.TrimSpace(os.Getenv("TEMPORAL_TASK_QUEUE")), "neurobridge"),

		ClientCertPath: strings.TrimSpace(os.Getenv("TEMPORAL_CLIENT_CERT_PATH")),
		ClientKeyPath:  strings.TrimSpace(os.Getenv("TEMPORAL_CLIENT_KEY_PATH")),
		ClientCAPath:   strings.TrimSpace(os.Getenv("TEMPORAL_CLIENT_CA_PATH")),
	}
}

func stringsOr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

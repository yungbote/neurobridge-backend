package httputil

import (
	"encoding/json"
	"net/http"
	"strings"
)

func WriteError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = http.StatusText(status)
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": msg,
		},
	})
}

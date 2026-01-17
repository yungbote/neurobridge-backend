package httputil

import (
	"fmt"
	"net/http"
	"strings"
)

func WriteSSE(w http.ResponseWriter, event string, data string) error {
	if strings.TrimSpace(event) != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", strings.TrimSpace(event)); err != nil {
			return err
		}
	}
	for _, line := range strings.Split(data, "\n") {
		if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil {
			return err
		}
	}
	_, err := fmt.Fprint(w, "\n")
	return err
}

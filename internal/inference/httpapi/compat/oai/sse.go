package oai

import (
	"encoding/json"
	"net/http"

	"github.com/yungbote/neurobridge-backend/internal/inference/engine"
	"github.com/yungbote/neurobridge-backend/internal/inference/httpapi/httputil"
)

func streamResponses(w http.ResponseWriter, r *http.Request, eng engine.Engine, model string, messages []engine.Message, opts engine.GenerateOptions) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, http.StatusInternalServerError, "streaming unsupported", "server_error", "", "")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	ctx := r.Context()
	_, err := eng.StreamText(ctx, model, messages, opts, func(delta string) {
		payload, _ := json.Marshal(map[string]any{
			"type":  "response.output_text.delta",
			"delta": delta,
		})
		_ = httputil.WriteSSE(w, "response.output_text.delta", string(payload))
		flusher.Flush()
	})
	if err != nil {
		payload, _ := json.Marshal(map[string]any{
			"type":  "response.error",
			"error": map[string]any{"message": err.Error(), "type": "engine_error"},
		})
		_ = httputil.WriteSSE(w, "response.error", string(payload))
		flusher.Flush()
		return
	}

	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()
}

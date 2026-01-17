package v1

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/yungbote/neurobridge-backend/internal/inference/config"
	"github.com/yungbote/neurobridge-backend/internal/inference/engine"
	"github.com/yungbote/neurobridge-backend/internal/inference/httpapi/httputil"
	"github.com/yungbote/neurobridge-backend/internal/inference/router"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

func handleTextGenerate(cfg *config.Config, _ *logger.Logger, r *router.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()

		var in TextGenerateRequest
		if err := httputil.DecodeJSON(w, req, cfg.HTTP.MaxRequestBytes, &in); err != nil {
			WriteError(w, http.StatusBadRequest, err.Error(), "invalid_request", "")
			return
		}

		model := strings.TrimSpace(in.Model)
		if model == "" {
			WriteError(w, http.StatusBadRequest, "model is required", "invalid_request", "model")
			return
		}

		route, ok := r.RouteForModel(model)
		if !ok {
			WriteError(w, http.StatusNotFound, "model not found", "model_not_found", "model")
			return
		}

		messages, err := normalizeMessages(in.Messages)
		if err != nil {
			WriteError(w, http.StatusBadRequest, err.Error(), "invalid_request", "messages")
			return
		}

		opts := engine.GenerateOptions{Temperature: in.Temperature}
		if in.JSONSchema != nil {
			opts.JSONSchema = &engine.JSONSchema{
				Name:   strings.TrimSpace(in.JSONSchema.Name),
				Schema: in.JSONSchema.Schema,
				Strict: in.JSONSchema.Strict,
			}
		}

		if in.Stream {
			streamText(w, req, route.Engine, route.UpstreamModel, messages, opts)
			return
		}

		text, err := route.Engine.GenerateText(ctx, route.UpstreamModel, messages, opts)
		if err != nil {
			WriteError(w, http.StatusBadGateway, err.Error(), "engine_error", "")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TextGenerateResponse{
			Model:      route.PublicModel,
			OutputText: text,
		})
	}
}

func normalizeMessages(msgs []Message) ([]engine.Message, error) {
	if msgs == nil || len(msgs) == 0 {
		return nil, errors.New("messages is required")
	}
	out := make([]engine.Message, 0, len(msgs))
	for _, m := range msgs {
		role := strings.TrimSpace(m.Role)
		if role == "" {
			return nil, errors.New("message role is required")
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		out = append(out, engine.Message{Role: role, Content: content})
	}
	if len(out) == 0 {
		return nil, errors.New("messages must include at least one non-empty content")
	}
	return out, nil
}

func streamText(w http.ResponseWriter, r *http.Request, eng engine.Engine, model string, messages []engine.Message, opts engine.GenerateOptions) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, http.StatusInternalServerError, "streaming unsupported", "server_error", "")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	ctx := r.Context()
	_, err := eng.StreamText(ctx, model, messages, opts, func(delta string) {
		payload, _ := json.Marshal(map[string]any{"delta": delta})
		_ = httputil.WriteSSE(w, "text.delta", string(payload))
		flusher.Flush()
	})
	if err != nil {
		payload, _ := json.Marshal(map[string]any{"message": err.Error()})
		_ = httputil.WriteSSE(w, "error", string(payload))
		flusher.Flush()
		return
	}

	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()
}

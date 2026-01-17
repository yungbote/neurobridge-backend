package oai

import (
	"bytes"
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

func handleResponses(cfg *config.Config, _ *logger.Logger, r *router.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()

		var in responsesRequest
		if err := httputil.DecodeJSON(w, req, cfg.HTTP.MaxRequestBytes, &in); err != nil {
			WriteError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "", "")
			return
		}

		model := strings.TrimSpace(in.Model)
		if model == "" {
			WriteError(w, http.StatusBadRequest, "model is required", "invalid_request_error", "model", "")
			return
		}

		route, ok := r.RouteForModel(model)
		if !ok {
			WriteError(w, http.StatusNotFound, "model not found", "invalid_request_error", "model", "model_not_found")
			return
		}

		messages, err := normalizeMessages(in.Instructions, in.Input)
		if err != nil {
			WriteError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "input", "")
			return
		}

		opts := engine.GenerateOptions{Temperature: in.Temperature}
		if js := parseJSONSchema(in.Text.Format); js != nil {
			opts.JSONSchema = js
		}

		if in.Stream {
			streamResponses(w, req, route.Engine, route.UpstreamModel, messages, opts)
			return
		}

		text, err := route.Engine.GenerateText(ctx, route.UpstreamModel, messages, opts)
		if err != nil {
			WriteError(w, http.StatusBadGateway, err.Error(), "engine_error", "", "")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(buildResponsesResponse(text))
	}
}

func normalizeMessages(instructions string, input []struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}) ([]engine.Message, error) {
	var out []engine.Message

	if strings.TrimSpace(instructions) != "" {
		out = append(out, engine.Message{Role: "system", Content: strings.TrimSpace(instructions)})
	}

	for _, item := range input {
		role := strings.TrimSpace(item.Role)
		if role == "" {
			return nil, errors.New("input role is required")
		}
		text := extractText(item.Content)
		if strings.TrimSpace(text) == "" {
			continue
		}
		out = append(out, engine.Message{Role: role, Content: text})
	}

	if len(out) == 0 {
		return nil, errors.New("input is required")
	}
	return out, nil
}

func extractText(content any) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		var buf bytes.Buffer
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			t, _ := m["type"].(string)
			if strings.EqualFold(strings.TrimSpace(t), "input_text") {
				if s, ok := m["text"].(string); ok && strings.TrimSpace(s) != "" {
					if buf.Len() > 0 {
						buf.WriteString("\n")
					}
					buf.WriteString(s)
				}
			}
		}
		return buf.String()
	case map[string]any:
		if s, ok := v["text"].(string); ok {
			return s
		}
		return ""
	default:
		return ""
	}
}

func parseJSONSchema(format map[string]any) *engine.JSONSchema {
	if format == nil {
		return nil
	}
	t, _ := format["type"].(string)
	if strings.TrimSpace(t) != "json_schema" {
		return nil
	}

	name, _ := format["name"].(string)
	schema, _ := format["schema"].(map[string]any)
	strict, _ := format["strict"].(bool)
	return &engine.JSONSchema{
		Name:   strings.TrimSpace(name),
		Schema: schema,
		Strict: strict,
	}
}

func buildResponsesResponse(text string) responsesResponse {
	return responsesResponse{
		Output: []struct {
			Type    string `json:"type"`
			Role    string `json:"role,omitempty"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text,omitempty"`
			} `json:"content,omitempty"`
		}{
			{
				Type: "message",
				Role: "assistant",
				Content: []struct {
					Type string `json:"type"`
					Text string `json:"text,omitempty"`
				}{
					{Type: "output_text", Text: text},
				},
			},
		},
	}
}

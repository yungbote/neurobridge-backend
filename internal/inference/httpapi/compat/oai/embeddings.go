package oai

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/yungbote/neurobridge-backend/internal/inference/config"
	"github.com/yungbote/neurobridge-backend/internal/inference/httpapi/httputil"
	"github.com/yungbote/neurobridge-backend/internal/inference/router"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

func handleEmbeddings(cfg *config.Config, _ *logger.Logger, r *router.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()

		var in embeddingsRequest
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

		inputs, err := normalizeEmbeddingsInput(in.Input)
		if err != nil {
			WriteError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "input", "")
			return
		}

		vecs, err := route.Engine.Embed(ctx, route.UpstreamModel, inputs)
		if err != nil {
			WriteError(w, http.StatusBadGateway, err.Error(), "engine_error", "", "")
			return
		}

		data := make([]embeddingsItem, 0, len(vecs))
		for i, v := range vecs {
			data = append(data, embeddingsItem{
				Object:    "embedding",
				Embedding: v,
				Index:     i,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(embeddingsResponse{
			Object: "list",
			Data:   data,
			Model:  route.PublicModel,
			Usage: embeddingsUsage{
				PromptTokens: 0,
				TotalTokens:  0,
			},
		})
	}
}

func normalizeEmbeddingsInput(v any) ([]string, error) {
	switch t := v.(type) {
	case nil:
		return nil, errors.New("input is required")
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			s = " "
		}
		return []string{s}, nil
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			s, ok := item.(string)
			if !ok {
				return nil, errors.New("input must be a string or an array of strings")
			}
			s = strings.TrimSpace(s)
			if s == "" {
				s = " "
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, errors.New("input must be a string or an array of strings")
	}
}

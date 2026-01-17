package v1

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

		var in EmbeddingsRequest
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

		inputs, err := normalizeInputs(in.Inputs)
		if err != nil {
			WriteError(w, http.StatusBadRequest, err.Error(), "invalid_request", "inputs")
			return
		}

		vecs, err := route.Engine.Embed(ctx, route.UpstreamModel, inputs)
		if err != nil {
			WriteError(w, http.StatusBadGateway, err.Error(), "engine_error", "")
			return
		}

		data := make([]EmbeddingsItem, 0, len(vecs))
		for i, v := range vecs {
			data = append(data, EmbeddingsItem{Index: i, Embedding: v})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(EmbeddingsResponse{
			Model: route.PublicModel,
			Data:  data,
		})
	}
}

func normalizeInputs(inputs []string) ([]string, error) {
	if inputs == nil {
		return nil, errors.New("inputs is required")
	}
	if len(inputs) == 0 {
		return nil, errors.New("inputs must be non-empty")
	}
	out := make([]string, len(inputs))
	for i := range inputs {
		s := strings.TrimSpace(inputs[i])
		if s == "" {
			s = " "
		}
		out[i] = s
	}
	return out, nil
}

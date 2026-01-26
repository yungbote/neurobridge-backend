package v1

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/yungbote/neurobridge-backend/internal/inference/config"
	"github.com/yungbote/neurobridge-backend/internal/inference/engine"
	"github.com/yungbote/neurobridge-backend/internal/inference/httpapi/httputil"
	"github.com/yungbote/neurobridge-backend/internal/inference/router"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

func handleTextScore(cfg *config.Config, _ *logger.Logger, r *router.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()

		var in TextScoreRequest
		if err := httputil.DecodeJSON(w, req, cfg.HTTP.MaxRequestBytes, &in); err != nil {
			WriteError(w, http.StatusBadRequest, err.Error(), "invalid_request", "")
			return
		}

		model := strings.TrimSpace(in.Model)
		if model == "" {
			WriteError(w, http.StatusBadRequest, "model is required", "invalid_request", "model")
			return
		}
		if len(in.Pairs) == 0 {
			WriteError(w, http.StatusBadRequest, "pairs is required", "invalid_request", "pairs")
			return
		}

		route, ok := r.RouteForModel(model)
		if !ok {
			WriteError(w, http.StatusNotFound, "model not found", "model_not_found", "model")
			return
		}

		pairs := make([]engine.TextPair, 0, len(in.Pairs))
		for _, p := range in.Pairs {
			a := strings.TrimSpace(p.A)
			b := strings.TrimSpace(p.B)
			if a == "" && b == "" {
				continue
			}
			pairs = append(pairs, engine.TextPair{A: a, B: b})
		}
		if len(pairs) == 0 {
			WriteError(w, http.StatusBadRequest, "pairs must include non-empty entries", "invalid_request", "pairs")
			return
		}

		scores, err := route.Engine.ScoreTextPairs(ctx, route.UpstreamModel, pairs)
		if err != nil {
			WriteError(w, http.StatusBadGateway, err.Error(), "engine_error", "")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TextScoreResponse{
			Model:  route.PublicModel,
			Scores: scores,
		})
	}
}

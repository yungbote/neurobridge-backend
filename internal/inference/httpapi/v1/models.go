package v1

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/yungbote/neurobridge-backend/internal/inference/router"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

func handleModels(_ *logger.Logger, r *router.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		ids := r.ListModels()
		sort.Strings(ids)

		models := make([]Model, 0, len(ids))
		for _, id := range ids {
			models = append(models, Model{ID: id})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ModelsResponse{Models: models})
	}
}

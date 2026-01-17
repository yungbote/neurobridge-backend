package oai

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/yungbote/neurobridge-backend/internal/inference/router"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

func handleModels(_ *logger.Logger, r *router.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		models := r.ListModels()
		sort.Strings(models)

		data := make([]modelEntry, 0, len(models))
		for _, id := range models {
			data = append(data, modelEntry{ID: id, Object: "model", OwnedBy: "neurobridge"})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(modelsResponse{
			Object: "list",
			Data:   data,
		})
	}
}

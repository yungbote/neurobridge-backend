package v1

import (
	"net/http"

	"github.com/yungbote/neurobridge-backend/internal/inference/config"
	"github.com/yungbote/neurobridge-backend/internal/inference/router"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

func Register(mux *http.ServeMux, cfg *config.Config, log *logger.Logger, r *router.Router) {
	mux.HandleFunc("GET /v1/models", handleModels(log, r))
	mux.HandleFunc("POST /v1/embeddings", handleEmbeddings(cfg, log, r))
	mux.HandleFunc("POST /v1/text/generate", handleTextGenerate(cfg, log, r))

	// Planned surfaces (kept stable early; may return 501 until engines are configured).
	mux.HandleFunc("POST /v1/images/generate", handleImagesGenerate(cfg, log, r))
	mux.HandleFunc("POST /v1/videos/generate", handleVideosGenerate(cfg, log, r))
	mux.HandleFunc("GET /v1/videos/{id}", handleVideosGet(cfg, log, r))
	mux.HandleFunc("GET /v1/videos/{id}/content", handleVideosContent(cfg, log, r))
}

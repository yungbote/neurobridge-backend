package oai

import (
	"net/http"

	"github.com/yungbote/neurobridge-backend/internal/inference/config"
	"github.com/yungbote/neurobridge-backend/internal/inference/router"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

func Register(mux *http.ServeMux, cfg *config.Config, log *logger.Logger, r *router.Router) {
	mux.HandleFunc("GET /compat/oai/v1/models", handleModels(log, r))
	mux.HandleFunc("POST /compat/oai/v1/embeddings", handleEmbeddings(cfg, log, r))
	mux.HandleFunc("POST /compat/oai/v1/responses", handleResponses(cfg, log, r))

	// Planned surfaces (kept stable early; may return 501 until engines are configured).
	mux.HandleFunc("POST /compat/oai/v1/images/generations", handleImagesGenerations(cfg, log, r))
	mux.HandleFunc("POST /compat/oai/v1/videos", handleVideosCreate(cfg, log, r))
	mux.HandleFunc("GET /compat/oai/v1/videos/{id}", handleVideosGet(cfg, log, r))
	mux.HandleFunc("GET /compat/oai/v1/videos/{id}/content", handleVideosContent(cfg, log, r))
	mux.HandleFunc("POST /compat/oai/v1/conversations", handleConversationsCreate(cfg, log, r))
}

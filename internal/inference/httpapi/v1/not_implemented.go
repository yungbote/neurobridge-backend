package v1

import (
	"net/http"

	"github.com/yungbote/neurobridge-backend/internal/inference/config"
	"github.com/yungbote/neurobridge-backend/internal/inference/router"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

func handleImagesGenerate(_ *config.Config, _ *logger.Logger, _ *router.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotImplemented, "image generation not implemented", "not_implemented", "")
	}
}

func handleVideosGenerate(_ *config.Config, _ *logger.Logger, _ *router.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotImplemented, "video generation not implemented", "not_implemented", "")
	}
}

func handleVideosGet(_ *config.Config, _ *logger.Logger, _ *router.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotImplemented, "video generation not implemented", "not_implemented", "")
	}
}

func handleVideosContent(_ *config.Config, _ *logger.Logger, _ *router.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotImplemented, "video generation not implemented", "not_implemented", "")
	}
}

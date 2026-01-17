package oai

import (
	"net/http"

	"github.com/yungbote/neurobridge-backend/internal/inference/config"
	"github.com/yungbote/neurobridge-backend/internal/inference/router"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

func handleImagesGenerations(_ *config.Config, _ *logger.Logger, _ *router.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotImplemented, "image generation not implemented", "server_error", "", "not_implemented")
	}
}

func handleVideosCreate(_ *config.Config, _ *logger.Logger, _ *router.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotImplemented, "video generation not implemented", "server_error", "", "not_implemented")
	}
}

func handleVideosGet(_ *config.Config, _ *logger.Logger, _ *router.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotImplemented, "video generation not implemented", "server_error", "", "not_implemented")
	}
}

func handleVideosContent(_ *config.Config, _ *logger.Logger, _ *router.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotImplemented, "video generation not implemented", "server_error", "", "not_implemented")
	}
}

func handleConversationsCreate(_ *config.Config, _ *logger.Logger, _ *router.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotImplemented, "conversations not implemented", "server_error", "", "not_implemented")
	}
}

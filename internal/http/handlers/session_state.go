package handlers

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yungbote/neurobridge-backend/internal/http/response"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type SessionStateHandler struct {
	sessionState services.SessionStateService
}

func NewSessionStateHandler(sessionState services.SessionStateService) *SessionStateHandler {
	return &SessionStateHandler{sessionState: sessionState}
}

// GET /api/session/state
func (h *SessionStateHandler) Get(c *gin.Context) {
	state, err := h.sessionState.Get(dbctx.Context{Ctx: c.Request.Context()})
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "get_session_state_failed", err)
		return
	}
	response.RespondOK(c, gin.H{"state": state})
}

// PATCH /api/session/state
func (h *SessionStateHandler) Patch(c *gin.Context) {
	var req services.SessionStatePatch
	if err := c.ShouldBindJSON(&req); err != nil {
		if !errors.Is(err, io.EOF) {
			response.RespondError(c, http.StatusBadRequest, "invalid_request", err)
			return
		}
		// Allow empty body as a simple "touch".
		req = services.SessionStatePatch{}
	}
	state, err := h.sessionState.Patch(dbctx.Context{Ctx: c.Request.Context()}, req)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "patch_session_state_failed", err)
		return
	}
	response.RespondOK(c, gin.H{"state": state})
}

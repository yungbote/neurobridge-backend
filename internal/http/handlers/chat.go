package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/http/response"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type ChatHandler struct {
	chat services.ChatService
}

func NewChatHandler(chat services.ChatService) *ChatHandler {
	return &ChatHandler{chat: chat}
}

type createThreadReq struct {
	Title  string     `json:"title"`
	PathID *uuid.UUID `json:"path_id"`
	JobID  *uuid.UUID `json:"job_id"`
}

// POST /api/chat/threads
func (h *ChatHandler) CreateThread(c *gin.Context) {
	var req createThreadReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_request", err)
		return
	}
	dbc := dbctx.Context{Ctx: c.Request.Context()}
	thread, err := h.chat.CreateThread(dbc, req.Title, req.PathID, req.JobID)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "create_thread_failed", err)
		return
	}
	response.RespondOK(c, gin.H{"thread": thread})
}

// GET /api/chat/threads?limit=50
func (h *ChatHandler) ListThreads(c *gin.Context) {
	limit := 50
	if v := strings.TrimSpace(c.Query("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	dbc := dbctx.Context{Ctx: c.Request.Context()}
	threads, err := h.chat.ListThreads(dbc, limit)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "list_threads_failed", err)
		return
	}
	response.RespondOK(c, gin.H{"threads": threads})
}

// GET /api/chat/threads/:id?limit=50
func (h *ChatHandler) GetThread(c *gin.Context) {
	threadID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_thread_id", err)
		return
	}
	limit := 50
	if v := strings.TrimSpace(c.Query("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	dbc := dbctx.Context{Ctx: c.Request.Context()}
	thread, msgs, err := h.chat.GetThread(dbc, threadID, limit)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "thread_not_found", err)
		return
	}
	response.RespondOK(c, gin.H{"thread": thread, "messages": msgs})
}

// GET /api/chat/threads/:id/messages?limit=50&before_seq=123
func (h *ChatHandler) ListMessages(c *gin.Context) {
	threadID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_thread_id", err)
		return
	}
	limit := 50
	if v := strings.TrimSpace(c.Query("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	var before *int64
	if v := strings.TrimSpace(c.Query("before_seq")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			before = &n
		}
	}
	dbc := dbctx.Context{Ctx: c.Request.Context()}
	msgs, err := h.chat.ListMessages(dbc, threadID, limit, before)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "list_messages_failed", err)
		return
	}
	response.RespondOK(c, gin.H{"messages": msgs})
}

// POST /api/chat/threads/:id/rebuild
func (h *ChatHandler) RebuildThread(c *gin.Context) {
	threadID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_thread_id", err)
		return
	}
	dbc := dbctx.Context{Ctx: c.Request.Context()}
	job, err := h.chat.RebuildThread(dbc, threadID)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "rebuild_thread_failed", err)
		return
	}
	response.RespondOK(c, gin.H{"job": job})
}

// DELETE /api/chat/threads/:id
func (h *ChatHandler) DeleteThread(c *gin.Context) {
	threadID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_thread_id", err)
		return
	}
	dbc := dbctx.Context{Ctx: c.Request.Context()}
	job, err := h.chat.DeleteThread(dbc, threadID)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "delete_thread_failed", err)
		return
	}
	response.RespondOK(c, gin.H{"job": job})
}

type sendMessageReq struct {
	Content        string `json:"content"`
	IdempotencyKey string `json:"idempotency_key"`
}

// POST /api/chat/threads/:id/messages
func (h *ChatHandler) SendMessage(c *gin.Context) {
	threadID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_thread_id", err)
		return
	}
	var req sendMessageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_request", err)
		return
	}

	idem := strings.TrimSpace(req.IdempotencyKey)
	if hdr := strings.TrimSpace(c.GetHeader("Idempotency-Key")); hdr != "" {
		idem = hdr
	}

	dbc := dbctx.Context{Ctx: c.Request.Context()}
	userMsg, asstMsg, job, err := h.chat.SendMessage(dbc, threadID, req.Content, idem)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "send_message_failed", err)
		return
	}
	response.RespondOK(c, gin.H{
		"user_message":      userMsg,
		"assistant_message": asstMsg,
		"job":               job,
	})
}

type updateMessageReq struct {
	Content string `json:"content"`
}

// PATCH /api/chat/threads/:id/messages/:message_id
func (h *ChatHandler) UpdateMessage(c *gin.Context) {
	threadID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_thread_id", err)
		return
	}
	messageID, err := uuid.Parse(c.Param("message_id"))
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_message_id", err)
		return
	}
	var req updateMessageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_request", err)
		return
	}
	dbc := dbctx.Context{Ctx: c.Request.Context()}
	job, err := h.chat.UpdateMessage(dbc, threadID, messageID, req.Content)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "update_message_failed", err)
		return
	}
	response.RespondOK(c, gin.H{"job": job})
}

// DELETE /api/chat/threads/:id/messages/:message_id
func (h *ChatHandler) DeleteMessage(c *gin.Context) {
	threadID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_thread_id", err)
		return
	}
	messageID, err := uuid.Parse(c.Param("message_id"))
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_message_id", err)
		return
	}
	dbc := dbctx.Context{Ctx: c.Request.Context()}
	job, err := h.chat.DeleteMessage(dbc, threadID, messageID)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "delete_message_failed", err)
		return
	}
	response.RespondOK(c, gin.H{"job": job})
}

// GET /api/chat/turns/:id
func (h *ChatHandler) GetTurn(c *gin.Context) {
	turnID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_turn_id", err)
		return
	}
	dbc := dbctx.Context{Ctx: c.Request.Context()}
	turn, err := h.chat.GetTurn(dbc, turnID)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "get_turn_failed", err)
		return
	}
	if turn == nil {
		response.RespondError(c, http.StatusNotFound, "turn_not_found", nil)
		return
	}
	response.RespondOK(c, gin.H{"turn": turn})
}

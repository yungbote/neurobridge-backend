package handlers

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/requestdata"
	"github.com/yungbote/neurobridge-backend/internal/sse"
)

type RealtimeHandler struct {
	Log *logger.Logger
	Hub *sse.SSEHub

	mu      sync.RWMutex
	clients map[uuid.UUID]*sse.SSEClient // key: SessionID (UserToken.ID)
}

func NewRealtimeHandler(log *logger.Logger, hub *sse.SSEHub) *RealtimeHandler {
	return &RealtimeHandler{
		Log:     log,
		Hub:     hub,
		clients: make(map[uuid.UUID]*sse.SSEClient),
	}
}

func (h *RealtimeHandler) SSEStream(c *gin.Context) {
	rd := requestdata.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	userID := rd.UserID
	sessionID := rd.SessionID
	h.Log.Info("SSEStream open", "user_id", userID.String(), "session_id", sessionID.String())
	if sessionID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing session id"})
		return
	}

	h.mu.Lock()
	// If this session already has a client, close it and replace.
	if existing, ok := h.clients[sessionID]; ok {
		h.Hub.CloseClient(existing)
		delete(h.clients, sessionID)
	}
	client := h.Hub.NewSSEClient(userID)
	client.ID = uuid.New()
	client.Logger = h.Log.With("SSEClientID", client.ID)

	// Store client keyed by sessionID
	h.clients[sessionID] = client
	h.mu.Unlock()

	// USER-GLOBAL CHANNEL: subscribe every session to the user's channel
	userChannel := userID.String() // or "user:"+userID.String()
	h.Hub.AddChannel(client, userChannel)

	// Now serve the SSE stream
	h.Hub.ServeHTTP(c.Writer, c.Request, client)

	// Cleanup after disconnect
	h.mu.Lock()
	delete(h.clients, sessionID)
	h.mu.Unlock()
	h.Hub.CloseClient(client)
}

func (h *RealtimeHandler) SSESubscribe(c *gin.Context) {
	rd := requestdata.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	sessionID := rd.SessionID
	if sessionID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing session id"})
		return
	}

	var req struct {
		Channel string `json:"channel"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Channel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel"})
		return
	}

	h.mu.RLock()
	client, exists := h.clients[sessionID]
	h.mu.RUnlock()
	if !exists {
		c.JSON(http.StatusConflict, gin.H{"error": "no active SSE connection for this session"})
		return
	}

	h.Hub.AddChannel(client, req.Channel)
	c.JSON(http.StatusOK, gin.H{"message": "subscribed", "channel": req.Channel})
}

func (h *RealtimeHandler) SSEUnsubscribe(c *gin.Context) {
	rd := requestdata.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	sessionID := rd.SessionID
	if sessionID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing session id"})
		return
	}

	var req struct {
		Channel string `json:"channel"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Channel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel"})
		return
	}

	h.mu.RLock()
	client, exists := h.clients[sessionID]
	h.mu.RUnlock()
	if !exists {
		c.JSON(http.StatusConflict, gin.H{"error": "no active SSE connection for this session"})
		return
	}

	h.Hub.RemoveChannel(client, req.Channel)
	c.JSON(http.StatusOK, gin.H{"message": "unsubscribed", "channel": req.Channel})
}











package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/realtime"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type UserHandler struct {
	userService services.UserService
	hub         *realtime.SSEHub // API server broadcasts directly to connected clients
	bucket      gcp.BucketService
}

func NewUserHandler(userService services.UserService, hub *realtime.SSEHub, bucket gcp.BucketService) *UserHandler {
	return &UserHandler{
		userService: userService,
		hub:         hub,
		bucket:      bucket,
	}
}

// GET /me
func (uh *UserHandler) GetMe(c *gin.Context) {
	me, err := uh.userService.GetMe(dbctx.Context{Ctx: c.Request.Context()})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	normalizeUserAvatarURL(uh.bucket, me)
	c.JSON(http.StatusOK, gin.H{"me": me})
}

// PATCH /user/name
// body: { "first_name": "...", "last_name": "..." }
func (uh *UserHandler) ChangeName(c *gin.Context) {
	var req struct {
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "detail": err.Error()})
		return
	}

	u, err := uh.userService.UpdateName(c.Request.Context(), req.FirstName, req.LastName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "change_name_failed", "detail": err.Error()})
		return
	}
	normalizeUserAvatarURL(uh.bucket, u)

	uh.broadcastUser(u.ID.String(), realtime.SSEEventUserNameChanged, gin.H{
		"first_name":   u.FirstName,
		"last_name":    u.LastName,
		"avatar_url":   u.AvatarURL,   // name change regenerates initials avatar
		"avatar_color": u.AvatarColor, // unchanged, but useful for clients
	})

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// PATCH /user/theme
// body: { "preferred_theme": "light" | "dark" | "system", "preferred_ui_theme": "classic" | "slate" | "dune" | "sage" | "aurora" | "ink" | "linen" | "ember" | "harbor" | "moss" }
func (uh *UserHandler) ChangeTheme(c *gin.Context) {
	var req struct {
		PreferredTheme   *string `json:"preferred_theme"`
		PreferredUITheme *string `json:"preferred_ui_theme"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "detail": err.Error()})
		return
	}
	if req.PreferredTheme == nil && req.PreferredUITheme == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "detail": "no theme changes provided"})
		return
	}

	u, err := uh.userService.UpdateThemePreferences(c.Request.Context(), req.PreferredTheme, req.PreferredUITheme)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "change_theme_failed", "detail": err.Error()})
		return
	}

	uh.broadcastUser(u.ID.String(), realtime.SSEEventUserThemeChanged, gin.H{
		"preferred_theme":    u.PreferredTheme,
		"preferred_ui_theme": u.PreferredUITheme,
	})

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// PATCH /user/avatar_color
// body: { "avatar_color": "#RRGGBB" } (or whatever convention you decide)
func (uh *UserHandler) ChangeAvatarColor(c *gin.Context) {
	var req struct {
		AvatarColor string `json:"avatar_color"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "detail": err.Error()})
		return
	}
	req.AvatarColor = strings.TrimSpace(req.AvatarColor)
	if req.AvatarColor == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "avatar_color_required"})
		return
	}

	u, err := uh.userService.UpdateAvatarColor(c.Request.Context(), req.AvatarColor)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "change_avatar_color_failed", "detail": err.Error()})
		return
	}
	normalizeUserAvatarURL(uh.bucket, u)

	uh.broadcastUser(u.ID.String(), realtime.SSEEventUserAvatarUpdated, gin.H{
		"avatar_url":   u.AvatarURL,
		"avatar_color": u.AvatarColor,
	})

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// POST /user/avatar/upload (multipart/form-data)
// field: "file"
func (uh *UserHandler) UploadAvatar(c *gin.Context) {
	// Accept up to ~10MB (adjust as you want)
	const maxBytes = 10 << 20

	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing_file"})
		return
	}

	f, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "open_file_failed", "detail": err.Error()})
		return
	}
	defer f.Close()

	raw, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read_file_failed", "detail": err.Error()})
		return
	}
	if len(raw) > maxBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file_too_large"})
		return
	}

	u, err := uh.userService.UploadAvatarImage(c.Request.Context(), raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "upload_avatar_failed", "detail": err.Error()})
		return
	}
	normalizeUserAvatarURL(uh.bucket, u)

	uh.broadcastUser(u.ID.String(), realtime.SSEEventUserAvatarUpdated, gin.H{
		"avatar_url":   u.AvatarURL,
		"avatar_color": u.AvatarColor, // unchanged; include anyway
	})

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GET /user/personalization
func (uh *UserHandler) GetPersonalizationPrefs(c *gin.Context) {
	row, err := uh.userService.GetPersonalizationPrefs(dbctx.Context{Ctx: c.Request.Context()})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if row == nil || len(row.PrefsJSON) == 0 || string(row.PrefsJSON) == "null" {
		c.JSON(http.StatusOK, gin.H{"prefs": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"prefs":      json.RawMessage(row.PrefsJSON),
		"updated_at": row.UpdatedAt,
	})
}

// PATCH /user/personalization
// body: { "prefs": { ... } }
func (uh *UserHandler) PatchPersonalizationPrefs(c *gin.Context) {
	var req struct {
		Prefs json.RawMessage `json:"prefs"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "detail": err.Error()})
		return
	}
	if len(req.Prefs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "prefs_required"})
		return
	}
	row, err := uh.userService.UpsertPersonalizationPrefs(c.Request.Context(), req.Prefs)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "prefs_update_failed", "detail": err.Error()})
		return
	}

	if row != nil {
		uh.broadcastUser(row.UserID.String(), realtime.SSEEventUserPrefsChanged, gin.H{
			"updated_at": row.UpdatedAt,
		})
	}

	if row == nil || len(row.PrefsJSON) == 0 || string(row.PrefsJSON) == "null" {
		c.JSON(http.StatusOK, gin.H{"prefs": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"prefs":      json.RawMessage(row.PrefsJSON),
		"updated_at": row.UpdatedAt,
	})
}

// ---- helpers ----

func (uh *UserHandler) broadcastUser(channel string, event realtime.SSEEvent, data any) {
	if uh == nil || uh.hub == nil {
		return
	}
	if strings.TrimSpace(channel) == "" {
		return
	}
	uh.hub.Broadcast(realtime.SSEMessage{
		Channel: channel,
		Event:   event,
		Data:    data,
	})
}

// Keep it here if you want to ensure the frontend matches.
func (uh *UserHandler) mustHub() error {
	if uh == nil || uh.hub == nil {
		return fmt.Errorf("SSE hub not configured")
	}
	return nil
}

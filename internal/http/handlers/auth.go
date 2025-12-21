package handlers

import (
	"github.com/gin-gonic/gin"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/http/response"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"net/http"
)

type AuthHandler struct {
	authService services.AuthService
}

func NewAuthHandler(authService services.AuthService) *AuthHandler {
	return &AuthHandler{authService: authService}
}

func (ah *AuthHandler) Register(c *gin.Context) {
	var req struct {
		Email     string `json:"email"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Password  string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_request", err)
		return
	}
	user := types.User{
		Email:     req.Email,
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Password:  req.Password,
	}
	if err := ah.authService.RegisterUser(c.Request.Context(), &user); err != nil {
		response.RespondError(c, http.StatusBadRequest, "registration_failed", err)
		return
	}
	response.RespondOK(c, gin.H{"ok": true})
}

func (ah *AuthHandler) Login(c *gin.Context) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_request", err)
		return
	}
	accessToken, refreshToken, err := ah.authService.LoginUser(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		response.RespondError(c, http.StatusUnauthorized, "invalid_credentials", err)
		return
	}
	expiresIn := int(ah.authService.GetAccessTTL().Seconds())
	response.RespondOK(c, gin.H{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"expires_in":    expiresIn,
	})
}

func (ah *AuthHandler) Refresh(c *gin.Context) {
	accessToken, refreshToken, err := ah.authService.RefreshUser(c.Request.Context())
	if err != nil {
		response.RespondError(c, http.StatusUnauthorized, "refresh_failed", err)
		return
	}
	expiresIn := int(ah.authService.GetAccessTTL().Seconds())
	response.RespondOK(c, gin.H{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"expires_in":    expiresIn,
	})
}

func (ah *AuthHandler) Logout(c *gin.Context) {
	if err := ah.authService.LogoutUser(c.Request.Context()); err != nil {
		response.RespondError(c, http.StatusBadRequest, "logout_failed", err)
		return
	}
	response.RespondOK(c, gin.H{"ok": true})
}

func (ah *AuthHandler) OAuthNonce(c *gin.Context) {
	var req struct {
		Provider string `json:"provider"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_request", err)
		return
	}
	nonceID, nonce, expiresIn, err := ah.authService.CreateOAuthNonce(c.Request.Context(), req.Provider)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "nonce_failed", err)
		return
	}
	response.RespondOK(c, gin.H{
		"nonce_id":    nonceID.String(),
		"nonce":       nonce,
		"expires_in":  expiresIn,
	})
}

func (ah *AuthHandler) OAuthGoogle(c *gin.Context) {
	var req struct {
		IDToken   string `json:"id_token"`
		NonceID   string `json:"nonce_id"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_request", err)
		return
	}
	nonceID, err := uuid.Parse(req.NonceID)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_nonce_id", err)
		return
	}
	accessToken, refreshToken, err := ah.authService.OAuthLoginGoogle(c.Request.Context(), req.IDToken, nonceID, req.FirstName, req.LastName)
	if err != nil {
		response.RespondError(c, http.StatusUnauthorized, "oauth_login_failed", err)
		return
	}
	expiresIn := int(ah.authService.GetAccessTTL().Seconds())
	response.RespondOK(c, gin.H{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"expires_in":    expiresIn,
	})
}

func (ah *AuthHandler) OAuthApple(c *gin.Context) {
	var req struct {
		IDToken   string `json:"id_token"`
		NonceID   string `json:"nonce_id"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_request", err)
		return
	}
	nonceID, err := uuid.Parse(req.NonceID)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_nonce_id", err)
		return
	}
	accessToken, refreshToken, err := ah.authService.OAuthLoginApple(c.Request.Context(), req.IDToken, nonceID, req.FirstName, req.LastName)
	if err != nil {
		response.RespondError(c, http.StatusUnauthorized, "oauth_login_failed", err)
		return
	}
	expiresIn := int(ah.authService.GetAccessTTL().Seconds())
	response.RespondOK(c, gin.H{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"expires_in":    expiresIn,
	})
}











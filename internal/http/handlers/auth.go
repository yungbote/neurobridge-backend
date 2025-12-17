package handlers

import (
  "net/http"
  "github.com/gin-gonic/gin"
  "github.com/yungbote/neurobridge-backend/internal/types"
  "github.com/yungbote/neurobridge-backend/internal/services"
)

type AuthHandler struct {
  authService       services.AuthService
}

func NewAuthHandler(authService services.AuthService) *AuthHandler {
  return &AuthHandler{authService: authService}
}

func (ah *AuthHandler) Register(c *gin.Context) {
  var req struct {
    Email       string      `json:"email"`
    FirstName   string      `json:"first_name"`
    LastName    string      `json:"last_name"`
    Password    string      `json:"password"`
  }
  if err := c.ShouldBindJSON(&req); err != nil {
    RespondError(c, http.StatusBadRequest, "invalid_request", err)
    return
  }
  user := types.User{
    Email:      req.Email,
    FirstName:  req.FirstName,
    LastName:   req.LastName,
    Password:   req.Password,
  }
  if err := ah.authService.RegisterUser(c.Request.Context(), &user); err != nil {
    RespondError(c, http.StatusBadRequest, "registration_failed", err)
    return
  }
  RespondOK(c, gin.H{"ok": true})
}

func (ah *AuthHandler) Login(c *gin.Context) {
  var req struct {
    Email       string      `json:"email"`
    Password    string      `json:"password"`
  }
  if err := c.ShouldBindJSON(&req); err != nil {
    RespondError(c, http.StatusBadRequest, "invalid_request", err)
    return
  }
  accessToken, refreshToken, err := ah.authService.LoginUser(c.Request.Context(), req.Email, req.Password)
  if err != nil {
    RespondError(c, http.StatusUnauthorized, "invalid_credentials", err)
    return
  }
  expiresIn := int(ah.authService.GetAccessTTL().Seconds())
  RespondOK(c, gin.H{
    "access_token":   accessToken,
    "refresh_token":  refreshToken,
    "expires_in":     expiresIn,
  })
}

func (ah *AuthHandler) Refresh(c *gin.Context) {
  accessToken, refreshToken, err := ah.authService.RefreshUser(c.Request.Context())
  if err != nil {
    RespondError(c, http.StatusUnauthorized, "refresh_failed", err)
    return
  }
  expiresIn := int(ah.authService.GetAccessTTL().Seconds())
  RespondOK(c, gin.H{
    "access_token":   accessToken,
    "refresh_token":  refreshToken,
    "expires_in":     expiresIn,
  })
}

func (ah *AuthHandler) Logout(c *gin.Context) {
  if err := ah.authService.LogoutUser(c.Request.Context()); err != nil {
    RespondError(c, http.StatusBadRequest, "logout_failed", err)
    return
  }
  RespondOK(c, gin.H{"ok": true})
}











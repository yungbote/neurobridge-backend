package handlers

import (
  "net/http"
  "github.com/gin-gonic/gin"
  "github.com/yungbote/neurobridge-backend/internal/types"
  "github.com/yungbote/neurobridge-backend/internal/services"
//  "github.com/yungbote/neurobridge-backend/internal/sse"
//  "github.com/yungbote/neurobridge-backend/internal/ssedata"
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
    c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
    return
  }
  user := types.User{
    Email:      req.Email,
    FirstName:  req.FirstName,
    LastName:   req.LastName,
    Password:   req.Password,
  }
  err := ah.authService.RegisterUser(c.Request.Context(), &user)
  if err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
    return
  }
  c.JSON(http.StatusOK, gin.H{"success": "true"})
}

func (ah *AuthHandler) Login(c *gin.Context) {
  var req struct {
    Email         string      `json:"email"`
    Password      string      `json:"password"`
  }
  if err := c.ShouldBindJSON(&req); err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
    return
  }
  accessToken, refreshToken, err := ah.authService.LoginUser(c.Request.Context(), req.Email, req.Password)
  if err != nil {
    c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
    return
  }
  accessTTL := ah.authService.GetAccessTTL()
  expiresIn := int(accessTTL.Seconds())
  c.JSON(http.StatusOK, gin.H{"access_token": accessToken, "refresh_token": refreshToken, "expires_in": expiresIn})
}

func (ah *AuthHandler) Refresh(c *gin.Context) {
  accessToken, refreshToken, err := ah.authService.RefreshUser(c.Request.Context())
  if err != nil {
    c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
    return
  }
  accessTTL := ah.authService.GetAccessTTL()
  expiresIn := int(accessTTL.Seconds())
  c.JSON(http.StatusOK, gin.H{"access_token": accessToken, "refresh_token": refreshToken, "expires_in": expiresIn})
}

func (ah *AuthHandler) Logout(c *gin.Context) {
  err := ah.authService.LogoutUser(c.Request.Context())
  if err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
    return
  }
  c.JSON(http.StatusOK, gin.H{"message": "logged out successfully"})
}


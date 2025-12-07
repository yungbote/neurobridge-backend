package handlers

import (
  "net/http"
  "github.com/gin-gonic/gin"
  "github.com/yungbote/neurobridge-backend/internal/services"
)

type UserHandler struct {
  userService     services.UserService
}

func NewUserHandler(userService services.UserService) *UserHandler {
  return &UserHandler{userService: userService}
}

func (uh *UserHandler) GetMe(c *gin.Context) {
  me, err := uh.userService.GetMe(c.Request.Context(), nil)
  if err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
    return
  }
  c.JSON(http.StatusOK, gin.H{"me": me})
}



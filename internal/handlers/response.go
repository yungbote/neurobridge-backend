package handlers

import (
  "net/http"
  "github.com/gin-gonic/gin"
)

type APIError struct {
  Message     string	`json:"message"`
  Code	      string	`json:"code,omitempty"`
}

type ErrorEnvelope struct {
  Error	      APIError	`json:"error"`
}

func RespondError(c *gin.Context, status int, code string, err error) {
  msg := "unknown error"
  if err != nil {
    msg = err.Error()
  }
  c.JSON(status, ErrorEnvelope{
    Error: APIError{
      Message: msg,
      Code:    code,
    },
  })
}

func RespondOK(c *gin.Context, payload any) {
  c.JSON(http.StatusOK, payload)
}











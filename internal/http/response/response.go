package response

import (
	"net/http"
	"github.com/gin-gonic/gin"
)


type APIError struct {
	Message			string		`json:"message"`
	Code				string		`json:"code,omitempty"`
}

type ErrorEnvelope struct {
	Error				APIError	`json:"error"`
}

func OK(c *gin.Context, payload any) {
	c.JSON(http.StatusOK, payload)
}











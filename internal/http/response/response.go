package response

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

type APIError struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

type ErrorEnvelope struct {
	Error     APIError `json:"error"`
	TraceID   string   `json:"trace_id,omitempty"`
	RequestID string   `json:"request_id,omitempty"`
}

func RespondError(c *gin.Context, status int, code string, err error) {
	msg := "unknown error"
	if err != nil {
		msg = err.Error()
	}
	traceID := c.GetString("trace_id")
	requestID := c.GetString("request_id")
	c.JSON(status, ErrorEnvelope{
		Error: APIError{
			Message: msg,
			Code:    code,
		},
		TraceID:   traceID,
		RequestID: requestID,
	})
}

func RespondOK(c *gin.Context, payload any) {
	c.JSON(http.StatusOK, payload)
}

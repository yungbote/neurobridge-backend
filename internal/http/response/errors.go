package reponse

import (
	"github.com/gin-gonic/gin"
)

func Error(c *gin.Context, status int, code string, err error) {
	msg := "unknown error"
	if err != nil {
		msg = err.Error()
	}
	c.JSON(status, ErrorEnvelope{
		Error: APIError{
			Message: msg,
			Code:		 code,
		},
	})
}











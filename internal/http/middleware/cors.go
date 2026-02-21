package middleware

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func CORS() gin.HandlerFunc {
	return cors.New(cors.Config{
		AllowOrigins: []string{
			"http://localhost:80",
			"http://localhost:3000",
			"http://localhost:5174",
			"http://localhost:5173",
			"http://127.0.0.1:80",
			"http://127.0.0.1:3000",
			"http://127.0.0.1:5174",
			"http://127.0.0.1:5173",
		},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowHeaders:     []string{"Authorization", "Content-Type", "X-Requested-With", "Idempotency-Key"},
		AllowCredentials: true,
	})
}

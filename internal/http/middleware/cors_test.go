package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCORSAllowsLocalDevOrigins(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	origins := []string{
		"http://localhost:5174",
		"http://127.0.0.1:5174",
	}

	for _, origin := range origins {
		origin := origin
		t.Run(origin, func(t *testing.T) {
			t.Parallel()
			r := gin.New()
			r.Use(CORS())
			r.OPTIONS("/api/login", func(c *gin.Context) {
				c.Status(http.StatusNoContent)
			})

			req := httptest.NewRequest(http.MethodOptions, "/api/login", nil)
			req.Header.Set("Origin", origin)
			req.Header.Set("Access-Control-Request-Method", http.MethodPost)

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusNoContent {
				t.Fatalf("unexpected status: got=%d want=%d", rec.Code, http.StatusNoContent)
			}
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
				t.Fatalf("unexpected allow-origin header: got=%q want=%q", got, origin)
			}
		})
	}
}

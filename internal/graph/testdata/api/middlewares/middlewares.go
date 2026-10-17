package middlewares

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Request-ID", "generated")
		c.Next()
	}
}

func Logger(c *gin.Context) { c.Next() }

func Late(c *gin.Context) { c.Next() }

type Auth struct{}

// RequireUser answers 401 unless a bearer token is sent.
func (a *Auth) RequireUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

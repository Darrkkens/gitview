// Package middlewares has the authentication, authorization and CORS
// middlewares of the fixture.
package middlewares

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// Auth reads the bearer token and answers 401 without one.
func Auth(c *gin.Context) {
	token := c.GetHeader("Authorization")
	if token == "" {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	log.Printf("auth token=%s", token)                       // GO-LOG-001: credential in the logs
	t, _, err := jwt.NewParser().ParseUnverified(token, nil) // GO-AUTHN-003
	if err != nil || t == nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	c.Set("company_id", "from-token")
	c.Set("role", "from-token")
	c.Next()
}

// RequireAdmin answers 403 unless the session has the admin role.
func RequireAdmin(c *gin.Context) {
	if c.GetString("role") != "admin" {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	c.Next()
}

// CORS reflects any origin and allows credentials (GO-CONF-001).
func CORS(c *gin.Context) {
	origin := c.GetHeader("Origin")
	c.Header("Access-Control-Allow-Origin", origin)
	c.Header("Access-Control-Allow-Credentials", "true")
	c.Next()
}

var allowed = map[string]bool{"https://app.example": true}

// CORSAllowList echoes the origin only when it is on the allow-list, and
// refuses other origins with 403 (before any authentication).
func CORSAllowList(c *gin.Context) {
	origin := c.GetHeader("Origin")
	if origin == "" {
		c.Next()
		return
	}
	if !allowed[origin] {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	c.Header("Access-Control-Allow-Origin", origin)
	c.Header("Access-Control-Allow-Credentials", "true")
	c.Next()
}

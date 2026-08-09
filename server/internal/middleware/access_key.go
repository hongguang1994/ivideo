package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const AccessKeyHeader = "X-Ivideo-Access-Key"

// AccessKey protects every ivideo API endpoint when an access key is set.
// Browsers cannot send custom WebSocket headers, so WebSocket clients may pass
// the same value as access_key in the query string.
func AccessKey(key string) gin.HandlerFunc {
	key = strings.TrimSpace(key)
	return func(c *gin.Context) {
		if key == "" {
			c.Next()
			return
		}
		provided := c.GetHeader(AccessKeyHeader)
		if provided == "" && c.IsWebsocket() {
			provided = c.Query("access_key")
		}
		if subtle.ConstantTimeCompare([]byte(provided), []byte(key)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "msg": "需要访问密钥", "data": nil})
			return
		}
		c.Next()
	}
}

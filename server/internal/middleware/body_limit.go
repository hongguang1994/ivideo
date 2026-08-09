package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// DefaultMaxRequestBodyBytes keeps API JSON payloads bounded while leaving
// binary media streaming endpoints unaffected (they do not accept request
// bodies). Two MiB is ample for batch imports and provider tokens.
const DefaultMaxRequestBodyBytes int64 = 2 << 20

// BodyLimit rejects oversized API payloads before a JSON binder can allocate
// memory for them. MaxBytesReader also covers chunked requests with no useful
// Content-Length header.
func BodyLimit(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if limit <= 0 || c.Request.Body == nil {
			c.Next()
			return
		}
		if c.Request.ContentLength > limit {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"code": http.StatusRequestEntityTooLarge, "msg": "请求内容过大", "data": nil})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

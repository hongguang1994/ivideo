package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAccessKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(AccessKey("test-key"))
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for _, test := range []struct {
		key  string
		want int
	}{{"", http.StatusUnauthorized}, {"wrong", http.StatusUnauthorized}, {"test-key", http.StatusNoContent}} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if test.key != "" {
			req.Header.Set(AccessKeyHeader, test.key)
		}
		writer := httptest.NewRecorder()
		router.ServeHTTP(writer, req)
		if writer.Code != test.want {
			t.Fatalf("key %q: got %d want %d", test.key, writer.Code, test.want)
		}
	}
}

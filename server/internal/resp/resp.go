// Package resp 统一 JSON 接口的返回结构：{code, msg, data}。
//
// 约定：HTTP 状态码保留语义（200 成功 / 4xx-5xx 错误），
// body 统一为 {code, msg, data}，成功时 code=0，错误时 code=对应 HTTP 状态码。
//
// 注意：流式/二进制/重定向响应（stream、hls 切片、file 302）不套此结构。
package resp

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

var (
	upstreamURLPattern = regexp.MustCompile(`https?://[^\s"'<>]+`)
	secretPattern      = regexp.MustCompile(`(?i)\b(access[_-]?token|refresh[_-]?token|token|cookie|authorization)\s*[:=]\s*["']?[^\s,;"'}]+`)
	bearerPattern      = regexp.MustCompile(`(?i)\bbearer\s+[^\s,;"'}]+`)
)

// Body 是统一响应体。
type Body struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data"`
}

// OK 返回成功，data 为业务数据。
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Body{Code: 0, Msg: "ok", Data: data})
}

// Fail 返回错误，status 同时作为 HTTP 状态码与业务 code。
func Fail(c *gin.Context, status int, msg string) {
	c.JSON(status, Body{Code: status, Msg: SafeMessage(msg), Data: nil})
}

// SafeMessage preserves useful failure context without returning signed URLs,
// provider tokens, cookies, or authorization headers to browser clients.
// The raw error should still be logged by the owning backend component.
func SafeMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "请求失败"
	}
	msg = upstreamURLPattern.ReplaceAllString(msg, "上游服务")
	msg = bearerPattern.ReplaceAllString(msg, "Bearer [已隐藏]")
	msg = secretPattern.ReplaceAllString(msg, "$1=[已隐藏]")
	if len(msg) > 800 {
		return "上游服务返回了过长的错误信息，请查看后端日志"
	}
	return msg
}

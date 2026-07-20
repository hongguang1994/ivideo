// Package resp 统一 JSON 接口的返回结构：{code, msg, data}。
//
// 约定：HTTP 状态码保留语义（200 成功 / 4xx-5xx 错误），
// body 统一为 {code, msg, data}，成功时 code=0，错误时 code=对应 HTTP 状态码。
//
// 注意：流式/二进制/重定向响应（stream、hls 切片、file 302）不套此结构。
package resp

import (
	"net/http"

	"github.com/gin-gonic/gin"
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
	c.JSON(status, Body{Code: status, Msg: msg, Data: nil})
}

// Package middleware 提供通用的 HTTP 中间件核心实现，供 httpx 及其它 net/http
// 兼容框架复用。
//
// 设计约定：
//
//   - 一个中间件一个文件、一个类型；用 NewXxx(...) 构造（构造时完成参数预计算，
//     避免每请求重复解析），用 (m *Xxx) Middleware() 取得标准中间件；
//
//   - Middleware() 返回标准形式 func(http.Handler) http.Handler，不依赖具体框架，
//     可用于标准 net/http、httpx、gin、echo 等：
//
//     // 标准 net/http
//     handler := middleware.NewRecovery().Middleware()(mux)
//
//     // httpx（internal_middleware.go 已内置 With* 便捷函数，直接 server.Use 即可）
//     server.Use(httpx.WithRecovery())
//
//     // gin
//     router.Use(gin.WrapH(middleware.NewRequestID().Middleware()(ginEngine)))
//
//   - 错误响应通过全局 ErrorHandler 写入（默认 http.Error 纯文本）。httpx 主包会在
//     init 时注入其统一 JSON 错误响应，使 httpx.With* 中间件保持原响应格式；
//     在其它框架中可用 SetErrorHandler 注入框架自己的错误渲染。
package middleware

import (
	"context"
	"net/http"
)

// ErrorHandler 中间件产生错误响应时（如超时、拒绝、解密失败）的写入函数。
type ErrorHandler func(ctx context.Context, w http.ResponseWriter, status int, msg string)

// defaultErrorHandler 默认错误响应：http.Error 纯文本（不依赖任何框架）。
func defaultErrorHandler(_ context.Context, w http.ResponseWriter, status int, msg string) {
	http.Error(w, msg, status)
}

var errorHandler ErrorHandler = defaultErrorHandler

// SetErrorHandler 替换全局错误响应写入函数；fn 为 nil 时恢复默认（http.Error）。
//
// httpx 主包在 init 中注入其统一 JSON 错误响应（携带 request_id），因此经
// httpx.With* 注册的中间件保持原有错误响应格式不变；在 gin/echo 等框架中
// 可注入框架自身的错误渲染，让中间件产出的错误与框架风格一致。
func SetErrorHandler(fn ErrorHandler) {
	if fn == nil {
		errorHandler = defaultErrorHandler
		return
	}
	errorHandler = fn
}

// WriteError 使用当前全局 ErrorHandler 写入错误响应。
// 供本包中间件内部及不直接依赖 httpx 主包的调用方（如 jwt.AuthMiddleware）
// 复用同一错误渲染：httpx 主包被 import 时输出统一 JSON，否则默认 http.Error。
func WriteError(ctx context.Context, w http.ResponseWriter, status int, msg string) {
	writeError(ctx, w, status, msg)
}

// writeError 使用全局 ErrorHandler 写入错误响应。
func writeError(ctx context.Context, w http.ResponseWriter, status int, msg string) {
	errorHandler(ctx, w, status, msg)
}

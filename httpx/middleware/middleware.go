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
//   - 错误响应通过 ErrorHandler 写入（默认 http.Error 纯文本）。
//     渲染函数的解析顺序为：**请求 context 携带的**（ContextWithErrorHandler）
//     优先，未携带时回退到进程级全局（SetErrorHandler）。
//     httpx 主包的 Server 会在每个请求上注入自己的统一 JSON 渲染，
//     因此经 httpx 分发的请求保持 httpx 响应格式，
//     而同进程内其它框架（gin/echo）的路由不受影响。
//     需要改变本包在无请求作用域时的默认行为，用 SetErrorHandler 显式注入。
package middleware

import (
	"context"
	"net/http"
	"sync"
)

// ErrorHandler 中间件产生错误响应时（如超时、拒绝、解密失败）的写入函数。
type ErrorHandler func(ctx context.Context, w http.ResponseWriter, status int, msg string)

// defaultErrorHandler 默认错误响应：http.Error 纯文本（不依赖任何框架）。
func defaultErrorHandler(_ context.Context, w http.ResponseWriter, status int, msg string) {
	http.Error(w, msg, status)
}

var (
	errorHandlerMu sync.RWMutex
	errorHandler   ErrorHandler = defaultErrorHandler
)

// SetErrorHandler 替换全局错误响应写入函数；fn 为 nil 时恢复默认（http.Error）。
//
// 全局值仅在**请求 context 未携带**渲染函数时生效（见 ContextWithErrorHandler）。
// httpx 主包不再在 init 中改写本全局：
// 它改为在 Server 处理请求时按请求注入，避免“仅 import httpx”就静默改变
// 同进程内 gin/echo 路由的错误响应格式。
// 无需请求作用域、希望进程级生效时（如直接用本包供 gin/echo），用本函数显式注入。
func SetErrorHandler(fn ErrorHandler) {
	errorHandlerMu.Lock()
	defer errorHandlerMu.Unlock()
	if fn == nil {
		errorHandler = defaultErrorHandler
		return
	}
	errorHandler = fn
}

// globalErrorHandler 返回当前全局错误渲染函数。
func globalErrorHandler() ErrorHandler {
	errorHandlerMu.RLock()
	defer errorHandlerMu.RUnlock()
	return errorHandler
}

// errorHandlerKey 请求作用域错误渲染函数的 context key。
// 用私有空结构体：不会与其它包的 key 冲突。
type errorHandlerKey struct{}

// ContextWithErrorHandler 返回携带错误渲染函数 fn 的 context 副本。
//
// 错误响应格式因此可以按**请求**作用域生效，而不是只能靠进程级全局状态。
// httpx 的 Server 在每个请求上注入自己的统一 JSON 渲染，
// 使经 httpx 分发的请求保持 httpx 格式，同进程内其它框架的路由不受牵连。
// fn 为 nil 时原样返回 ctx。
func ContextWithErrorHandler(ctx context.Context, fn ErrorHandler) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, errorHandlerKey{}, fn)
}

// WriteError 使用当前生效的 ErrorHandler 写入错误响应。
// 优先使用 ctx 携带的渲染函数（httpx 请求即为这种情形），否则回退全局。
// 供本包中间件内部及不直接依赖 httpx 主包的调用方（如 jwt.AuthMiddleware）
// 复用同一错误渲染。
func WriteError(ctx context.Context, w http.ResponseWriter, status int, msg string) {
	writeError(ctx, w, status, msg)
}

// writeError 使用当前生效的 ErrorHandler 写入错误响应。
func writeError(ctx context.Context, w http.ResponseWriter, status int, msg string) {
	if ctx != nil {
		if fn, ok := ctx.Value(errorHandlerKey{}).(ErrorHandler); ok && fn != nil {
			fn(ctx, w, status, msg)
			return
		}
	}
	globalErrorHandler()(ctx, w, status, msg)
}

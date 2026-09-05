package middleware

import (
	"context"
	"net/http"
	"runtime/debug"

	"github.com/chihqiang/infra-go/logger"
)

// Recovery 是 panic 恢复中间件。
// 捕获 handler 中的 panic，记录堆栈并返回 500，防止进程崩溃。
type Recovery struct{}

// NewRecovery 创建 Recovery 中间件。
func NewRecovery() *Recovery {
	return &Recovery{}
}

// Middleware 返回标准形式 func(http.Handler) http.Handler 的 panic 恢复中间件。
func (r *Recovery) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.ErrorCtx(req.Context(), "panic recovered",
						logger.Any("panic", rec),
						logger.String("method", req.Method),
						logger.String("path", req.URL.Path),
						logger.String("remote", req.RemoteAddr),
						logger.String("stack", string(debug.Stack())),
					)
					// 使用空 context 输出错误，避免依赖请求链路中的 request_id
					// （httpx 统一响应下与无 ctx 的 WriteHTTPError 行为一致）。
					writeError(context.Background(), w, http.StatusInternalServerError, "internal server error")
				}
			}()
			next.ServeHTTP(w, req)
		})
	}
}

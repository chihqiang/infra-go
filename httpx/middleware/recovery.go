package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/chihqiang/infra-go/httpx/x"
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
						logger.String("remote", x.ClientIP(req)),
						logger.String("stack", string(debug.Stack())),
					)
					// 用请求 context 渲染错误，与其它中间件（超时、限流、体积限制）保持一致，
					// 使响应体带上 request_id —— 500 正是最需要该关联 ID 的场景。
					// 取 context 中的值不受取消影响（ctx.Value 不查 Done），
					// 因此即使请求已被取消（如客户端提前断开）仍能取到 request_id。
					writeError(req.Context(), w, http.StatusInternalServerError, "internal server error")
				}
			}()
			next.ServeHTTP(w, req)
		})
	}
}

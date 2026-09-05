package middleware

import (
	"net/http"
	"time"

	"github.com/chihqiang/infra-go/httpx/match"
	"github.com/chihqiang/infra-go/httpx/respw"
	"github.com/chihqiang/infra-go/logger"
)

// AccessLogger 是请求访问日志中间件。
// 记录每个请求的方法、路径、状态码、响应字节数和耗时。
type AccessLogger struct {
	matcher *match.PathMatcher
}

// NewAccessLogger 创建访问日志中间件。
// skipPaths 为不记录日志的路径列表，常用于健康检查、心跳等高频探活接口。
// 匹配方式：精确匹配（如 "/healthz"）或以 "*" 结尾的前缀通配（如 "/internal/*"）。
func NewAccessLogger(skipPaths ...string) *AccessLogger {
	return &AccessLogger{matcher: match.NewPathMatcher(skipPaths)}
}

// Middleware 返回标准形式 func(http.Handler) http.Handler 的访问日志中间件。
// 配合 trace 包使用时，logger 的 Ctx 提取器会自动带上 trace_id/span_id。
func (l *AccessLogger) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := respw.NewRecorderWriter(w)
			next.ServeHTTP(rec, r)
			// 命中忽略规则的路径不写访问日志（业务照常处理）
			if l.matcher.Match(r.URL.Path) {
				return
			}
			logger.InfoCtx(r.Context(), "http request",
				logger.String("method", r.Method),
				logger.String("path", r.URL.Path),
				logger.String("query", r.URL.RawQuery),
				logger.String("remote", r.RemoteAddr),
				logger.Int("status", rec.Status()),
				logger.Int("bytes", rec.Bytes()),
				logger.Duration("latency", time.Since(start)),
			)
		})
	}
}

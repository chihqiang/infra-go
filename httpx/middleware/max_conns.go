package middleware

import (
	"net/http"

	"github.com/chihqiang/infra-go/logger"
)

// MaxConns 是并发连接数限制中间件。
// 基于带缓冲 channel 的轻量信号量，仅用于并发计数（不需要 Wait 语义）；
// 并发数超过上限时直接返回 503 Service Unavailable，防止连接耗尽。
type MaxConns struct {
	sem chan struct{}
}

// NewMaxConns 创建并发连接数限制中间件。
// n <= 0 表示不限制。
func NewMaxConns(n int) *MaxConns {
	if n <= 0 {
		return &MaxConns{}
	}
	return &MaxConns{sem: make(chan struct{}, n)}
}

// Middleware 返回标准形式 func(http.Handler) http.Handler 的并发数限制中间件。
func (m *MaxConns) Middleware() func(http.Handler) http.Handler {
	// n <= 0：不限制，直接透传
	if m.sem == nil {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case m.sem <- struct{}{}:
				defer func() { <-m.sem }()
				next.ServeHTTP(w, r)
			default:
				logger.WarnCtx(r.Context(), "too many concurrent connections",
					logger.Int("limit", cap(m.sem)),
					logger.String("path", r.URL.Path),
				)
				writeError(r.Context(), w, http.StatusServiceUnavailable, "too many concurrent connections")
			}
		})
	}
}

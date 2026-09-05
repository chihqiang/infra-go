package middleware

import (
	"net/http"

	"github.com/chihqiang/infra-go/logger"
)

// MaxBytes 是请求体大小限制中间件。
// 请求体 Content-Length 超过 n 字节时直接返回 413 Request Entity Too Large；
// 对分块传输（无 Content-Length）的请求，用 http.MaxBytesReader 在读取时限制。
type MaxBytes struct {
	n int64
}

// NewMaxBytes 创建请求体大小限制中间件。
// n <= 0 表示不限制。
func NewMaxBytes(n int64) *MaxBytes {
	return &MaxBytes{n: n}
}

// Middleware 返回标准形式 func(http.Handler) http.Handler 的请求体大小限制中间件。
func (m *MaxBytes) Middleware() func(http.Handler) http.Handler {
	// n <= 0：不限制，直接透传
	if m.n <= 0 {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > m.n {
				logger.WarnCtx(r.Context(), "request body too large",
					logger.Int64("limit", m.n),
					logger.Int64("content_length", r.ContentLength),
					logger.String("path", r.URL.Path),
				)
				writeError(r.Context(), w, http.StatusRequestEntityTooLarge, "request entity too large")
				return
			}

			// 限制读取时的最大字节数（覆盖分块传输场景）
			r.Body = http.MaxBytesReader(w, r.Body, m.n)
			next.ServeHTTP(w, r)
		})
	}
}

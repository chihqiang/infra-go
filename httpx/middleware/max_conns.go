package middleware

import (
	"net/http"
	"time"

	"github.com/chihqiang/infra-go/logger"
)

// maxConnsRetryAfter 是并发超限时的默认重试提示间隔。
//
// 并发占用通常是短时堆积，因此给出较短的提示值；
// RFC 9110 §15.6.4 建议 503 响应附带 Retry-After。
const maxConnsRetryAfter = time.Second

// MaxConns 是并发连接数限制中间件。
// 基于带缓冲 channel 的轻量信号量，仅用于并发计数（不需要 Wait 语义）；
// 并发数超过上限时直接返回 503 Service Unavailable，防止连接耗尽。
type MaxConns struct {
	sem chan struct{}
	// retryAfter 为并发超限时提示客户端的重试间隔，默认 maxConnsRetryAfter。
	retryAfter time.Duration
}

// NewMaxConns 创建并发连接数限制中间件。
// n <= 0 表示不限制。
func NewMaxConns(n int) *MaxConns {
	if n <= 0 {
		return &MaxConns{}
	}
	return &MaxConns{sem: make(chan struct{}, n), retryAfter: maxConnsRetryAfter}
}

// WithRetryAfter 设置并发超限时的 Retry-After 提示间隔（RFC 9110 §10.2.3）。
// d <= 0 表示不发送该头。
func (m *MaxConns) WithRetryAfter(d time.Duration) *MaxConns {
	m.retryAfter = d
	return m
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
				// RFC 9110 §15.6.4：服务器过载导致的 503 SHOULD 给出 Retry-After
				WriteRetryAfter(r.Context(), w, http.StatusServiceUnavailable,
					m.retryAfter, "too many concurrent connections")
			}
		})
	}
}

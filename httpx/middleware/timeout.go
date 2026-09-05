package middleware

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/chihqiang/infra-go/httpx/respw"
)

// statusClientClosedRequest 客户端主动关闭请求（非标准状态码 499，nginx 约定）。
const statusClientClosedRequest = 499

// Timeout 是请求超时中间件。
// 每个请求最多执行 duration，超时返回 503 Service Unavailable。
// 客户端主动断开返回 499；WebSocket / SSE 请求不受超时限制。
type Timeout struct {
	duration time.Duration
}

// NewTimeout 创建请求超时中间件。
// duration <= 0 时中间件不生效（直接放行）。
func NewTimeout(duration time.Duration) *Timeout {
	return &Timeout{duration: duration}
}

// Middleware 返回标准形式 func(http.Handler) http.Handler 的请求超时中间件。
func (t *Timeout) Middleware() func(http.Handler) http.Handler {
	// duration <= 0：不生效，直接透传
	if t.duration <= 0 {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// WebSocket 升级与 SSE 长连接不适用请求超时
			if r.Header.Get("Upgrade") == "websocket" ||
				r.Header.Get("Accept") == "text/event-stream" {
				next.ServeHTTP(w, r)
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), t.duration)
			defer cancel()
			r = r.WithContext(ctx)

			done := make(chan struct{})
			tw := respw.NewTimeoutWriter(w)
			panicChan := make(chan any, 1)
			go func() {
				defer func() {
					if p := recover(); p != nil {
						panicChan <- p
					}
				}()
				next.ServeHTTP(tw, r)
				close(done)
			}()

			select {
			case p := <-panicChan:
				panic(p)
			case <-done:
				// 正常完成：将缓冲的 header/status/body 写到底层 ResponseWriter
				tw.Done()
			case <-ctx.Done():
				if errors.Is(ctx.Err(), context.Canceled) {
					w.WriteHeader(statusClientClosedRequest)
				} else {
					writeError(r.Context(), w, http.StatusServiceUnavailable, "request timeout")
				}
				tw.Timeout()
			}
		})
	}
}

package httpx

import (
	"net/http"
	"runtime/debug"
	"time"

	"github.com/chihqiang/infra-go/logger"
)

// --- CORS 常量 ---

const (
	// corsAllowAllOrigins 允许所有来源的通配符。
	corsAllowAllOrigins = "*"

	// CORS 相关 HTTP 头名称。
	// corsHeaderOrigin Origin 请求/响应头名称，同时用作 Vary 头的值。
	corsHeaderOrigin = "Origin"
	// corsHeaderVary Vary 响应头名称。
	corsHeaderVary = "Vary"
	// corsHeaderAllowOrigin Access-Control-Allow-Origin 响应头名称。
	corsHeaderAllowOrigin = "Access-Control-Allow-Origin"
	// corsHeaderAllowMethods Access-Control-Allow-Methods 响应头名称。
	corsHeaderAllowMethods = "Access-Control-Allow-Methods"
	// corsHeaderAllowHeaders Access-Control-Allow-Headers 响应头名称。
	corsHeaderAllowHeaders = "Access-Control-Allow-Headers"
	// corsHeaderExposeHeaders Access-Control-Expose-Headers 响应头名称。
	corsHeaderExposeHeaders = "Access-Control-Expose-Headers"
	// corsHeaderAllowCredentials Access-Control-Allow-Credentials 响应头名称。
	corsHeaderAllowCredentials = "Access-Control-Allow-Credentials"
	// corsHeaderMaxAge Access-Control-Max-Age 响应头名称。
	corsHeaderMaxAge = "Access-Control-Max-Age"

	// CORS 响应头默认值。
	// corsDefaultAllowMethods 允许的 HTTP 方法列表。
	corsDefaultAllowMethods = "GET, POST, PUT, DELETE, OPTIONS, PATCH"
	// corsDefaultAllowHeaders 允许的请求头列表。
	corsDefaultAllowHeaders = "Content-Type, Authorization, X-Requested-With, Accept, Origin"
	// corsDefaultExposeHeaders 允许暴露给前端 JavaScript 的响应头列表。
	corsDefaultExposeHeaders = "Content-Length, Content-Type"
	// corsDefaultAllowCredentials 是否允许携带凭证（Cookie）。
	corsDefaultAllowCredentials = "true"
	// corsDefaultMaxAge 预检请求缓存时间（秒），86400 = 24 小时。
	corsDefaultMaxAge = "86400"

	// 请求协议 scheme，用于同源判断。
	// corsSchemeHTTP HTTP 协议 scheme。
	corsSchemeHTTP = "http"
	// corsSchemeHTTPS HTTPS 协议 scheme。
	corsSchemeHTTPS = "https"
)

// WithCors 返回一个为响应设置 CORS 头的中间件。
//
// allowOrigins 为允许的来源列表；传入 "*" 表示允许所有来源。
// 同源请求（Origin 与 Host 一致）不设置 CORS 头；
// 未授权来源返回 403；OPTIONS 预检请求返回 204。
func WithCors(allowOrigins ...string) Middleware {
	allowAll := false
	for _, o := range allowOrigins {
		if o == corsAllowAllOrigins {
			allowAll = true
			break
		}
	}

	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get(corsHeaderOrigin)
			if origin == "" {
				next(w, r)
				return
			}

			// 同源请求不需要 CORS 头
			scheme := corsSchemeHTTP
			if r.TLS != nil {
				scheme = corsSchemeHTTPS
			}
			if origin == scheme+"://"+r.Host {
				next(w, r)
				return
			}

			// 校验 Origin
			allowed := allowAll
			if !allowed {
				for _, o := range allowOrigins {
					if o == origin {
						allowed = true
						break
					}
				}
			}
			if !allowed {
				w.WriteHeader(http.StatusForbidden)
				return
			}

			// 设置 CORS 头
			if allowAll {
				w.Header().Set(corsHeaderAllowOrigin, corsAllowAllOrigins)
			} else {
				w.Header().Set(corsHeaderAllowOrigin, origin)
				w.Header().Set(corsHeaderVary, corsHeaderOrigin)
			}
			w.Header().Set(corsHeaderAllowMethods, corsDefaultAllowMethods)
			w.Header().Set(corsHeaderAllowHeaders, corsDefaultAllowHeaders)
			w.Header().Set(corsHeaderExposeHeaders, corsDefaultExposeHeaders)
			w.Header().Set(corsHeaderAllowCredentials, corsDefaultAllowCredentials)
			w.Header().Set(corsHeaderMaxAge, corsDefaultMaxAge)

			// OPTIONS 预检
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next(w, r)
		}
	}
}

// --- 响应记录器 ---

// statusRecorder 包装 http.ResponseWriter，捕获状态码和响应字节数。
// 用于日志中间件记录响应信息。默认状态码为 200（handler 未显式调用 WriteHeader 时）。
type statusRecorder struct {
	http.ResponseWriter
	status    int
	bytes     int
	wroteHead bool
}

func newStatusRecorder(w http.ResponseWriter) *statusRecorder {
	return &statusRecorder{ResponseWriter: w, status: http.StatusOK}
}

// WriteHeader 记录状态码（仅首次有效），并委托给底层 ResponseWriter。
func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHead {
		r.status = code
		r.wroteHead = true
	}
	r.ResponseWriter.WriteHeader(code)
}

// Write 累计写入字节数，并委托给底层 ResponseWriter。
func (r *statusRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// --- Recovery 中间件 ---

// WithRecovery 返回一个 panic 恢复中间件。
// 捕获 handler 中的 panic，记录堆栈并返回 500，防止进程崩溃。
//
//	server.Use(httpx.WithRecovery())
func WithRecovery() Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.ErrorCtx(r.Context(), "panic recovered",
						logger.Any("panic", rec),
						logger.String("method", r.Method),
						logger.String("path", r.URL.Path),
						logger.String("remote", r.RemoteAddr),
						logger.String("stack", string(debug.Stack())),
					)
					WriteHTTPError(w, http.StatusInternalServerError, "internal server error")
				}
			}()
			next(w, r)
		}
	}
}

// --- Logging 中间件 ---

// WithLogger 返回一个请求日志中间件。
// 记录每个请求的方法、路径、状态码、响应字节数和耗时。
// 配合 trace 包使用时，logger 的 Ctx 提取器会自动带上 trace_id/span_id。
//
//	server.Use(httpx.WithLogger())
func WithLogger() Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := newStatusRecorder(w)
			next(rec, r)
			logger.InfoCtx(r.Context(), "http request",
				logger.String("method", r.Method),
				logger.String("path", r.URL.Path),
				logger.String("query", r.URL.RawQuery),
				logger.String("remote", r.RemoteAddr),
				logger.Int("status", rec.status),
				logger.Int("bytes", rec.bytes),
				logger.Duration("latency", time.Since(start)),
			)
		}
	}
}

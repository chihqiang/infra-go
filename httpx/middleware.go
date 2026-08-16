package httpx

import (
	"net/http"
	"runtime/debug"
	"time"

	"github.com/chihqiang/infra-go/logger"
	"github.com/google/uuid"
)

// WithCors 返回一个为响应设置 CORS 头的中间件。
//
// allowOrigins 为允许的来源列表；传入 "*" 表示允许所有来源。
// 同源请求（Origin 与 Host 一致）不设置 CORS 头；
// 未授权来源返回 403；OPTIONS 预检请求返回 204。
func WithCors(allowOrigins ...string) Middleware {
	// 常量仅在 WithCors 内部使用，直接内联。
	const (
		corsAllowAll        = "*"
		hdrOrigin           = "Origin"
		hdrVary             = "Vary"
		hdrAllowOrigin      = "Access-Control-Allow-Origin"
		hdrAllowMethods     = "Access-Control-Allow-Methods"
		hdrAllowHeaders     = "Access-Control-Allow-Headers"
		hdrExposeHeaders    = "Access-Control-Expose-Headers"
		hdrAllowCredentials = "Access-Control-Allow-Credentials"
		hdrMaxAge           = "Access-Control-Max-Age"
		defaultAllowMethods = "GET, POST, PUT, DELETE, OPTIONS, PATCH"
		defaultAllowHeaders = "Content-Type, Authorization, X-Requested-With, Accept, Origin"
		defaultExposeHeader = "Content-Length, Content-Type"
		defaultCredentials  = "true"
		defaultMaxAge       = "86400"
		schemeHTTP          = "http"
		schemeHTTPS         = "https"
	)

	allowAll := false
	// 预构建允许来源集合，避免每次请求 O(n) 扫描
	allowedOrigins := make(map[string]struct{}, len(allowOrigins))
	for _, o := range allowOrigins {
		if o == corsAllowAll {
			allowAll = true
			break
		}
		allowedOrigins[o] = struct{}{}
	}

	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get(hdrOrigin)
			if origin == "" {
				next(w, r)
				return
			}

			// 同源请求不需要 CORS 头
			scheme := schemeHTTP
			if r.TLS != nil {
				scheme = schemeHTTPS
			}
			if origin == scheme+"://"+r.Host {
				next(w, r)
				return
			}

			// 校验 Origin
			if !allowAll {
				if _, ok := allowedOrigins[origin]; !ok {
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}

			// 设置 CORS 头
			if allowAll {
				w.Header().Set(hdrAllowOrigin, corsAllowAll)
			} else {
				w.Header().Set(hdrAllowOrigin, origin)
				w.Header().Set(hdrVary, hdrOrigin)
			}
			w.Header().Set(hdrAllowMethods, defaultAllowMethods)
			w.Header().Set(hdrAllowHeaders, defaultAllowHeaders)
			w.Header().Set(hdrExposeHeaders, defaultExposeHeader)
			w.Header().Set(hdrAllowCredentials, defaultCredentials)
			w.Header().Set(hdrMaxAge, defaultMaxAge)

			// OPTIONS 预检
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next(w, r)
		}
	}
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

// --- Request ID 中间件 ---

// HeaderRequestID 请求 ID 使用的 HTTP Header 名。
const HeaderRequestID = "X-Request-Id"

// WithRequestID 返回一个 request_id 中间件。
// 从 X-Request-Id 请求头读取，不存在则自动生成（google/uuid），
// 注入 context 并回写响应头 X-Request-Id。
//
//	server.Use(httpx.WithRequestID())
//
// 配合 OkJSONCtx / OkXMLCtx / WriteHTTPErrorCtx 使用，request_id 会自动出现在响应中。
func WithRequestID() Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(HeaderRequestID)
			if id == "" {
				id = uuid.NewString()
			}
			w.Header().Set(HeaderRequestID, id)
			next(w, r.WithContext(ContextWithRequestID(r.Context(), id)))
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

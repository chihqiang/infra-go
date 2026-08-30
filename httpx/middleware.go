package httpx

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/chihqiang/infra-go/breaker"
	"github.com/chihqiang/infra-go/hash"
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

// --- 熔断中间件 ---

// WithBreaker 返回一个熔断中间件，保护下游 handler 不被级联拖垮。
// 基于 breaker 模块的 Google SRE 算法，熔断器以 "METHOD://path" 为名，按路由隔离。
//
// 熔断打开时返回 503 Service Unavailable；请求成功（<500）上报 Accept，
// 请求失败（>=500）上报 Reject，用于驱动熔断状态。
//
//	server.Use(httpx.WithBreaker())
func WithBreaker() Middleware {
	// 熔断器在构造时创建一次（按路由共享，通过中间件闭包持有）。
	// 注意：这里按全局单一熔断器实现；如需按路由隔离，
	// 请在使用时配合 Go 1.22 的 path 参数，在 WithRouteBreaker 中按路由创建。
	brk := breaker.NewBreaker(breaker.WithName("http"))
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			promise, err := brk.AllowCtx(r.Context())
			if err != nil {
				logger.WarnCtx(r.Context(), "http request dropped by breaker",
					logger.String("path", r.URL.Path),
					logger.String("remote", r.RemoteAddr),
					logger.Err(err),
				)
				WriteHTTPErrorCtx(r.Context(), w, http.StatusServiceUnavailable, "service unavailable")
				return
			}

			rec := newStatusRecorder(w)
			next(rec, r)
			if rec.status < http.StatusInternalServerError {
				promise.Accept()
			} else {
				promise.Reject(fmt.Sprintf("%d %s", rec.status, http.StatusText(rec.status)))
			}
		}
	}
}

// WithRouteBreaker 返回一个按路由隔离的熔断中间件。
// 每个路由（METHOD:path）拥有独立的熔断器，统计互不影响，
// 避免单个路由的失败拉低其他路由的通过率。
//
// 熔断器通过 breaker.GetBreaker 按名称缓存，同名路由共享同一实例。
// 熔断打开时返回 503 Service Unavailable；请求成功（<500）上报 Accept，
// 请求失败（>=500）上报 Reject。
//
//	server.Use(httpx.WithRouteBreaker())
func WithRouteBreaker() Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// 以 METHOD:path 作为熔断器名称，按路由隔离
			name := r.Method + ":" + r.URL.Path
			brk := breaker.GetBreaker(name)

			promise, err := brk.AllowCtx(r.Context())
			if err != nil {
				logger.WarnCtx(r.Context(), "http request dropped by route breaker",
					logger.String("breaker", name),
					logger.String("path", r.URL.Path),
					logger.String("remote", r.RemoteAddr),
					logger.Err(err),
				)
				WriteHTTPErrorCtx(r.Context(), w, http.StatusServiceUnavailable, "service unavailable")
				return
			}

			rec := newStatusRecorder(w)
			next(rec, r)
			if rec.status < http.StatusInternalServerError {
				promise.Accept()
			} else {
				promise.Reject(fmt.Sprintf("%d %s", rec.status, http.StatusText(rec.status)))
			}
		}
	}
}

// --- 请求超时中间件 ---

// statusClientClosedRequest 客户端主动关闭请求（非标准状态码 499，nginx 约定）。
const statusClientClosedRequest = 499

// WithTimeout 返回一个请求超时中间件。
// 每个请求最多执行 duration，超时返回 503 Service Unavailable。
// 客户端主动断开返回 499；WebSocket / SSE 请求不受超时限制。
//
//	duration <= 0 时中间件不生效（直接放行）。
//
//	server.Use(httpx.WithTimeout(5 * time.Second))
func WithTimeout(duration time.Duration) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		if duration <= 0 {
			return next
		}

		return func(w http.ResponseWriter, r *http.Request) {
			// WebSocket 升级与 SSE 长连接不适用请求超时
			if r.Header.Get("Upgrade") == "websocket" ||
				r.Header.Get("Accept") == "text/event-stream" {
				next(w, r)
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), duration)
			defer cancel()
			r = r.WithContext(ctx)

			done := make(chan struct{})
			tw := &timeoutWriter{
				w:    w,
				h:    make(http.Header),
				code: http.StatusOK,
			}
			panicChan := make(chan any, 1)
			go func() {
				defer func() {
					if p := recover(); p != nil {
						panicChan <- p
					}
				}()
				next(tw, r)
				close(done)
			}()

			select {
			case p := <-panicChan:
				panic(p)
			case <-done:
				tw.mu.Lock()
				defer tw.mu.Unlock()
				dst := w.Header()
				for k, vv := range tw.h {
					dst[k] = vv
				}
				if tw.code != http.StatusOK {
					w.WriteHeader(tw.code)
				}
				_, _ = w.Write(tw.wbuf.Bytes())
			case <-ctx.Done():
				tw.mu.Lock()
				defer tw.mu.Unlock()
				if errors.Is(ctx.Err(), context.Canceled) {
					w.WriteHeader(statusClientClosedRequest)
				} else {
					WriteHTTPErrorCtx(r.Context(), w, http.StatusServiceUnavailable, "request timeout")
				}
				tw.timedOut = true
			}
		}
	}
}

// timeoutWriter 已移至 responsewriter.go。

// --- 请求体大小限制中间件 ---

// WithMaxBytes 返回一个限制请求体大小的中间件。
// 请求体 Content-Length 超过 n 字节时直接返回 413 Request Entity Too Large。
// 对分块传输（无 Content-Length）的请求，用 http.MaxBytesReader 在读取时限制。
//
//	n <= 0 表示不限制。
//
//	server.Use(httpx.WithMaxBytes(1 << 20)) // 限制 1MB
func WithMaxBytes(n int64) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		if n <= 0 {
			return next
		}

		return func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > n {
				logger.WarnCtx(r.Context(), "request body too large",
					logger.Int64("limit", n),
					logger.Int64("content_length", r.ContentLength),
					logger.String("path", r.URL.Path),
				)
				WriteHTTPErrorCtx(r.Context(), w, http.StatusRequestEntityTooLarge, "request entity too large")
				return
			}

			// 限制读取时的最大字节数（覆盖分块传输场景）
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next(w, r)
		}
	}
}

// --- gzip 请求体解压中间件 ---

// WithGunzip 返回一个自动解压 gzip 请求体的中间件。
// 请求头 Content-Encoding 含 "gzip" 时，将请求体包装为 gzip 读取器。
// 解压失败返回 400 Bad Request。
//
//	server.Use(httpx.WithGunzip())
func WithGunzip() Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.Header.Get("Content-Encoding"), "gzip") {
				reader, err := gzip.NewReader(r.Body)
				if err != nil {
					WriteHTTPErrorCtx(r.Context(), w, http.StatusBadRequest, "invalid gzip body")
					return
				}
				defer reader.Close()
				r.Body = reader
			}
			next(w, r)
		}
	}
}

// --- 并发连接数限制中间件 ---

// WithMaxConns 返回一个限制同时处理请求数的中间件。
// 并发数超过 n 时直接返回 503 Service Unavailable，防止连接耗尽。
//
//	n <= 0 表示不限制。
//
//	server.Use(httpx.WithMaxConns(1000))
func WithMaxConns(n int) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		if n <= 0 {
			return next
		}

		// 基于带缓冲 channel 的轻量信号量，仅用于并发计数（不需要 Wait 语义）
		sem := make(chan struct{}, n)
		return func(w http.ResponseWriter, r *http.Request) {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				next(w, r)
			default:
				logger.WarnCtx(r.Context(), "too many concurrent connections",
					logger.Int("limit", n),
					logger.String("path", r.URL.Path),
				)
				WriteHTTPErrorCtx(r.Context(), w, http.StatusServiceUnavailable, "too many concurrent connections")
			}
		}
	}
}

// --- 请求/响应加密中间件 ---

// maxEncryptedResponseBytes 限制加密响应的最大缓冲大小（默认 1MB）。
// 超过此大小的响应将回退为明文输出，避免大响应导致内存暴涨。
const maxEncryptedResponseBytes = 1 << 20 // 1 MB

// WithCryption 返回一个 AES-GCM 请求/响应加密中间件。
// 请求体需为 base64 编码的 AES-GCM 密文（nonce || ciphertext），
// 中间件解密后交给 handler；handler 写入的响应会被加密后返回给客户端。
//
// 采用 AES-GCM 认证加密（AEAD），同时保证机密性与完整性（防篡改），
// nonce 每次随机生成；相比常见的 AES-ECB 等非认证模式，GCM 能抵御
// 篡改与重放，安全性更高。
//
// 响应超过 1MB 时自动回退为明文输出（不加密），避免大响应导致 OOM。
//
// 密钥 key 长度必须为 16/24/32 字节（对应 AES-128/192/256）。
//
//	server.Use(httpx.WithCryption([]byte("0123456789abcdef")))
func WithCryption(key []byte) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// 解密请求体
			if r.ContentLength > 0 {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					WriteHTTPErrorCtx(r.Context(), w, http.StatusBadRequest, "failed to read request body")
					return
				}
				plain, err := hash.AESGCMDecrypt(key, string(body))
				if err != nil {
					logger.WarnCtx(r.Context(), "decrypt request body failed",
						logger.String("path", r.URL.Path),
						logger.Err(err),
					)
					WriteHTTPErrorCtx(r.Context(), w, http.StatusBadRequest, "invalid encrypted body")
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(plain))
				r.ContentLength = int64(len(plain))
			}

			// 加密响应
			cw := &cryptionResponseWriter{
				ResponseWriter: w,
				maxBufBytes:    maxEncryptedResponseBytes,
			}
			next(cw, r)

			// 缓冲超限：回退为明文输出，不加密
			if cw.overflowed {
				logger.WarnCtx(r.Context(), "encrypted response exceeds max buffer, falling back to plaintext",
					logger.String("path", r.URL.Path),
					logger.Int("max_bytes", maxEncryptedResponseBytes),
				)
				if cw.code != 0 {
					w.WriteHeader(cw.code)
				}
				_, _ = w.Write(cw.buf.Bytes())
				return
			}

			// 写入加密后的响应
			encrypted, err := hash.AESGCMEncrypt(key, cw.buf.Bytes())
			if err != nil {
				logger.ErrorCtx(r.Context(), "encrypt response failed", logger.Err(err))
				return
			}
			_, _ = w.Write([]byte(encrypted))
		}
	}
}

// cryptionResponseWriter 已移至 responsewriter.go。

// --- 内容安全校验中间件 ---

// ContentSecurityHeader 内容安全请求头 `X-Content-Security` 的字段名。
const ContentSecurityHeader = "X-Content-Security"

// WithContentSecurity 返回一个内容安全校验中间件（防篡改 + 防重放）。
// 客户端需在 `X-Content-Security` 头携带签名：
//
//	X-Content-Security: time=<unix秒>; signature=<base64 HMAC-SHA256>
//
// 签名内容为：`timestamp\nmethod\npath\nquery\nbodySha256Hex`
// （timestamp 为请求头中的时间戳，bodySha256Hex 为请求体的 SHA-256 十六进制摘要）。
//
// 校验规则：
//   - 签名有效（HMAC-SHA256 匹配）且时间戳在 tolerance 容差内 → 放行
//   - 签名无效 → 返回 401
//   - 时间戳超出容差（防重放）→ 返回 403
//
// key 为双方共享的 HMAC 密钥。
//
//	server.Use(httpx.WithContentSecurity([]byte("shared-secret"), 5*time.Minute))
func WithContentSecurity(key []byte, tolerance time.Duration) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// 解析 X-Content-Security 头，提取 timestamp 与 signature
			header := r.Header.Get(ContentSecurityHeader)
			timestamp, signature := "", ""
			for _, part := range strings.Split(header, ";") {
				kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
				if len(kv) != 2 {
					continue
				}
				switch strings.TrimSpace(kv[0]) {
				case "time":
					timestamp = strings.TrimSpace(kv[1])
				case "signature":
					signature = strings.TrimSpace(kv[1])
				}
			}
			if timestamp == "" || signature == "" {
				WriteHTTPErrorCtx(r.Context(), w, http.StatusUnauthorized, "invalid content security header")
				return
			}

			// 防重放：时间戳超出容差则拒绝
			ts, err := strconv.ParseInt(timestamp, 10, 64)
			if err != nil {
				WriteHTTPErrorCtx(r.Context(), w, http.StatusUnauthorized, "invalid timestamp")
				return
			}
			now := time.Now().Unix()
			tol := int64(tolerance.Seconds())
			if ts+tol < now || now+tol < ts {
				WriteHTTPErrorCtx(r.Context(), w, http.StatusForbidden, "request expired")
				return
			}

			// 计算被签名内容：timestamp\nmethod\npath\nquery\nbodySha256Hex
			body := ""
			if r.Body != nil {
				if b, err := io.ReadAll(r.Body); err == nil {
					sum := sha256.Sum256(b)
					body = hex.EncodeToString(sum[:])
					// 恢复 body 供下游 handler 读取
					r.Body = io.NopCloser(bytes.NewReader(b))
				}
			}
			signContent := strings.Join([]string{
				timestamp,
				r.Method,
				r.URL.Path,
				r.URL.RawQuery,
				body,
			}, "\n")

			// 校验签名
			if !hash.HMACVerify(key, signContent, signature) {
				WriteHTTPErrorCtx(r.Context(), w, http.StatusUnauthorized, "invalid signature")
				return
			}

			next(w, r)
		}
	}
}

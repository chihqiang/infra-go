package httpx

import (
	"context"
	"net/http"
	"time"

	"github.com/chihqiang/infra-go/httpx/middleware"
	"github.com/chihqiang/infra-go/jwt"
)

// 本文件提供内置 HTTP 中间件的 httpx 适配层（转发 httpx/middleware 子包）。
// 核心逻辑统一在 httpx/middleware 子包实现：一个中间件一个文件、面向对象
// （NewXxx + Middleware()），返回标准形式 func(http.Handler) http.Handler，
// 不依赖 httpx，可被 gin / echo 等其它 net/http 兼容框架复用。
//
// 本文件仅把标准中间件适配为 httpx.Middleware（func(http.HandlerFunc) http.HandlerFunc）
// 供 server.Use / WithMiddleware / ApplyMiddleware 注册，方法签名保持不变。
//
// 中间件清单：CORS（WithCors）、Recovery、RequestID、链路追踪（WithTracing）、访问日志、
// 熔断（全局/按路由）、超时、请求体大小限制、gzip 解压、并发连接数限制、限流（WithRateLimit）、
// JWT 认证（WithJWT，转发 jwt.AuthMiddleware）、请求/响应加密、内容安全校验。

func init() {
	// 注入统一 JSON 错误响应（携带 request_id），使 httpx 注册的中间件
	// 错误响应保持原有格式；middleware 子包在其它框架中可用 SetErrorHandler
	// 自行注入框架的错误渲染。
	middleware.SetErrorHandler(func(ctx context.Context, w http.ResponseWriter, status int, msg string) {
		WriteHTTPErrorCtx(ctx, w, status, msg)
	})
}

// AsMiddleware 将标准形式的中间件适配为 httpx.Middleware，方便快速把
// 任意 func(http.Handler) http.Handler 中间件（如 httpx/middleware 子包的
// NewXxx().Middleware()，或其它基于 net/http 的第三方中间件）注册到 server。
//
//	// 自定义/第三方标准中间件 → httpx 中间件
//	server.Use(httpx.AsMiddleware(myStdMiddleware))
//
//	// 使用 httpx/middleware 子包（OO 形态）时亦可通过本函数接入：
//	server.Use(httpx.AsMiddleware(middleware.NewCORS("*").Middleware()))
func AsMiddleware(mw func(http.Handler) http.Handler) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return mw(http.HandlerFunc(next)).ServeHTTP
	}
}

// WithCors 返回一个为响应设置 CORS 头的中间件。
//
// allowOrigins 为允许的来源列表；传入 "*" 表示允许所有来源。
// 同源请求（Origin 与 Host 一致）不设置 CORS 头；
// 未授权来源返回 403；OPTIONS 预检请求返回 204。
//
//	server.Use(httpx.WithCors("*"))
//	server.Use(httpx.WithCors("http://a.com", "http://b.com"))
func WithCors(allowOrigins ...string) Middleware {
	return AsMiddleware(middleware.NewCORS(allowOrigins...).Middleware())
}

// WithRecovery 返回一个 panic 恢复中间件。
// 捕获 handler 中的 panic，记录堆栈并返回 500，防止进程崩溃。
//
//	server.Use(httpx.WithRecovery())
func WithRecovery() Middleware {
	return AsMiddleware(middleware.NewRecovery().Middleware())
}

// WithRequestID 返回一个 request_id 中间件。
// 从 X-Request-Id 请求头读取，不存在则自动生成（google/uuid），
// 注入 context 并回写响应头 X-Request-Id。
//
//	server.Use(httpx.WithRequestID())
//
// 配合 OkJSONCtx / OkXMLCtx / WriteHTTPErrorCtx 使用，request_id 会自动出现在响应中。
func WithRequestID() Middleware {
	return AsMiddleware(middleware.NewRequestID().Middleware())
}

// WithTracing 返回一个 HTTP 服务端链路追踪中间件（转发 middleware.NewTracing）。
//
// 功能：
//   - 从请求头提取上游传播的 span 上下文（W3C traceparent）
//   - 为每个请求创建服务端 span（携带 method/path/status 等 HTTP 语义属性）
//   - 将 span 注入 context，供下游 logger/orm/redisx 等模块自动关联 trace_id
//
// 默认使用全局 TracerProvider（经 trace.StartAgent 装配）；请求 context 中已有
// 有效 span 时沿用其 TracerProvider（支持嵌套追踪）。
//
// ignorePaths 为不追踪的路径列表（健康检查、探针等），命中直接放行、不创建 span。
// 匹配方式与 WithLogger 一致：精确匹配（如 "/health"）或以 "*" 结尾的前缀通配
// （如 "/health*" 命中 /health、/healthz、/health/live）。
//
//	server.Use(httpx.WithTracing())                    // 追踪所有请求
//	server.Use(httpx.WithTracing("/health*", "/metrics/*")) // 跳过探活
func WithTracing(ignorePaths ...string) Middleware {
	return AsMiddleware(middleware.NewTracing(ignorePaths...).Middleware())
}

// WithLogger 返回一个请求日志中间件。
// 记录每个请求的方法、路径、状态码、响应字节数和耗时。
// 配合 trace 包使用时，logger 的 Ctx 提取器会自动带上 trace_id/span_id。
//
// skipPaths 为不记录日志的路径列表，常用于健康检查、心跳等高频探活接口。
// 支持两种匹配方式：
//
//   - 精确匹配：如 "/healthz"，仅命中该路径；
//
//   - 前缀通配：以 "*" 结尾，如 "/internal/*"，命中以该前缀开头的所有路径。
//
//     server.Use(httpx.WithLogger())                       // 记录所有请求
//     server.Use(httpx.WithLogger("/healthz", "/metrics")) // 精确跳过
//     server.Use(httpx.WithLogger("/internal/*"))          // 前缀通配跳过
func WithLogger(skipPaths ...string) Middleware {
	return AsMiddleware(middleware.NewAccessLogger(skipPaths...).Middleware())
}

// WithBreaker 返回一个熔断中间件，保护下游 handler 不被级联拖垮。
// 基于 breaker 模块的 Google SRE 算法，所有请求共享同一熔断器实例；
// 如需按路由隔离请使用 WithRouteBreaker。
//
// 熔断打开时返回 503 Service Unavailable；请求成功（<500）上报 Accept，
// 请求失败（>=500）上报 Reject，用于驱动熔断状态。
//
//	server.Use(httpx.WithBreaker())
func WithBreaker() Middleware {
	return AsMiddleware(middleware.NewBreaker().Middleware())
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
	return AsMiddleware(middleware.NewRouteBreaker().Middleware())
}

// WithTimeout 返回一个请求超时中间件。
// 每个请求最多执行 duration，超时返回 503 Service Unavailable。
// 客户端主动断开返回 499；WebSocket / SSE 请求不受超时限制。
//
//	duration <= 0 时中间件不生效（直接放行）。
//
//	server.Use(httpx.WithTimeout(5 * time.Second))
func WithTimeout(duration time.Duration) Middleware {
	return AsMiddleware(middleware.NewTimeout(duration).Middleware())
}

// WithMaxBytes 返回一个限制请求体大小的中间件。
// 请求体 Content-Length 超过 n 字节时直接返回 413 Request Entity Too Large。
// 对分块传输（无 Content-Length）的请求，用 http.MaxBytesReader 在读取时限制。
//
//	n <= 0 表示不限制。
//
//	server.Use(httpx.WithMaxBytes(1 << 20)) // 限制 1MB
func WithMaxBytes(n int64) Middleware {
	return AsMiddleware(middleware.NewMaxBytes(n).Middleware())
}

// WithGunzip 返回一个自动解压 gzip 请求体的中间件。
// 请求头 Content-Encoding 含 "gzip" 时，将请求体包装为 gzip 读取器。
// 解压失败返回 400 Bad Request。
//
//	server.Use(httpx.WithGunzip())
func WithGunzip() Middleware {
	return AsMiddleware(middleware.NewGunzip().Middleware())
}

// WithMaxConns 返回一个限制同时处理请求数的中间件。
// 并发数超过 n 时直接返回 503 Service Unavailable，防止连接耗尽。
//
//	n <= 0 表示不限制。
//
//	server.Use(httpx.WithMaxConns(1000))
func WithMaxConns(n int) Middleware {
	return AsMiddleware(middleware.NewMaxConns(n).Middleware())
}

// WithRateLimit 返回一个基于限流器的 HTTP 限流中间件（转发 middleware.NewRateLimit）。
//
// 每个请求先向限流器申请配额，允许则放行；被限流返回 429 Too Many Requests。
// limiter 由 ratelimit 包提供（ratelimit.NewTokenBucket / NewSlidingWindow / Redis 限流器等），
// 方法集与 middleware.RateLimiter 一致，可直接传入；nil 时降级为不限流（fail-open）。
//
// skipPaths 为不参与限流的路径列表，命中直接放行（常用于健康检查等高频探活接口）。
// 匹配方式与 WithLogger 一致：精确匹配（如 "/healthz"）或以 "*" 结尾的前缀通配
// （如 "/internal/*"）。
//
//	server.Use(httpx.WithRateLimit(ratelimit.NewTokenBucket(100, 200)))          // 每秒 100 次、突发 200
//	server.Use(httpx.WithRateLimit(ratelimit.NewSlidingWindow(10, time.Minute))) // 每分钟 10 次
//	server.Use(httpx.WithRateLimit(redisLimiter, "/healthz", "/metrics"))      // 跳过探活接口
func WithRateLimit(limiter middleware.RateLimiter, skipPaths ...string) Middleware {
	return AsMiddleware(middleware.NewRateLimit(limiter, skipPaths...).Middleware())
}

// WithCryption 返回一个 AES-GCM 请求/响应加密中间件。
// 请求体需为 base64 编码的 AES-GCM 密文（nonce || ciphertext），
// 中间件解密后交给 handler；handler 写入的响应会被加密后返回给客户端。
//
// 采用 AES-GCM 认证加密（AEAD），同时保证机密性与完整性（防篡改），
// nonce 每次随机生成；响应超过 1MB 时自动回退为明文输出（不加密），
// 避免大响应导致 OOM。密钥 key 长度必须为 16/24/32 字节（对应 AES-128/192/256）。
//
// skipPaths 为不进行请求/响应加解密的路径列表，命中路径以明文透传
// （常用于回调、静态资源等无法加密的场景）。匹配方式与 WithLogger 一致：
// 精确匹配（如 "/callback"）或以 "*" 结尾的前缀通配（如 "/public/*"）。
//
//	server.Use(httpx.WithCryption([]byte("0123456789abcdef")))               // 全部加解密
//	server.Use(httpx.WithCryption([]byte("0123456789abcdef"), "/callback"))  // 精确跳过
//	server.Use(httpx.WithCryption([]byte("0123456789abcdef"), "/public/*"))  // 前缀通配跳过
func WithCryption(key []byte, skipPaths ...string) Middleware {
	return AsMiddleware(middleware.NewCryption(key, skipPaths...).Middleware())
}

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
	return AsMiddleware(middleware.NewContentSecurity(key, tolerance).Middleware())
}

// WithJWT 返回一个基于 JWT 的 HTTP 认证中间件（转发 jwt.JWT.AuthMiddleware）。
//
// j 为 *jwt.JWT；getToken 由调用方提供，从请求中提取 token（如从 Header/Cookie/Query），
// 中间件只负责解析验证与注入业务 claims，不关心 token 来源。
// 验证失败返回 401；成功时将业务 claims（排除标准声明与 token_type）注入 context，
// 下游 handler 通过 jwt.ClaimsFromContext 读取。
// 错误响应经 httpx/middleware 统一错误机制输出（即本包统一 JSON 响应）。
//
//	j := jwt.MustNew(jwt.Config{Secret: "..."})
//	server.Use(httpx.WithJWT(j, func(r *http.Request) string {
//	    return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
//	}))
func WithJWT(j *jwt.JWT, getToken func(*http.Request) string) Middleware {
	return j.AuthMiddleware(getToken)
}

package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/chihqiang/infra-go/breaker"
	"github.com/chihqiang/infra-go/httpx/respw"
	"github.com/chihqiang/infra-go/httpx/x"
	"github.com/chihqiang/infra-go/logger"
)

// RouteBreaker 是按路由隔离的熔断中间件。
// 每个路由（METHOD:pattern）拥有独立的熔断器，统计互不影响，
// 避免单个路由的失败拉低其他路由的通过率。
type RouteBreaker struct {
	// retryAfter 为熔断打开时提示客户端的重试间隔，默认 breakerRetryAfter。
	retryAfter time.Duration
}

// NewRouteBreaker 创建按路由隔离的熔断中间件。
func NewRouteBreaker() *RouteBreaker {
	return &RouteBreaker{retryAfter: breakerRetryAfter}
}

// WithRetryAfter 设置熔断打开时的 Retry-After 提示间隔（RFC 9110 §10.2.3）。
// d <= 0 表示不发送该头。
func (b *RouteBreaker) WithRetryAfter(d time.Duration) *RouteBreaker {
	b.retryAfter = d
	return b
}

// breakerName 返回该请求对应的熔断器名称。
//
// **必须使用路由模板而不是具体路径**：熔断器由 breaker.GetBreaker 永久缓存，
// 若用 r.URL.Path（/users/1、/users/2 …），每个不同的路径参数都会创建一个新熔断器：
//
//   - 「按路由隔离」退化为「按请求隔离」，熔断统计失去意义；
//   - 内存无界增长（攻击者构造大量不同路径即可耗尽内存）。
//
// 取值优先级：
//  1. r.Pattern：net/http 在把请求分发到命中 handler 时填充（Go 1.23+）。
//     中间件若注册在路由级别（mux 内层）可直接拿到。
//  2. context 中的模板：全局中间件位于 mux 外层，此时 r.Pattern 尚为空，
//     httpx.Server 会预判路由并写入 context（见 PatternFromContext）。
//  3. 归一化路径：其它框架 / 未预判的场景，把像标识符的段替换为占位符，
//     保证名称基数有界。
func breakerName(r *http.Request) string {
	if r.Pattern != "" {
		return r.Pattern
	}
	if pattern := PatternFromContext(r.Context()); pattern != "" {
		return pattern
	}
	return r.Method + ":" + normalizePathPattern(r.URL.Path)
}

// pathIDPlaceholder 归一化路径中"参数段"的占位符。
const pathIDPlaceholder = "{}"

// maxPatternSegments 归一化路径时保留的最大段数，避免超长路径放大名称长度。
const maxPatternSegments = 16

// normalizePathPattern 将具体路径归一化为模板形式，使基数有界。
//
// 判定为"参数段"的规则：
//   - 全数字（/users/123）
//   - 长十六进制（/files/a1b2c3d4…，常见于 hash/UUID 去掉连字符）
//   - 含连字符且长度 >= 32 的 UUID 形式
//   - 长度 >= 16 的其它长串（保守起见视为标识符）
//
// 例：/users/123/orders/456 → /users/{}/orders/{}
func normalizePathPattern(p string) string {
	if p == "" {
		return "/"
	}

	segments := strings.Split(p, "/")
	if len(segments) > maxPatternSegments {
		segments = segments[:maxPatternSegments]
	}

	for i, seg := range segments {
		if isIdentifierSegment(seg) {
			segments[i] = pathIDPlaceholder
		}
	}
	return strings.Join(segments, "/")
}

// isIdentifierSegment 判断路径段是否像"标识符"（而非固定的路由片段）。
func isIdentifierSegment(seg string) bool {
	switch len(seg) {
	case 0:
		// 空段来自开头/结尾的 "/" 或连续斜杠，保留原样
		return false
	case 1:
		// 单个数字视为标识符（/users/0）；单个字母视为固定路由片段（/v/a 等）保留。
		return seg[0] >= '0' && seg[0] <= '9'
	}

	allDigits := true
	allHex := true
	for _, c := range seg {
		switch {
		case c >= '0' && c <= '9':
			// 数字同时满足两者
		case (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'):
			allDigits = false
		case c == '-' || c == '_':
			// 分隔符：不再是纯数字，也不再是纯 hex
			allDigits = false
			allHex = false
		default:
			allDigits = false
			allHex = false
		}
	}

	return allDigits || (allHex && len(seg) >= 8) || len(seg) >= 16
}

// Middleware 返回标准形式 func(http.Handler) http.Handler 的按路由熔断中间件。
// 熔断器通过 breaker.GetBreaker 按名称缓存，同一路由模板共享同一实例。
// 熔断打开时返回 503 Service Unavailable（并带 Retry-After）；
// 请求成功（<500）上报 Accept，请求失败（>=500）上报 Reject。
func (b *RouteBreaker) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			name := breakerName(r)
			brk := breaker.GetBreaker(name)

			promise, err := brk.AllowCtx(r.Context())
			if err != nil {
				logger.WarnCtx(r.Context(), "http request dropped by route breaker",
					logger.String("breaker", name),
					logger.String("path", r.URL.Path),
					logger.String("remote", x.ClientIP(r)),
					logger.Err(err),
				)
				// RFC 9110 §15.6.4：因故障/过载返回 503 时 SHOULD 给出 Retry-After
				WriteRetryAfter(r.Context(), w, http.StatusServiceUnavailable,
					b.retryAfter, "service unavailable")
				return
			}

			rec := respw.NewRecorderWriter(w)
			next.ServeHTTP(rec, r)
			if rec.Status() < http.StatusInternalServerError {
				promise.Accept()
			} else {
				promise.Reject(fmt.Sprintf("%d %s", rec.Status(), http.StatusText(rec.Status())))
			}
		})
	}
}

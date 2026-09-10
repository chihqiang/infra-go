package middleware

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// 与状态码语义相关的标准响应头名称。
const (
	// HeaderWWWAuthenticate 401 响应必须携带的认证质询头（RFC 9110 §11.3）。
	HeaderWWWAuthenticate = "WWW-Authenticate"
	// HeaderRetryAfter 429/503 建议携带的重试提示头（RFC 9110 §10.2.3）。
	HeaderRetryAfter = "Retry-After"
)

// RFC 6750 §3 定义的 Bearer challenge error 取值。
const (
	// BearerErrorInvalidRequest 请求缺少必要的认证信息。
	BearerErrorInvalidRequest = "invalid_request"
	// BearerErrorInvalidToken 令牌无效、过期或已被撤销。
	BearerErrorInvalidToken = "invalid_token"
	// BearerErrorInsufficientScope 令牌有效但权限不足。
	BearerErrorInsufficientScope = "insufficient_scope"
)

// Challenge 描述 401 响应中告知客户端的认证方案（RFC 9110 §11.3.1）。
//
// 形式上为 `scheme 1*SP #auth-param`，例如：
//
//	Bearer realm="api", error="invalid_token"
type Challenge struct {
	// Scheme 认证方案名，如 "Bearer"（RFC 6750）。
	Scheme string
	// Realm 保护空间标识，可选。
	Realm string
	// Error 失败原因，取值见 RFC 6750 §3；仅 Bearer 方案定义，其它方案留空。
	Error string
}

// String 渲染为 WWW-Authenticate 头值。
// Scheme 为空时返回空字符串（调用方据此跳过设置该头）。
// auth-param 的值按 RFC 9110 §11.2 使用 quoted-string。
func (c Challenge) String() string {
	scheme := strings.TrimSpace(c.Scheme)
	if scheme == "" {
		return ""
	}

	params := make([]string, 0, 2)
	if c.Realm != "" {
		params = append(params, `realm="`+quoteHeaderValue(c.Realm)+`"`)
	}
	if c.Error != "" {
		params = append(params, `error="`+quoteHeaderValue(c.Error)+`"`)
	}
	if len(params) == 0 {
		return scheme
	}
	return scheme + " " + strings.Join(params, ", ")
}

// quoteHeaderValue 转义 quoted-string 中不允许直接出现的字符。
// 只保留可见 ASCII 与空格，其余字符去掉，避免头注入（CR/LF）与非法值。
func quoteHeaderValue(v string) string {
	var b strings.Builder
	for _, r := range v {
		switch {
		case r == '"' || r == '\\':
			// quoted-string 中的引号与反斜杠需要反斜杠转义
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			// 控制字符（含 CR/LF）直接丢弃，防止响应头注入
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// WriteUnauthorized 写入 401 响应，并在需要时附带 WWW-Authenticate 头。
//
// RFC 9110 §15.5.2 规定：生成 401 响应的服务器 **MUST** 发送
// WWW-Authenticate 头，且其中至少包含一个适用于目标资源的质询。
// 缺少该头时，遵循规范的客户端与网关无法得知应采用哪种认证方案，
// 因此认证类中间件应统一经本函数输出 401。
func WriteUnauthorized(ctx context.Context, w http.ResponseWriter, c Challenge, msg string) {
	if v := c.String(); v != "" {
		w.Header().Set(HeaderWWWAuthenticate, v)
	}
	writeError(ctx, w, http.StatusUnauthorized, msg)
}

// WriteRetryAfter 写入 429 / 503 响应，并在能给出建议时附带 Retry-After 头。
//
// RFC 9110 §10.2.3 规定 Retry-After 取值可以是 delay-seconds（非负十进制整数）
// 或 HTTP-date；这里统一使用 delay-seconds，它更简单且不受双方时钟偏差影响。
//
// RFC 6585 §4 对 429 的措辞是 MAY，RFC 9110 §15.6.4 对 503 的措辞是 SHOULD；
// 两者都依赖该头来指导客户端退避。d <= 0 时不发送该头：
// 与其给出编造的等待时长（客户端可能据此长时间不重试），不如省略。
func WriteRetryAfter(ctx context.Context, w http.ResponseWriter, status int, d time.Duration, msg string) {
	if secs := RetryAfterSeconds(d); secs > 0 {
		w.Header().Set(HeaderRetryAfter, strconv.Itoa(secs))
	}
	writeError(ctx, w, status, msg)
}

// RetryAfterSeconds 将建议等待时长取整为 Retry-After 所需的秒数。
//
// 向上取整：宁可让客户端多等一点，也不要早于限流窗口/熔断冷却结束就重试。
// 不足 1 秒也返回 1，避免出现 "Retry-After: 0"（会被解读为可立即重试）。
// d <= 0 返回 0，表示不应发送该头。
func RetryAfterSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	secs := int(math.Ceil(d.Seconds()))
	if secs < 1 {
		return 1
	}
	return secs
}

// RetryAfterProvider 由能够给出建议重试间隔的限流器实现（可选）。
//
// 限流中间件在拒绝请求时会断言该接口，据此生成精确的 Retry-After：
// 例如令牌桶可算出"距下一个令牌可用还有多久"，
// 滑动窗口可算出"最早一次请求何时滑出窗口"。
//
// 无法给出有意义估计的实现（如并发数限流）不必实现本接口，
// 此时中间件会退回配置项 RetryAfter，仍为 0 则省略该头。
type RetryAfterProvider interface {
	RetryAfter() time.Duration
}

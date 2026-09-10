package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chihqiang/infra-go/hash"
)

// ContentSecurityHeader 内容安全请求头 `X-Content-Security` 的字段名。
const ContentSecurityHeader = "X-Content-Security"

// defaultContentSecurityMaxBytes 默认参与签名的请求体上限（5MB，与 Cryption 一致）。
const defaultContentSecurityMaxBytes = 5 << 20

// contentSecurityScheme 是本中间件在 WWW-Authenticate 中声明的认证方案名。
// 自定义 HMAC 签名不是 Basic/Bearer，需用自有 scheme 名（RFC 9110 §11.3）。
const contentSecurityScheme = "ContentSecurity"

// ContentSecurity 是内容安全校验中间件（防篡改 + 防重放）。
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
//   - 请求体读取失败 → 返回 400（不得当作空体继续校验）
//   - 请求体超过 maxBodyBytes → 返回 413
//
// 安全说明：请求体必须完整参与签名，因此本中间件需要把整个主体读入内存。
// maxBodyBytes 限制了这个开销，避免超大请求体耗尽内存。
type ContentSecurity struct {
	key          []byte
	tolerance    time.Duration
	maxBodyBytes int64
}

// NewContentSecurity 创建内容安全校验中间件。
// key 为双方共享的 HMAC 密钥。
// 请求体上限默认为 5MB，可用 WithMaxBodyBytes 调整。
func NewContentSecurity(key []byte, tolerance time.Duration) *ContentSecurity {
	return &ContentSecurity{
		key:          key,
		tolerance:    tolerance,
		maxBodyBytes: defaultContentSecurityMaxBytes,
	}
}

// WithMaxBodyBytes 设置参与签名的请求体上限（字节）。
// n <= 0 时恢复默认值（5MB）。
func (c *ContentSecurity) WithMaxBodyBytes(n int64) *ContentSecurity {
	if n <= 0 {
		n = defaultContentSecurityMaxBytes
	}
	c.maxBodyBytes = n
	return c
}

// challenge 返回本中间件使用的认证质询。
//
// 本中间件采用自定义的 HMAC 签名方案（`X-Content-Security` 头），
// 不属于 RFC 7617（Basic）或 RFC 6750（Bearer），因此使用自定义 scheme 名。
// RFC 9110 §11.3 允许注册/自定义方案，401 响应必须给出质询。
func (c *ContentSecurity) challenge() Challenge {
	return Challenge{Scheme: contentSecurityScheme}
}

// writeUnauthorized 输出带 WWW-Authenticate 的 401 响应（RFC 9110 §15.5.2 MUST）。
func (c *ContentSecurity) writeUnauthorized(ctx context.Context, w http.ResponseWriter, msg string) {
	WriteUnauthorized(ctx, w, c.challenge(), msg)
}

// Middleware 返回标准形式 func(http.Handler) http.Handler 的内容安全校验中间件。
func (c *ContentSecurity) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				c.writeUnauthorized(r.Context(), w, "missing or malformed content security header")
				return
			}

			// 防重放：时间戳超出容差则拒绝
			ts, err := strconv.ParseInt(timestamp, 10, 64)
			if err != nil {
				c.writeUnauthorized(r.Context(), w, "invalid timestamp")
				return
			}
			now := time.Now().Unix()
			tol := int64(c.tolerance.Seconds())
			if ts+tol < now || now+tol < ts {
				// 时间戳过期属于“凭据失效”（lacks valid authentication credentials），
				// 按 RFC 9110 §15.5.2 应使用 401 而非 403：403 表达的是
				// “服务器理解请求但拒绝执行”的授权不足，与“凭据不对”不是同一语义。
				// 同时保持与本中间件其它失败路径一致（均为 401 + challenge）。
				c.writeUnauthorized(r.Context(), w, "request expired")
				return
			}

			// 计算被签名内容：timestamp\nmethod\npath\nquery\nbodySha256Hex
			//
			// 请求体必须完整参与签名，因此这里把它读入内存；
			// maxBodyBytes 限制开销（多读 1 字节用于判断是否超限）。
			//
			// 读取失败时必须拒绝，而不是按空体继续校验：
			// 旧实现里 err != nil 时 body 保持 ""，相当于把“读取失败”当成“空体”，
			// 请求体是否参与签名变得不可控（完整性约束可被绕过）。
			body := ""
			if r.Body != nil {
				limit := c.maxBodyBytes
				if limit <= 0 {
					limit = defaultContentSecurityMaxBytes
				}
				b, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
				if err != nil {
					writeError(r.Context(), w, http.StatusBadRequest, "failed to read request body")
					return
				}
				if int64(len(b)) > limit {
					writeError(r.Context(), w, http.StatusRequestEntityTooLarge, "request entity too large")
					return
				}
				sum := sha256.Sum256(b)
				body = hex.EncodeToString(sum[:])
				// 恢复 body 供下游 handler 读取
				r.Body = io.NopCloser(bytes.NewReader(b))
			}
			signContent := strings.Join([]string{
				timestamp,
				r.Method,
				r.URL.Path,
				r.URL.RawQuery,
				body,
			}, "\n")

			// 校验签名
			if !hash.HMACVerify(c.key, signContent, signature) {
				c.writeUnauthorized(r.Context(), w, "invalid signature")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

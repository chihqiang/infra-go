package middleware

import (
	"bytes"
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
type ContentSecurity struct {
	key       []byte
	tolerance time.Duration
}

// NewContentSecurity 创建内容安全校验中间件。
// key 为双方共享的 HMAC 密钥。
func NewContentSecurity(key []byte, tolerance time.Duration) *ContentSecurity {
	return &ContentSecurity{key: key, tolerance: tolerance}
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
				writeError(r.Context(), w, http.StatusUnauthorized, "invalid content security header")
				return
			}

			// 防重放：时间戳超出容差则拒绝
			ts, err := strconv.ParseInt(timestamp, 10, 64)
			if err != nil {
				writeError(r.Context(), w, http.StatusUnauthorized, "invalid timestamp")
				return
			}
			now := time.Now().Unix()
			tol := int64(c.tolerance.Seconds())
			if ts+tol < now || now+tol < ts {
				writeError(r.Context(), w, http.StatusForbidden, "request expired")
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
			if !hash.HMACVerify(c.key, signContent, signature) {
				writeError(r.Context(), w, http.StatusUnauthorized, "invalid signature")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

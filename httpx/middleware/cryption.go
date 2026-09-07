package middleware

import (
	"bytes"
	"io"
	"net/http"

	"github.com/chihqiang/infra-go/hash"
	"github.com/chihqiang/infra-go/httpx/match"
	"github.com/chihqiang/infra-go/httpx/respw"
	"github.com/chihqiang/infra-go/logger"
)

// maxEncryptedResponseBytes 限制加密响应的最大缓冲大小（默认 1MB）。
// 超过此大小的响应将回退为明文输出，避免大响应导致内存暴涨。
const maxEncryptedResponseBytes = 1 << 20 // 1 MB

// defaultMaxRequestBytes 默认密文请求体上限（4MB）。
// 解密前先限流读取，避免超大请求体导致内存暴涨。
const defaultMaxRequestBytes int64 = 4 << 20 // 4 MB

// Cryption 是请求/响应 AES-GCM 加解密中间件。
// 请求体需为 base64 编码的 AES-GCM 密文（nonce || ciphertext），中间件解密后
// 交给 handler；handler 写入的 2xx 响应会被加密后返回给客户端。
//
// 采用 AES-GCM 认证加密（AEAD），同时保证机密性与完整性（防篡改），nonce 每次
// 随机生成；相比常见的 AES-ECB 等非认证模式，GCM 能抵御篡改与重放，安全性更高。
//
// 响应加密策略：
//   - 仅 2xx（且非 204/205、非 HEAD）的成功响应体加密；
//   - 错误响应（4xx/5xx）、重定向（3xx）、204/205 及 HEAD 请求保持明文透传，
//     并保留原始状态码，便于客户端排查与 HTTP 语义正确（无 body 的状态码不输出密文 body）。
//   - 响应超过 1MB 缓冲上限时回退为明文输出（不加密），避免大响应导致 OOM。
type Cryption struct {
	key             []byte
	matcher         *match.PathMatcher
	maxRequestBytes int64
}

// NewCryption 创建请求/响应加解密中间件。
// key 长度必须为 16/24/32 字节（对应 AES-128/192/256）。
// skipPaths 为不进行请求/响应加解密的路径列表，命中路径以明文透传（常用于回调、
// 静态资源等无法加密的场景）。匹配方式：精确匹配（如 "/callback"）或以 "*" 结尾
// 的前缀通配（如 "/public/*"）。
// 密文请求体默认上限 4MB，超限返回 413；需要调整请用 NewCryptionWithLimit。
func NewCryption(key []byte, skipPaths ...string) *Cryption {
	return NewCryptionWithLimit(key, defaultMaxRequestBytes, skipPaths...)
}

// NewCryptionWithLimit 创建加解密中间件，并指定密文请求体的最大字节数。
// maxRequestBytes <= 0 时使用默认值 4MB。
func NewCryptionWithLimit(key []byte, maxRequestBytes int64, skipPaths ...string) *Cryption {
	if maxRequestBytes <= 0 {
		maxRequestBytes = defaultMaxRequestBytes
	}
	return &Cryption{
		key:             key,
		matcher:         match.NewPathMatcher(skipPaths),
		maxRequestBytes: maxRequestBytes,
	}
}

// Middleware 返回标准形式 func(http.Handler) http.Handler 的加解密中间件。
func (c *Cryption) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 命中跳过规则的路径不做请求/响应加解密，明文透传（业务照常处理）
			if c.matcher.Match(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// 解密请求体。按"是否存在 body"判断（而非 ContentLength>0），
			// 以兼容 Transfer-Encoding: chunked（ContentLength == -1）的请求。
			if r.Body != nil && r.Body != http.NoBody {
				body, err := io.ReadAll(io.LimitReader(r.Body, c.maxRequestBytes+1))
				if err != nil {
					writeError(r.Context(), w, http.StatusBadRequest, "failed to read request body")
					return
				}
				if int64(len(body)) > c.maxRequestBytes {
					logger.WarnCtx(r.Context(), "encrypted request body too large",
						logger.String("path", r.URL.Path),
						logger.Int64("max_bytes", c.maxRequestBytes),
					)
					writeError(r.Context(), w, http.StatusRequestEntityTooLarge, "encrypted request body too large")
					return
				}
				if len(body) > 0 {
					plain, err := hash.AESGCMDecrypt(c.key, string(body))
					if err != nil {
						logger.WarnCtx(r.Context(), "decrypt request body failed",
							logger.String("path", r.URL.Path),
							logger.Err(err),
						)
						writeError(r.Context(), w, http.StatusBadRequest, "invalid encrypted body")
						return
					}
					r.Body = io.NopCloser(bytes.NewReader(plain))
					r.ContentLength = int64(len(plain))
				}
			}

			// 缓冲 handler 的响应，结束后统一决定"加密输出"还是"明文透传"。
			cw := respw.NewCryptionWriter(w, maxEncryptedResponseBytes)
			next.ServeHTTP(cw, r)

			// handler 未显式 WriteHeader 时按 200 处理。
			code := cw.StatusCode()
			if code == 0 {
				code = http.StatusOK
			}

			// 明文透传分支：
			//  1) 缓冲超限（响应过大，回退明文避免 OOM）；
			//  2) 非 2xx（错误/重定向响应明文，便于客户端直接读取与排错）；
			//  3) 204/205（HTTP 规定无响应体，不得输出密文 body）；
			//  4) HEAD 请求（无响应体）。
			encryptable := code >= http.StatusOK && code < http.StatusMultipleChoices &&
				code != http.StatusNoContent && code != http.StatusResetContent &&
				r.Method != http.MethodHead

			if cw.Overflowed() || !encryptable {
				if cw.Overflowed() {
					logger.WarnCtx(r.Context(), "encrypted response exceeds max buffer, falling back to plaintext",
						logger.String("path", r.URL.Path),
						logger.Int("max_bytes", maxEncryptedResponseBytes),
					)
				}
				// Content-Length 交由 net/http 按实际 body 自动计算，避免与透传内容不一致
				w.Header().Del("Content-Length")
				w.WriteHeader(code)
				if r.Method != http.MethodHead && len(cw.Buffered()) > 0 {
					_, _ = w.Write(cw.Buffered())
				}
				return
			}

			// 加密 2xx 成功响应体并输出。
			encrypted, err := hash.AESGCMEncrypt(c.key, cw.Buffered())
			if err != nil {
				logger.ErrorCtx(r.Context(), "encrypt response failed",
					logger.String("path", r.URL.Path),
					logger.Err(err),
				)
				writeError(r.Context(), w, http.StatusInternalServerError, "encrypt response failed")
				return
			}
			// 密文为 base64 文本；清理可能与密文长度冲突的响应头后输出
			h := w.Header()
			h.Del("Content-Length")
			h.Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(code)
			_, _ = w.Write([]byte(encrypted))
		})
	}
}

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

// Cryption 是请求/响应 AES-GCM 加解密中间件。
// 请求体需为 base64 编码的 AES-GCM 密文（nonce || ciphertext），中间件解密后
// 交给 handler；handler 写入的响应会被加密后返回给客户端。
//
// 采用 AES-GCM 认证加密（AEAD），同时保证机密性与完整性（防篡改），nonce 每次
// 随机生成；相比常见的 AES-ECB 等非认证模式，GCM 能抵御篡改与重放，安全性更高。
// 响应超过 1MB 时自动回退为明文输出（不加密），避免大响应导致 OOM。
type Cryption struct {
	key     []byte
	matcher *match.PathMatcher
}

// NewCryption 创建请求/响应加解密中间件。
// key 长度必须为 16/24/32 字节（对应 AES-128/192/256）。
// skipPaths 为不进行请求/响应加解密的路径列表，命中路径以明文透传（常用于回调、
// 静态资源等无法加密的场景）。匹配方式：精确匹配（如 "/callback"）或以 "*" 结尾
// 的前缀通配（如 "/public/*"）。
func NewCryption(key []byte, skipPaths ...string) *Cryption {
	return &Cryption{key: key, matcher: match.NewPathMatcher(skipPaths)}
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

			// 解密请求体
			if r.ContentLength > 0 {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					writeError(r.Context(), w, http.StatusBadRequest, "failed to read request body")
					return
				}
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

			// 加密响应
			cw := respw.NewCryptionWriter(w, maxEncryptedResponseBytes)
			next.ServeHTTP(cw, r)

			// 缓冲超限：回退为明文输出，不加密
			if cw.Overflowed() {
				logger.WarnCtx(r.Context(), "encrypted response exceeds max buffer, falling back to plaintext",
					logger.String("path", r.URL.Path),
					logger.Int("max_bytes", maxEncryptedResponseBytes),
				)
				if cw.StatusCode() != 0 {
					w.WriteHeader(cw.StatusCode())
				}
				_, _ = w.Write(cw.Buffered())
				return
			}

			// 写入加密后的响应
			encrypted, err := hash.AESGCMEncrypt(c.key, cw.Buffered())
			if err != nil {
				logger.ErrorCtx(r.Context(), "encrypt response failed", logger.Err(err))
				return
			}
			_, _ = w.Write([]byte(encrypted))
		})
	}
}

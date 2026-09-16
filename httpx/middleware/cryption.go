package middleware

import (
	"bytes"
	"io"
	"net/http"

	"github.com/chihqiang/infra-go/hash"
	"github.com/chihqiang/infra-go/httpx/respw"
	"github.com/chihqiang/infra-go/httpx/x"
	"github.com/chihqiang/infra-go/logger"
)

// defaultMaxBytes is the default limit for the encrypted request body and the buffer for the
// encrypted response (both 5MB).
// An oversized request body returns 413; an oversized response falls back to plaintext output so
// that a large response cannot blow up memory.
const defaultMaxBytes = 5 << 20 // 5 MB

// Cryption is a request/response AES-GCM encryption middleware.
// The request body must be base64-encoded AES-GCM ciphertext (nonce || ciphertext), which the
// middleware decrypts before handing it to the handler; a 2xx response written by the handler is
// encrypted before being returned to the client.
//
// It uses AES-GCM authenticated encryption (AEAD), guaranteeing confidentiality and integrity
// (tamper-proof) at the same time, with a fresh random nonce for every message; compared with
// common unauthenticated modes such as AES-ECB, GCM resists tampering and replay and is therefore
// more secure.
//
// Response encryption policy:
//   - only 2xx (and neither 204/205 nor HEAD) successful response bodies are encrypted;
//   - error responses (4xx/5xx), redirects (3xx), 204/205 and HEAD requests keep passing through
//     as plaintext, preserving the original status code for easy client diagnosis and correct
//     HTTP semantics (a status code without a body must not emit an encrypted body).
//   - a response exceeding the buffer limit falls back to plaintext output (unencrypted) to avoid
//     OOM on large responses.
type Cryption struct {
	key              []byte
	matcher          *x.PathMatcher
	maxRequestBytes  int64 // encrypted request body limit
	maxResponseBytes int   // encrypted response buffer limit
}

// NewCryption creates the request/response encryption middleware.
// key must be 16/24/32 bytes long (corresponding to AES-128/192/256).
// skipPaths lists the paths that are neither decrypted nor encrypted; a matching path passes
// through as plaintext (commonly used for callbacks, static assets and other scenarios that
// cannot be encrypted). Matching is either exact (e.g. "/callback") or a prefix wildcard ending
// in "*" (e.g. "/public/*").
// The default limits for the encrypted request body and the encrypted response are both 5MB;
// exceeding them returns 413 / falls back to plaintext respectively. Use NewCryptionWithLimit to
// change them.
func NewCryption(key []byte, skipPaths ...string) *Cryption {
	return NewCryptionWithLimit(key, defaultMaxBytes, defaultMaxBytes, skipPaths...)
}

// NewCryptionWithLimit creates the encryption middleware with explicit maximum byte sizes for the
// encrypted request body and the encrypted response.
// maxRequestBytes is the encrypted request body limit (413 when exceeded); maxResponseBytes is the
// encrypted response buffer limit (falls back to plaintext output when exceeded, avoiding OOM).
// Any parameter <= 0 uses the default of 5MB.
func NewCryptionWithLimit(key []byte, maxRequestBytes int64, maxResponseBytes int, skipPaths ...string) *Cryption {
	if maxRequestBytes <= 0 {
		maxRequestBytes = defaultMaxBytes
	}
	if maxResponseBytes <= 0 {
		maxResponseBytes = defaultMaxBytes
	}
	return &Cryption{
		key:              key,
		matcher:          x.NewPathMatcher(skipPaths),
		maxRequestBytes:  maxRequestBytes,
		maxResponseBytes: maxResponseBytes,
	}
}

// Middleware returns the encryption middleware in the standard func(http.Handler) http.Handler
// form.
func (c *Cryption) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// A path matching a skip rule is neither decrypted nor encrypted; it passes through as
			// plaintext (the business logic still runs as usual)
			if c.matcher.Match(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Decrypt the request body. The check is "is there a body" rather than
			// ContentLength > 0, so that requests with Transfer-Encoding: chunked
			// (ContentLength == -1) work too.
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

			// Buffer the handler's response and decide at the end between "encrypted output" and
			// "plaintext pass-through".
			cw := respw.NewCryptionWriter(w, c.maxResponseBytes)
			next.ServeHTTP(cw, r)

			// A handler that never calls WriteHeader explicitly is treated as 200.
			code := cw.StatusCode()
			if code == 0 {
				code = http.StatusOK
			}

			encryptable := code >= http.StatusOK && code < http.StatusMultipleChoices &&
				code != http.StatusNoContent && code != http.StatusResetContent &&
				r.Method != http.MethodHead

			// Buffer overflow: CryptionWriter has already switched to plaintext pass-through mode,
			// and the response headers plus the complete body (buffered + subsequent) have been
			// written to the underlying writer, so nothing more may be written here — doing so would
			// duplicate the response. Historical defect: only the truncated buffered prefix was
			// written here, which silently truncated the overflow response so that the client
			// received incomplete data with no error signal at all.
			if cw.Overflowed() {
				logger.WarnCtx(r.Context(), "encrypted response exceeds max buffer, falling back to plaintext",
					logger.String("path", r.URL.Path),
					logger.Int("max_bytes", c.maxResponseBytes),
				)
				return
			}

			// Plaintext pass-through branches:
			//  1) not 2xx (error/redirect responses stay plaintext so clients can read and debug them
			//     directly);
			//  2) 204/205 (HTTP defines no response body, so an encrypted body must not be emitted);
			//  3) HEAD requests (no response body).
			if !encryptable {
				// Let net/http compute Content-Length from the actual body so that it cannot disagree
				// with the pass-through content
				w.Header().Del("Content-Length")
				w.WriteHeader(code)
				if r.Method != http.MethodHead && len(cw.Buffered()) > 0 {
					_, _ = w.Write(cw.Buffered())
				}
				return
			}

			// Encrypt the 2xx success response body and write it out.
			encrypted, err := hash.AESGCMEncrypt(c.key, cw.Buffered())
			if err != nil {
				logger.ErrorCtx(r.Context(), "encrypt response failed",
					logger.String("path", r.URL.Path),
					logger.Err(err),
				)
				writeError(r.Context(), w, http.StatusInternalServerError, "encrypt response failed")
				return
			}
			// The ciphertext is base64 text; drop the headers that could conflict with its length
			// before writing it out
			h := w.Header()
			h.Del("Content-Length")
			h.Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(code)
			_, _ = w.Write([]byte(encrypted))
		})
	}
}

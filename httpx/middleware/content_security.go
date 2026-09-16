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

// ContentSecurityHeader is the field name of the `X-Content-Security` content security request
// header.
const ContentSecurityHeader = "X-Content-Security"

// defaultContentSecurityMaxBytes is the default maximum size of a request body that participates
// in the signature (5MB, matching Cryption).
const defaultContentSecurityMaxBytes = 5 << 20

// contentSecurityScheme is the authentication scheme name this middleware declares in
// WWW-Authenticate. A custom HMAC signature is neither Basic nor Bearer, so it needs a scheme
// name of its own (RFC 9110 §11.3).
const contentSecurityScheme = "ContentSecurity"

// ContentSecurity is a content security verification middleware (tamper-proof + replay-proof).
// Clients must carry a signature in the `X-Content-Security` header:
//
//	X-Content-Security: time=<unix seconds>; signature=<base64 HMAC-SHA256>
//
// The signed content is: `timestamp\nmethod\npath\nquery\nbodySha256Hex`
// (timestamp is the timestamp from the request header and bodySha256Hex is the hex SHA-256
// digest of the request body).
//
// Verification rules:
//   - signature valid (HMAC-SHA256 matches) and timestamp within the tolerance → pass through
//   - signature invalid → 401
//   - timestamp outside the tolerance (replay protection) → 403
//   - reading the request body failed → 400 (must not keep verifying with an empty body)
//   - request body exceeds maxBodyBytes → 413
//
// Security note: the whole request body must participate in the signature, so this middleware
// needs to read the entire body into memory. maxBodyBytes bounds that cost and keeps an oversized
// request body from exhausting memory.
type ContentSecurity struct {
	key          []byte
	tolerance    time.Duration
	maxBodyBytes int64
}

// NewContentSecurity creates the content security verification middleware.
// key is the HMAC secret shared by both parties.
// The request body limit defaults to 5MB and can be adjusted with WithMaxBodyBytes.
func NewContentSecurity(key []byte, tolerance time.Duration) *ContentSecurity {
	return &ContentSecurity{
		key:          key,
		tolerance:    tolerance,
		maxBodyBytes: defaultContentSecurityMaxBytes,
	}
}

// WithMaxBodyBytes sets the maximum request body size (in bytes) that participates in the
// signature. n <= 0 restores the default (5MB).
func (c *ContentSecurity) WithMaxBodyBytes(n int64) *ContentSecurity {
	if n <= 0 {
		n = defaultContentSecurityMaxBytes
	}
	c.maxBodyBytes = n
	return c
}

// challenge returns the authentication challenge used by this middleware.
//
// This middleware uses a custom HMAC signature scheme (the `X-Content-Security` header) which is
// neither RFC 7617 (Basic) nor RFC 6750 (Bearer), so it uses a custom scheme name.
// RFC 9110 §11.3 allows registered/custom schemes, and a 401 response must carry a challenge.
func (c *ContentSecurity) challenge() Challenge {
	return Challenge{Scheme: contentSecurityScheme}
}

// writeUnauthorized writes a 401 response carrying WWW-Authenticate (RFC 9110 §15.5.2 MUST).
func (c *ContentSecurity) writeUnauthorized(ctx context.Context, w http.ResponseWriter, msg string) {
	WriteUnauthorized(ctx, w, c.challenge(), msg)
}

// Middleware returns the content security middleware in the standard func(http.Handler)
// http.Handler form.
func (c *ContentSecurity) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Parse the X-Content-Security header, extracting timestamp and signature
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

			// Replay protection: reject once the timestamp leaves the tolerance window
			ts, err := strconv.ParseInt(timestamp, 10, 64)
			if err != nil {
				c.writeUnauthorized(r.Context(), w, "invalid timestamp")
				return
			}
			now := time.Now().Unix()
			tol := int64(c.tolerance.Seconds())
			if ts+tol < now || now+tol < ts {
				// An expired timestamp counts as "lacks valid authentication credentials", so
				// RFC 9110 §15.5.2 requires 401 rather than 403: 403 expresses insufficient
				// authorization ("the server understood the request but refuses to fulfill it"),
				// which is not the same as wrong credentials. This also keeps this path consistent
				// with the middleware's other failure paths (all of them 401 + challenge).
				c.writeUnauthorized(r.Context(), w, "request expired")
				return
			}

			// Compute the signed content: timestamp\nmethod\npath\nquery\nbodySha256Hex
			//
			// The whole request body must participate in the signature, so read it into memory here;
			// maxBodyBytes bounds the overhead (one extra byte is read to detect overflow).
			//
			// A read failure must be rejected instead of falling back to an empty body:
			// the old implementation left body as "" when err != nil, which treated a failed read
			// as an empty body and made it unpredictable whether the request body participated in
			// the signature (the integrity constraint could be bypassed).
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
				// Restore the body so downstream handlers can read it
				r.Body = io.NopCloser(bytes.NewReader(b))
			}
			signContent := strings.Join([]string{
				timestamp,
				r.Method,
				r.URL.Path,
				r.URL.RawQuery,
				body,
			}, "\n")

			// Verify the signature
			if !hash.HMACVerify(c.key, signContent, signature) {
				c.writeUnauthorized(r.Context(), w, "invalid signature")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

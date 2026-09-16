package middleware

import (
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"strings"
)

// defaultMaxDecompressedBytes is the default limit on the decompressed request body (5MB, the same
// as the Cryption limit).
const defaultMaxDecompressedBytes = 5 << 20

// errDecompressedTooLarge means the decompressed size exceeded the limit.
var errDecompressedTooLarge = errors.New("middleware: decompressed body exceeds limit")

// Gunzip is a middleware that automatically decompresses gzip request bodies.
// When the Content-Encoding request header contains "gzip", the request body is wrapped in a gzip
// reader; a failed decompression returns 400 Bad Request.
//
// Security note: gzip can be abused as a "decompression bomb" — a tiny compressed payload can
// expand into a huge amount of content. Limiting only the compressed size (MaxBytes relies on
// Content-Length) would let the limit be bypassed. This middleware caps the number of bytes
// **after decompression** (5MB by default); once the limit is exceeded the downstream read
// receives an error, so the decompressed result cannot blow up memory.
//
// It is best combined with MaxBytes, with this middleware registered in an inner layer: MaxBytes
// limits the compressed body first and this middleware then limits the decompressed body, the two
// being complementary:
//
//	server.Use(httpx.WithMaxBytes(1<<20), httpx.WithGunzip())
type Gunzip struct {
	maxDecompressedBytes int64
}

// NewGunzip creates the gzip decompression middleware.
// The decompressed limit defaults to 5MB and can be adjusted with WithMaxDecompressedBytes.
func NewGunzip() *Gunzip {
	return &Gunzip{maxDecompressedBytes: defaultMaxDecompressedBytes}
}

// WithMaxDecompressedBytes sets the limit on the decompressed request body (in bytes).
// n <= 0 means unlimited (not recommended: it reintroduces the decompression bomb risk).
func (g *Gunzip) WithMaxDecompressedBytes(n int64) *Gunzip {
	g.maxDecompressedBytes = n
	return g
}

// Middleware returns the gzip decompression middleware in the standard func(http.Handler)
// http.Handler form.
func (g *Gunzip) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.Header.Get("Content-Encoding"), "gzip") {
				next.ServeHTTP(w, r)
				return
			}

			reader, err := gzip.NewReader(r.Body)
			if err != nil {
				writeError(r.Context(), w, http.StatusBadRequest, "invalid gzip body")
				return
			}

			r.Body = &limitedReadCloser{
				reader: reader,
				limit:  g.maxDecompressedBytes,
			}
			// The decompressed length is unknown and does not match the compressed body's
			// Content-Length, so it must be cleared; otherwise downstream (including MaxBytes) would
			// misjudge based on the compressed length.
			r.ContentLength = -1
			// The body is decompressed now, so the marker must not be passed downstream.
			r.Header.Del("Content-Encoding")

			next.ServeHTTP(w, r)
		})
	}
}

// limitedReadCloser caps the total number of bytes read from the underlying reader and returns an
// error once limit is exceeded.
//
// At most limit+1 bytes are read: the extra byte distinguishes "exactly at the limit" from "above
// the limit". Once the limit is known to be exceeded, the out-of-range bytes already read are
// discarded (returning 0, err) and the caller (e.g. io.ReadAll) stops reading right away.
type limitedReadCloser struct {
	reader   io.ReadCloser
	limit    int64
	consumed int64
}

func (l *limitedReadCloser) Read(p []byte) (int, error) {
	if l.limit > 0 {
		// limit + 1 is used to detect overflow
		remaining := l.limit + 1 - l.consumed
		if remaining <= 0 {
			return 0, errDecompressedTooLarge
		}
		if int64(len(p)) > remaining {
			p = p[:remaining]
		}
	}

	n, err := l.reader.Read(p)
	l.consumed += int64(n)

	if l.limit > 0 && l.consumed > l.limit {
		return 0, errDecompressedTooLarge
	}
	return n, err
}

// Close closes the underlying gzip reader and also releases the original Body it wraps.
func (l *limitedReadCloser) Close() error {
	return l.reader.Close()
}

package handler

import (
	"compress/gzip"
	"net/http"
	"strings"
)

const (
	headerContentEncoding = "Content-Encoding"
	headerContentLength   = "Content-Length"
	encodingGzip          = "gzip"
)

// GunzipHandler transparently decompresses gzip-encoded request bodies. Place it
// before MaxBytesHandler so the body limit is applied to the decompressed data
// instead of the (possibly tiny) compressed payload.
func GunzipHandler(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isGzipEncoding(r.Header.Get(headerContentEncoding)) {
			next(w, r)
			return
		}

		reader, err := gzip.NewReader(r.Body)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid gzip body")
			return
		}
		defer func() { _ = reader.Close() }()

		r.Body = reader
		r.Header.Del(headerContentEncoding)
		r.Header.Del(headerContentLength)
		r.ContentLength = -1

		next(w, r)
	}
}

// isGzipEncoding reports whether the Content-Encoding header contains gzip.
// Values are matched case-insensitively and as comma-separated tokens so
// "GZIP" and "gzip, identity" are recognised.
func isGzipEncoding(value string) bool {
	for _, part := range strings.Split(value, ",") {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case encodingGzip, "x-gzip":
			return true
		}
	}
	return false
}

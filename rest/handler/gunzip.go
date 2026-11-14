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
		if !strings.Contains(r.Header.Get(headerContentEncoding), encodingGzip) {
			next(w, r)
			return
		}

		reader, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
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

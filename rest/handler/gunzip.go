package handler

import (
	"compress/gzip"
	"net/http"
	"strings"

	"github.com/chihqiang/ten-go/core/logger"
)

const (
	headerContentEncoding = "Content-Encoding"
	headerContentLength   = "Content-Length"
	encodingGzip          = "gzip"
)

// GunzipConfig configures GunzipHandler.
type GunzipConfig struct {
	Enabled bool `json:",optional"`
}

// GunzipHandler transparently decompresses gzip-encoded request bodies.
type GunzipHandler struct {
	config GunzipConfig
}

// NewGunzipHandler creates a GunzipHandler.
func NewGunzipHandler(config GunzipConfig) *GunzipHandler {
	return &GunzipHandler{config: config}
}

// Handle wraps next with gzip request decompression.
func (h *GunzipHandler) Handle(next http.HandlerFunc) http.HandlerFunc {
	if !h.config.Enabled {
		return next
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if !h.isGzipEncoding(r.Header.Get(headerContentEncoding)) {
			next(w, r)
			return
		}

		reader, err := gzip.NewReader(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			logReject(r, "rest: invalid gzip request body", logger.Err(err))
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
func (h *GunzipHandler) isGzipEncoding(value string) bool {
	for _, part := range strings.Split(value, ",") {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case encodingGzip, "x-gzip":
			return true
		}
	}
	return false
}

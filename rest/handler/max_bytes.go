package handler

import (
	"errors"
	"io"
	"net/http"

	"github.com/chihqiang/ten-go/core/logger"
)

// MaxBytesConfig configures MaxBytesHandler.
type MaxBytesConfig struct {
	Enabled bool  `json:",default=true"`
	Limit   int64 `json:",default=1048576"`
}

// MaxBytesHandler limits the amount of request body data that can be read.
type MaxBytesHandler struct {
	config MaxBytesConfig
}

// NewMaxBytesHandler creates a MaxBytesHandler.
func NewMaxBytesHandler(config MaxBytesConfig) *MaxBytesHandler {
	return &MaxBytesHandler{config: config}
}

// Handle wraps next with the request body limit.
func (h *MaxBytesHandler) Handle(next http.HandlerFunc) http.HandlerFunc {
	if !h.config.Enabled || h.config.Limit <= 0 {
		return next
	}

	return func(w http.ResponseWriter, r *http.Request) {
		limit := h.config.Limit
		if r.ContentLength > limit {
			h.markConnectionClose(w)
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			logReject(r, "rest: request body too large",
				logger.Int64("content_length", r.ContentLength),
				logger.Int64("limit", limit),
			)
			return
		}
		if r.Body != nil {
			r.Body = &maxBytesBody{
				ReadCloser: http.MaxBytesReader(w, r.Body, limit),
				onLimit: func() {
					h.markConnectionClose(w)
					logReject(r, "rest: request body too large",
						logger.Int64("limit", limit),
					)
				},
			}
		}
		next(w, r)
	}
}

// maxBytesBody observes the limit error returned by http.MaxBytesReader.
type maxBytesBody struct {
	io.ReadCloser
	onLimit  func()
	notified bool
}

func (b *maxBytesBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if !b.notified {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			b.notified = true
			b.onLimit()
		}
	}
	return n, err
}

// markConnectionClose asks net/http to close the keep-alive connection after the
// reply: WriteHeader sets closeAfterReply when the response carries this header.
func (h *MaxBytesHandler) markConnectionClose(w http.ResponseWriter) {
	if w.Header().Get("Connection") == "" {
		w.Header().Set("Connection", "close")
	}
}

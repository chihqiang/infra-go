package handler

import (
	"context"
	"net/http"

	"github.com/chihqiang/ten-go/core/logger"
	"github.com/google/uuid"
)

// RequestIDHeader is the request and response header carrying the request ID.
const RequestIDHeader = "X-Request-ID"

// RequestIDLogField is the log field name used for the request ID.
const RequestIDLogField = "request_id"

// init wires the request ID into every ctx-aware log call, the way core/trace
// does for trace_id: once this package is linked, logger.XxxCtx adds
// request_id whenever the context carries one.
func init() {
	logger.RegisterContextExtractor(func(ctx context.Context) []logger.Field {
		id, ok := RequestIDFromContext(ctx)
		if !ok {
			return nil
		}
		return []logger.Field{logger.String(RequestIDLogField, id)}
	})
}

// RequestIDConfig configures RequestIDHandler.
type RequestIDConfig struct {
	Enabled bool `json:",default=true"`
}

// RequestIDHandler preserves a safe incoming request ID or generates a UUID.
type RequestIDHandler struct {
	config RequestIDConfig
}

// NewRequestIDHandler creates a RequestIDHandler.
func NewRequestIDHandler(config RequestIDConfig) *RequestIDHandler {
	return &RequestIDHandler{config: config}
}

// Handle assigns the request ID to the context and response.
func (h *RequestIDHandler) Handle(next http.HandlerFunc) http.HandlerFunc {
	if !h.config.Enabled {
		return next
	}

	return func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !h.validRequestID(id) {
			generated, err := uuid.NewRandom()
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				logReject(r, "rest: generate request ID", logger.Err(err))
				return
			}
			id = generated.String()
		}

		w.Header().Set(RequestIDHeader, id)
		r = r.WithContext(ContextWithRequestID(r.Context(), id))
		next(w, r)
	}
}

// validRequestID reports whether an incoming request ID is safe to echo back and
// to log: 1-128 characters from [A-Za-z0-9._-].
func (h *RequestIDHandler) validRequestID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, char := range id {
		if (char < 'a' || char > 'z') &&
			(char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') &&
			char != '-' && char != '_' && char != '.' {
			return false
		}
	}
	return true
}

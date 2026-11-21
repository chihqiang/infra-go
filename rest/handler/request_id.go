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

type requestIDContextKey struct{}

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
		if !validRequestID(id) {
			generated, err := uuid.NewRandom()
			if err != nil {
				logger.Error("rest: generate request ID", logger.Err(err))
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			id = generated.String()
		}

		w.Header().Set(RequestIDHeader, id)
		r = r.WithContext(ContextWithRequestID(r.Context(), id))
		next(w, r)
	}
}

// ContextWithRequestID returns a context carrying requestID in the same slot
// used by RequestIDHandler, so RequestIDFromContext and response helpers can
// read it. An empty requestID leaves ctx unchanged.
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if requestID == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDContextKey{}, requestID)
}

// RequestIDFromContext returns the request ID stored by RequestIDHandler or
// ContextWithRequestID.
func RequestIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	id, ok := ctx.Value(requestIDContextKey{}).(string)
	return id, ok
}

func validRequestID(id string) bool {
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

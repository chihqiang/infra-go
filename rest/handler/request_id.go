package handler

import (
	"context"
	"net/http"

	"github.com/chihqiang/infra-go/core/logger"
	"github.com/google/uuid"
)

const RequestIDHeader = "X-Request-ID"

type requestIDContextKey struct{}

// RequestIDHandler preserves a safe incoming request ID or generates a UUID.
func RequestIDHandler(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !validRequestID(id) {
			generated, err := uuid.NewRandom()
			if err != nil {
				logger.Error("rest: generate request ID", logger.Err(err))
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			id = generated.String()
		}

		w.Header().Set(RequestIDHeader, id)
		r = r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, id))
		next(w, r)
	}
}

// RequestIDFromContext returns the request ID stored by RequestIDHandler.
func RequestIDFromContext(ctx context.Context) (string, bool) {
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

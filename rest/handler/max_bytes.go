package handler

import (
	"net/http"

	"github.com/chihqiang/infra-go/rest/chain"
)

// MaxBytesHandler limits the amount of request body data that can be read.
// A nonpositive limit disables the check.
func MaxBytesHandler(limit int64) chain.Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		if limit <= 0 {
			return next
		}

		return func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				writeStatusJSON(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next(w, r)
		}
	}
}

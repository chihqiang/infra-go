package handler

import (
	"net/http"
	"strconv"

	"github.com/chihqiang/infra-go/core/breaker"
	"github.com/chihqiang/infra-go/rest/chain"
	"github.com/chihqiang/infra-go/rest/internal/response"
)

// BreakerHandler routes requests through a circuit breaker. Responses with a
// status of 500 or above are reported as failures.
func BreakerHandler(br breaker.Breaker) chain.Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		if br == nil {
			return next
		}

		return func(w http.ResponseWriter, r *http.Request) {
			promise, err := br.AllowCtx(r.Context())
			if err != nil {
				writeError(w, r, http.StatusServiceUnavailable, "service unavailable")
				return
			}

			recorder := response.NewStatusWriter(w)
			completed := false
			// A panicking handler must still resolve the breaker promise,
			// otherwise the request is counted but never classified.
			defer func() {
				if !completed {
					promise.Reject("handler panic")
				}
			}()

			next(recorder, r)
			completed = true

			status := recorder.Status()
			if status >= http.StatusInternalServerError {
				promise.Reject("http status " + strconv.Itoa(status))
				return
			}
			promise.Accept()
		}
	}
}

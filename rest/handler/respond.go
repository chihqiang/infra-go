package handler

import (
	"net/http"

	"github.com/chihqiang/infra-go/rest/internal/response"
)

// writeError writes the shared code/msg/request_id envelope. The envelope
// definition lives in internal/response so handler middleware and the rest
// package cannot drift apart; only the request ID is resolved here.
func writeError(w http.ResponseWriter, r *http.Request, status int, message string) {
	requestID, ok := RequestIDFromContext(r.Context())
	if !ok {
		// A recovered panic unwinds to the outer middleware with the original
		// request, but RequestIDHandler already stored the ID in the response
		// header before calling next.
		requestID = w.Header().Get(RequestIDHeader)
	}
	_ = response.WriteError(w, status, message, requestID)
}

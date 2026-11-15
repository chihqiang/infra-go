package handler

import (
	"net/http"

	"github.com/chihqiang/infra-go/rest/internal/response"
)

// writeError writes the shared code/msg/request_id envelope. The envelope
// definition lives in internal/response so handler middleware and the rest
// package cannot drift apart; only the request ID is resolved here.
func writeError(w http.ResponseWriter, r *http.Request, status int, message string) {
	requestID, _ := RequestIDFromContext(r.Context())
	_ = response.WriteError(w, status, message, requestID)
}

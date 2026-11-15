package handler

import (
	"encoding/json"
	"net/http"
)

// errorResponse mirrors the error fields of rest.BaseResponse. handler cannot
// import the rest package (rest imports handler), so this small shape is kept
// here and must stay in sync with rest.BaseResponse.
type errorResponse struct {
	Code      int    `json:"code"`
	Msg       string `json:"msg"`
	RequestID string `json:"request_id,omitempty"`
}

// businessCodeError matches rest.BusinessCodeError.
const businessCodeError = -1

// writeError writes the shared error envelope used by rest.Error: a business
// code of -1, the message and the request ID resolved from the request (with a
// fallback to the response header for recovered panics).
func writeError(w http.ResponseWriter, r *http.Request, status int, message string) {
	requestID, ok := RequestIDFromContext(r.Context())
	if !ok {
		requestID = w.Header().Get(RequestIDHeader)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{
		Code:      businessCodeError,
		Msg:       message,
		RequestID: requestID,
	})
}

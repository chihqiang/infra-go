package response

import (
	"encoding/json"
	"net/http"
)

const (
	// BusinessCodeOK is the code for a successful business response.
	BusinessCodeOK = 0
	// BusinessMsgOK is the default message for a successful business response.
	BusinessMsgOK = "ok"
	// BusinessCodeError is the code for a failed business response.
	BusinessCodeError = -1
)

// BaseResponse is the shared response envelope. Code is a business code, not an
// HTTP status; RequestID is filled from the request context when available.
type BaseResponse[T any] struct {
	Code      int    `json:"code" xml:"code"`
	Msg       string `json:"msg" xml:"msg"`
	RequestID string `json:"request_id,omitempty" xml:"request_id,omitempty"`
	Data      T      `json:"data,omitempty" xml:"data,omitempty"`
}

// WriteJSON encodes v as JSON with the given HTTP status.
func WriteJSON(w http.ResponseWriter, status int, v any) error {
	header := w.Header()
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", "application/json; charset=utf-8")
	}
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

// WriteError writes a failed BaseResponse using BusinessCodeError. Handler and
// rest responses share this envelope so middleware failures and business
// errors have the same code/msg/request_id shape.
func WriteError(w http.ResponseWriter, status int, message, requestID string) error {
	return WriteJSON(w, status, BaseResponse[any]{
		Code:      BusinessCodeError,
		Msg:       message,
		RequestID: requestID,
	})
}

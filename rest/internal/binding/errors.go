package binding

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Error describes a binding failure together with the HTTP status it should
// produce, so the rest package can answer with the right code.
type Error struct {
	Status int
	Err    error
}

// Error returns the underlying message.
func (e *Error) Error() string {
	if e.Err == nil {
		return http.StatusText(e.StatusCode())
	}
	return e.Err.Error()
}

// Unwrap exposes the cause for errors.Is and errors.As.
func (e *Error) Unwrap() error {
	return e.Err
}

// StatusCode reports the response status for the failure.
func (e *Error) StatusCode() int {
	if e.Status == 0 {
		return http.StatusBadRequest
	}
	return e.Status
}

func badRequest(format string, args ...any) error {
	return &Error{Status: http.StatusBadRequest, Err: fmt.Errorf(format, args...)}
}

// requestError classifies a failure raised while reading the request body.
// http.MaxBytesReader reports an oversized body as a *http.MaxBytesError, and
// http.Request.ParseForm has its own 10MB cap reported as "http: POST too
// large". Both are size limits, not malformed input, so they map to 413.
func requestError(format string, err error) error {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) || strings.Contains(err.Error(), "http: POST too large") {
		return &Error{
			Status: http.StatusRequestEntityTooLarge,
			Err:    fmt.Errorf("rest/binding: request body too large: %w", err),
		}
	}
	return badRequest(format, err)
}

func badRequestErr(err error) error {
	if err == nil {
		return nil
	}
	return &Error{Status: http.StatusBadRequest, Err: err}
}

func unsupportedMediaType(contentType string) error {
	return &Error{
		Status: http.StatusUnsupportedMediaType,
		Err:    fmt.Errorf("rest/binding: unsupported media type %q", contentType),
	}
}

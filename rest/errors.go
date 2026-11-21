package rest

import (
	"errors"
	"fmt"
	"net/http"
)

// Common request errors that map onto HTTP status codes. Each one implements
// StatusCoder, so StatusOf resolves it through the same path as any other error
// that chooses its own status.
var (
	ErrBadRequest        = NewStatusError(http.StatusBadRequest, "rest: bad request")
	ErrUnauthorized      = NewStatusError(http.StatusUnauthorized, "rest: unauthorized")
	ErrForbidden         = NewStatusError(http.StatusForbidden, "rest: forbidden")
	ErrResourceNotFound  = NewStatusError(http.StatusNotFound, "rest: resource not found")
	ErrConflict          = NewStatusError(http.StatusConflict, "rest: conflict")
	ErrTooManyRequests   = NewStatusError(http.StatusTooManyRequests, "rest: too many requests")
	ErrServerUnavailable = NewStatusError(http.StatusServiceUnavailable, "rest: service unavailable")
)

// StatusCoder lets an error choose its own response status.
type StatusCoder interface {
	StatusCode() int
}

// StatusError is an error that carries the HTTP status it maps to. Define
// custom sentinels with NewStatusError; unlike CodeMsg it is not rendered as a
// business error by ErrorJSON, so the envelope keeps code -1 and the error text
// (the text is preserved for 5xx too).
type StatusError struct {
	status  int
	message string
}

// NewStatusError creates a StatusError for the given HTTP status.
func NewStatusError(status int, message string) *StatusError {
	return &StatusError{status: status, message: message}
}

// Error returns the message the error was created with.
func (e *StatusError) Error() string {
	if e == nil {
		return ""
	}
	return e.message
}

// StatusCode returns the HTTP status the error maps to.
func (e *StatusError) StatusCode() int {
	if e == nil {
		return 0
	}
	return e.status
}

// StatusOf resolves the HTTP status for err. Errors implementing StatusCoder
// choose their own status; every other error is a server error.
func StatusOf(err error) int {
	if err == nil {
		return http.StatusOK
	}

	var coder StatusCoder
	if errors.As(err, &coder) {
		// Ignore out-of-range values: passing them to WriteHeader would panic.
		if status := coder.StatusCode(); status >= 100 && status <= 999 {
			return status
		}
	}
	return http.StatusInternalServerError
}

// CodeMsg combines a business code with a message. It implements error.
type CodeMsg struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// Error renders the code and message.
func (c *CodeMsg) Error() string {
	return fmt.Sprintf("code: %d, msg: %s", c.Code, c.Msg)
}

// StatusCode maps the business code onto an HTTP status so the error can be
// passed to ErrorJSON or ErrorXML: codes that already are HTTP 4xx/5xx statuses
// are used as-is, every other business code is treated as a client-side failure
// (400). This keeps the business message visible instead of turning every
// CodeMsg into a hidden 500.
func (c *CodeMsg) StatusCode() int {
	if c == nil {
		return 0
	}
	if c.Code >= 400 && c.Code <= 599 {
		return c.Code
	}
	return http.StatusBadRequest
}

// NewCodeError creates a business error carrying code and msg.
func NewCodeError(code int, msg string) error {
	return &CodeMsg{Code: code, Msg: msg}
}

package rest

import (
	"errors"
	"fmt"
	"net/http"
)

// Common request errors that map onto HTTP status codes.
var (
	ErrBadRequest         = errors.New("rest: bad request")
	ErrUnauthorized       = errors.New("rest: unauthorized")
	ErrForbidden          = errors.New("rest: forbidden")
	ErrNotFound           = errors.New("rest: not found")
	ErrConflict           = errors.New("rest: conflict")
	ErrTooManyRequests    = errors.New("rest: too many requests")
	ErrServiceUnavailable = errors.New("rest: service unavailable")
)

// StatusCoder lets an error choose its own response status.
type StatusCoder interface {
	StatusCode() int
}

// StatusOf resolves the HTTP status for err.
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

	switch {
	case errors.Is(err, ErrBadRequest):
		return http.StatusBadRequest
	case errors.Is(err, ErrUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrConflict):
		return http.StatusConflict
	case errors.Is(err, ErrTooManyRequests):
		return http.StatusTooManyRequests
	case errors.Is(err, ErrServiceUnavailable):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
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
// passed to Error: codes that already are HTTP 4xx/5xx statuses are used as-is,
// every other business code is treated as a client-side failure (400). This
// keeps the business message visible instead of turning every CodeMsg into a
// hidden 500.
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

// New creates a CodeMsg error carrying a business code.
//
// Deprecated: use NewCodeError.
func New(code int, msg string) error {
	return NewCodeError(code, msg)
}

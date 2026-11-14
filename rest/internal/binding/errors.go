package binding

import (
	"errors"
	"net/http"
)

// Error describes a binding failure with the HTTP status it should produce.
type Error struct {
	Status int
	Err    error
}

// Error returns the underlying message.
func (e *Error) Error() string {
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

func wrapError(err error) error {
	if err == nil {
		return nil
	}

	var bindingErr *Error
	if errors.As(err, &bindingErr) {
		return err
	}

	return &Error{Status: http.StatusBadRequest, Err: err}
}

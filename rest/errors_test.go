package rest

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

type codedError struct{}

func (codedError) Error() string   { return "gone" }
func (codedError) StatusCode() int { return http.StatusGone }

func TestStatusOf(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "nil", err: nil, want: http.StatusOK},
		{name: "bad request", err: ErrBadRequest, want: http.StatusBadRequest},
		{name: "wrapped unauthorized", err: fmt.Errorf("login: %w", ErrUnauthorized), want: http.StatusUnauthorized},
		{name: "forbidden", err: ErrForbidden, want: http.StatusForbidden},
		{name: "not found", err: ErrNotFound, want: http.StatusNotFound},
		{name: "conflict", err: ErrConflict, want: http.StatusConflict},
		{name: "too many requests", err: ErrTooManyRequests, want: http.StatusTooManyRequests},
		{name: "unavailable", err: ErrServiceUnavailable, want: http.StatusServiceUnavailable},
		{name: "status coder", err: codedError{}, want: http.StatusGone},
		{name: "unknown", err: errors.New("boom"), want: http.StatusInternalServerError},
	}

	for _, test := range tests {
		if got := StatusOf(test.err); got != test.want {
			t.Errorf("%s: status = %d, want %d", test.name, got, test.want)
		}
	}
}

func TestNewCarriesCodeAndMessage(t *testing.T) {
	err := New(4001, "invalid order")

	var codeMsg *CodeMsg
	if !errors.As(err, &codeMsg) {
		t.Fatalf("error = %T, want *CodeMsg", err)
	}
	if codeMsg.Code != 4001 || codeMsg.Msg != "invalid order" {
		t.Fatalf("code msg = %+v", codeMsg)
	}
	if err.Error() != "code: 4001, msg: invalid order" {
		t.Fatalf("Error() = %q", err.Error())
	}
}

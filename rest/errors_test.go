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

type invalidCodedError struct{}

func (invalidCodedError) Error() string   { return "bad status" }
func (invalidCodedError) StatusCode() int { return 42 }

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
		{name: "not found", err: ErrResourceNotFound, want: http.StatusNotFound},
		{name: "conflict", err: ErrConflict, want: http.StatusConflict},
		{name: "too many requests", err: ErrTooManyRequests, want: http.StatusTooManyRequests},
		{name: "unavailable", err: ErrServerUnavailable, want: http.StatusServiceUnavailable},
		{name: "status coder", err: codedError{}, want: http.StatusGone},
		{name: "invalid status coder falls back", err: invalidCodedError{}, want: http.StatusInternalServerError},
		{name: "code msg business code", err: NewCodeError(4001, "invalid order"), want: http.StatusBadRequest},
		{name: "code msg http code", err: NewCodeError(http.StatusConflict, "conflict"), want: http.StatusConflict},
		{name: "code msg typed nil", err: (*CodeMsg)(nil), want: http.StatusInternalServerError},
		{name: "unknown", err: errors.New("boom"), want: http.StatusInternalServerError},
	}

	for _, test := range tests {
		if got := StatusOf(test.err); got != test.want {
			t.Errorf("%s: status = %d, want %d", test.name, got, test.want)
		}
	}
}

func TestNewStatusErrorCarriesStatus(t *testing.T) {
	err := NewStatusError(http.StatusPaymentRequired, "rest: payment required")

	if err.Error() != "rest: payment required" {
		t.Fatalf("Error() = %q", err.Error())
	}
	if got := StatusOf(fmt.Errorf("pay: %w", err)); got != http.StatusPaymentRequired {
		t.Fatalf("wrapped status = %d, want %d", got, http.StatusPaymentRequired)
	}
	if got := StatusOf((*StatusError)(nil)); got != http.StatusInternalServerError {
		t.Fatalf("typed nil status = %d, want 500", got)
	}

	var codeMsg *CodeMsg
	if errors.As(err, &codeMsg) {
		t.Fatal("StatusError must not be rendered as a business CodeMsg")
	}
}

func TestNewCodeErrorCarriesCodeAndMessage(t *testing.T) {
	err := NewCodeError(4001, "invalid order")

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

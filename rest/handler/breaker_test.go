package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chihqiang/ten-go/core/breaker"
)

type fakePromise struct {
	accepted bool
	rejected string
}

func (p *fakePromise) Accept() { p.accepted = true }

func (p *fakePromise) Reject(reason string) { p.rejected = reason }

type fakeBreaker struct {
	promise *fakePromise
	blocked bool
}

func (b *fakeBreaker) Name() string { return "fake" }

func (b *fakeBreaker) Allow() (breaker.Promise, error) { return b.AllowCtx(context.Background()) }

func (b *fakeBreaker) AllowCtx(context.Context) (breaker.Promise, error) {
	if b.blocked {
		return nil, breaker.ErrServiceUnavailable
	}
	return b.promise, nil
}

func (b *fakeBreaker) Do(req func() error) error { return b.DoCtx(context.Background(), req) }

func (b *fakeBreaker) DoCtx(context.Context, func() error) error { return nil }

func (b *fakeBreaker) DoWithAcceptable(func() error, breaker.Acceptable) error { return nil }

func (b *fakeBreaker) DoWithAcceptableCtx(context.Context, func() error, breaker.Acceptable) error {
	return nil
}

func (b *fakeBreaker) DoWithFallback(func() error, breaker.Fallback) error { return nil }

func (b *fakeBreaker) DoWithFallbackCtx(context.Context, func() error, breaker.Fallback) error {
	return nil
}

func (b *fakeBreaker) DoWithFallbackAcceptable(func() error, breaker.Fallback, breaker.Acceptable) error {
	return nil
}

func (b *fakeBreaker) DoWithFallbackAcceptableCtx(
	context.Context, func() error, breaker.Fallback, breaker.Acceptable,
) error {
	return nil
}

func TestBreakerHandlerAcceptsSuccessfulResponse(t *testing.T) {
	br := &fakeBreaker{promise: &fakePromise{}}
	handlerFn := NewBreakerHandler(br).Handle(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handlerFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !br.promise.accepted {
		t.Fatal("successful response was not reported as success")
	}
}

func TestBreakerHandlerRejectsServerError(t *testing.T) {
	br := &fakeBreaker{promise: &fakePromise{}}
	handlerFn := NewBreakerHandler(br).Handle(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})

	handlerFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if br.promise.rejected != "http status 502" {
		t.Fatalf("rejection = %q, want http status 502", br.promise.rejected)
	}
}

func TestBreakerHandlerFailsFastWhenOpen(t *testing.T) {
	called := false
	handlerFn := NewBreakerHandler(&fakeBreaker{blocked: true}).Handle(func(http.ResponseWriter, *http.Request) {
		called = true
	})

	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if called {
		t.Fatal("request reached the handler while the breaker was open")
	}
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}

func TestBreakerHandlerRejectsPanickingRequest(t *testing.T) {
	br := &fakeBreaker{promise: &fakePromise{}}
	handlerFn := NewBreakerHandler(br).Handle(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})

	func() {
		defer func() {
			if recovered := recover(); recovered != "boom" {
				t.Fatalf("panic = %v, want boom", recovered)
			}
		}()
		handlerFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}()

	if br.promise.accepted {
		t.Fatal("panicking request must not be accepted")
	}
	if br.promise.rejected == "" {
		t.Fatal("panicking request did not reject the breaker promise")
	}
}

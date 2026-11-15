package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTimeoutPassesThroughCompletedResponse(t *testing.T) {
	handler := TimeoutHandler(time.Second)(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Done", "yes")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusCreated || recorder.Body.String() != "ok" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("X-Done"); got != "yes" {
		t.Fatalf("header = %q, want yes", got)
	}
}

func TestTimeoutDiscardsSlowHandlerOutput(t *testing.T) {
	release := make(chan struct{})
	handler := TimeoutHandler(10 * time.Millisecond)(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		_, _ = w.Write([]byte("too late"))
		close(release)
	})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "{\"code\":503,\"msg\":\"request timed out\"}\n" {
		t.Fatalf("body = %q", body)
	}
	<-release
}

func TestTimeoutCancelsHandlerContext(t *testing.T) {
	canceled := make(chan error, 1)
	handler := TimeoutHandler(10 * time.Millisecond)(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		canceled <- r.Context().Err()
	})

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	select {
	case err := <-canceled:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("context error = %v, want deadline exceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("handler context was not canceled")
	}
}

func TestTimeoutRecoversHandlerPanicOnCallerGoroutine(t *testing.T) {
	handler := RecoverHandler(TimeoutHandler(time.Second)(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
}

func TestTimeoutPropagatesHandlerPanic(t *testing.T) {
	handler := TimeoutHandler(time.Second)(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})

	defer func() {
		if recovered := recover(); recovered != "boom" {
			t.Fatalf("panic = %v, want boom", recovered)
		}
	}()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestTimeoutContainsPanicAfterTimeout(t *testing.T) {
	returned := make(chan struct{})
	handler := TimeoutHandler(5 * time.Millisecond)(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(returned)
		panic("late boom")
	})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}

	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("handler did not run after the timeout")
	}
	// Give the worker goroutine a chance to panic; a regression crashes the
	// test process instead of failing an assertion.
	time.Sleep(20 * time.Millisecond)
}

func TestTimeoutNonPositiveDisablesMiddleware(t *testing.T) {
	called := false
	handler := TimeoutHandler(0)(func(http.ResponseWriter, *http.Request) {
		called = true
	})

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !called {
		t.Fatal("handler was not called")
	}
}

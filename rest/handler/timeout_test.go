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

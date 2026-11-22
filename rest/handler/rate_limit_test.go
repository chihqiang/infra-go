package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chihqiang/ten-go/core/ratelimit"
)

func TestRateLimitRejectsAfterBurst(t *testing.T) {
	handlerFn := NewRateLimitPerSecondHandler(1, 2).Handle(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	statuses := make([]int, 0, 3)
	for i := 0; i < 3; i++ {
		recorder := httptest.NewRecorder()
		handlerFn.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		statuses = append(statuses, recorder.Code)
	}

	want := []int{http.StatusOK, http.StatusOK, http.StatusTooManyRequests}
	for i, status := range statuses {
		if status != want[i] {
			t.Fatalf("request %d status = %d, want %d", i, status, want[i])
		}
	}
}

func TestRateLimitNilLimiterPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a nil limiter should panic at construction")
		}
	}()
	NewRateLimitHandler(nil)
}

func TestMaxConnsRejectsWhenSaturated(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	handlerFn := NewMaxConnsHandler(MaxConnsConfig{Enabled: true, Limit: 1}).Handle(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusOK)
	})

	go handlerFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	<-entered

	rejected := httptest.NewRecorder()
	handlerFn.ServeHTTP(rejected, httptest.NewRequest(http.MethodGet, "/", nil))
	if rejected.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rejected.Code)
	}

	close(release)
}

func TestMaxConnsReleasesSlot(t *testing.T) {
	guard := ratelimit.NewConcurrency(1)
	if !guard.Allow() {
		t.Fatal("first request should be allowed")
	}
	guard.Release()
	if !guard.Allow() {
		t.Fatal("slot was not released")
	}
	guard.Release()

	done := make(chan int, 1)
	handlerFn := NewMaxConnsHandler(MaxConnsConfig{Enabled: true, Limit: 1}).Handle(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	go func() {
		recorder := httptest.NewRecorder()
		handlerFn.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		done <- recorder.Code
	}()

	select {
	case status := <-done:
		if status != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", status)
		}
	case <-time.After(time.Second):
		t.Fatal("request did not complete")
	}
}

type suggestingLimiter struct {
	delay time.Duration
	calls int
}

func (l *suggestingLimiter) Allow() bool { return l.AllowContextForTest() }

func (l *suggestingLimiter) AllowContextForTest() bool {
	l.calls++
	return l.calls == 1
}

func (l *suggestingLimiter) AllowContext(context.Context) (bool, error) {
	return l.AllowContextForTest(), nil
}

func (l *suggestingLimiter) RetryAfter() time.Duration { return l.delay }

func TestRateLimitHandlerSetsRetryAfterFromLimiter(t *testing.T) {
	handlerFn := NewRateLimitHandler(&suggestingLimiter{delay: 2500 * time.Millisecond}).Handle(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
	)

	first := httptest.NewRecorder()
	handlerFn.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200", first.Code)
	}

	second := httptest.NewRecorder()
	handlerFn.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", second.Code)
	}
	if got := second.Header().Get("Retry-After"); got != "3" {
		t.Fatalf("Retry-After = %q, want 3 (rounded up)", got)
	}
}

func TestRateLimitHandlerOmitsRetryAfterWithoutHint(t *testing.T) {
	slowBucket := ratelimit.NewTokenBucket(1, 1)
	handlerFn := NewRateLimitHandler(slowBucket).Handle(func(w http.ResponseWriter, _ *http.Request) {})

	handlerFn.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
	if got := recorder.Header().Get("Retry-After"); got == "" {
		t.Fatal("token bucket implements RetryAfter, header should be present")
	}
}

func TestRetryAfterSecondsNeverZero(t *testing.T) {
	handler := NewRateLimitPerSecondHandler(1, 1)
	if got := handler.retryAfterSeconds(150 * time.Millisecond); got != 1 {
		t.Fatalf("retryAfterSeconds(150ms) = %d, want 1", got)
	}
	if got := handler.retryAfterSeconds(2500 * time.Millisecond); got != 3 {
		t.Fatalf("retryAfterSeconds(2.5s) = %d, want 3", got)
	}
}

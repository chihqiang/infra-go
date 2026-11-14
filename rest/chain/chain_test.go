package chain

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestChain_OrderAndImmutability(t *testing.T) {
	var got []string
	wrap := func(name string) Middleware {
		return func(next http.HandlerFunc) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				got = append(got, name+" before")
				next(w, r)
				got = append(got, name+" after")
			}
		}
	}

	base := New(wrap("a"))
	extended := base.Append(wrap("b")).Prepend(wrap("c"))
	handler := extended.ThenFunc(func(http.ResponseWriter, *http.Request) {
		got = append(got, "handler")
	})
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	want := []string{"c before", "a before", "b before", "handler", "b after", "a after", "c after"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("middleware order = %v, want %v", got, want)
	}

	got = nil
	base.ThenFunc(func(http.ResponseWriter, *http.Request) {
		got = append(got, "handler")
	}).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !reflect.DeepEqual(got, []string{"a before", "handler", "a after"}) {
		t.Fatalf("base chain changed after extension: %v", got)
	}
}

func TestChain_ThenFuncWithNilHandler(t *testing.T) {
	handler := New().ThenFunc(nil)
	if handler == nil {
		t.Fatal("ThenFunc(nil) returned nil")
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code == 0 {
		t.Fatal("fallback handler wrote no status")
	}
}

func TestChain_SkipsNilMiddleware(t *testing.T) {
	called := false
	handler := New(nil, func(next http.HandlerFunc) http.HandlerFunc {
		return next
	}, nil).ThenFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !called {
		t.Fatal("handler was not called with nil middlewares in the chain")
	}
}

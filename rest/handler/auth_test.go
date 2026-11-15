package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chihqiang/infra-go/rest/token"
)

func newTokenParser(t *testing.T) *token.Parser {
	t.Helper()

	parser, err := token.NewParser(token.Config{Secret: "a-sufficiently-long-secret"})
	if err != nil {
		t.Fatal(err)
	}
	return parser
}

func TestAuthorizeStoresClaims(t *testing.T) {
	parser := newTokenParser(t)
	signed, err := parser.Generate("user-1", map[string]any{"scope": "read"})
	if err != nil {
		t.Fatal(err)
	}

	handlerFn := Authorize(parser)(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := token.ClaimsFrom(r.Context())
		if !ok {
			t.Error("claims missing from context")
			return
		}
		scope, _ := claims.Data["scope"].(string)
		_, _ = w.Write([]byte(claims.Subject + ":" + scope))
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(Header, "Bearer "+signed)
	recorder := httptest.NewRecorder()

	handlerFn.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || recorder.Body.String() != "user-1:read" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestAuthorizeRejectsBadHeaders(t *testing.T) {
	parser := newTokenParser(t)
	called := false
	handlerFn := Authorize(parser)(func(http.ResponseWriter, *http.Request) {
		called = true
	})

	for _, header := range []string{"", "Basic abc", "Bearer ", "Bearer not-a-token"} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		if header != "" {
			request.Header.Set(Header, header)
		}
		recorder := httptest.NewRecorder()
		handlerFn.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("header %q: status = %d, want 401", header, recorder.Code)
		}
		if got := recorder.Header().Get("WWW-Authenticate"); got == "" {
			t.Errorf("header %q: missing WWW-Authenticate", header)
		}
	}

	if called {
		t.Fatal("rejected requests must not reach the next handler")
	}
}

func TestAuthorizeAcceptsLowercaseScheme(t *testing.T) {
	parser := newTokenParser(t)
	signed, err := parser.Generate("user-1", nil)
	if err != nil {
		t.Fatal(err)
	}

	handlerFn := Authorize(parser)(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(Header, "bearer "+signed)
	recorder := httptest.NewRecorder()

	handlerFn.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
}

func TestAuthorizeUsesCallback(t *testing.T) {
	parser := newTokenParser(t)
	handlerFn := Authorize(parser, WithUnauthorizedCallback(
		func(w http.ResponseWriter, r *http.Request, err error) {
			w.Header().Set("X-Reason", err.Error())
			w.WriteHeader(http.StatusForbidden)
		},
	))(func(http.ResponseWriter, *http.Request) {})

	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
	if got := recorder.Header().Get("X-Reason"); got == "" {
		t.Fatal("callback did not receive the failure")
	}
}

func TestAuthorizeWithoutParserRejects(t *testing.T) {
	handlerFn := Authorize(nil)(func(http.ResponseWriter, *http.Request) {})

	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

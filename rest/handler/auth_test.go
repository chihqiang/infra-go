package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

	handlerFn := NewAuthHandler(AuthConfig{Enabled: true, Parser: parser}).Handle(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := token.ClaimsFrom(r.Context())
		if !ok {
			t.Error("claims missing from context")
			return
		}
		scope, _ := claims.Data["scope"].(string)
		_, _ = w.Write([]byte(claims.Subject + ":" + scope))
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(AuthorizationHeader, "Bearer "+signed)
	recorder := httptest.NewRecorder()

	handlerFn.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || recorder.Body.String() != "user-1:read" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestNewAuthHandlerBuildsParserFromTokenConfig(t *testing.T) {
	parser := newTokenParser(t)
	signed, err := parser.Generate("user-1", nil)
	if err != nil {
		t.Fatal(err)
	}

	config := AuthConfig{Enabled: true}
	config.Secret = "a-sufficiently-long-secret"
	handlerFn := NewAuthHandler(config).Handle(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(AuthorizationHeader, "Bearer "+signed)
	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
}

func TestNewAuthHandlerPanicsOnInvalidTokenConfig(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("invalid token config should panic")
		}
	}()
	NewAuthHandler(AuthConfig{Enabled: true, Config: token.Config{Secret: "short"}})
}

func TestAuthHandlerSkipsConfiguredPaths(t *testing.T) {
	parser := newTokenParser(t)
	handlerFn := NewAuthHandler(AuthConfig{
		Enabled:   true,
		Parser:    parser,
		SkipPaths: []string{"/health", "/public/*"},
	}).Handle(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	for _, path := range []string{"/health", "/public/info"} {
		recorder := httptest.NewRecorder()
		handlerFn.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNoContent {
			t.Errorf("%s: status = %d, want 204", path, recorder.Code)
		}
	}

	protected := httptest.NewRecorder()
	handlerFn.ServeHTTP(protected, httptest.NewRequest(http.MethodGet, "/private", nil))
	if protected.Code != http.StatusUnauthorized {
		t.Fatalf("protected status = %d, want 401", protected.Code)
	}
}

func TestAuthorizeRejectsBadHeaders(t *testing.T) {
	parser := newTokenParser(t)
	called := false
	handlerFn := NewAuthHandler(AuthConfig{Enabled: true, Parser: parser}).Handle(func(http.ResponseWriter, *http.Request) {
		called = true
	})

	tests := []struct {
		name        string
		header      string
		status      int
		challenge   []string
		noErrorAuth bool
	}{
		{
			name:        "missing credentials",
			status:      http.StatusUnauthorized,
			noErrorAuth: true,
		},
		{
			name:        "unsupported scheme",
			header:      "Basic abc",
			status:      http.StatusUnauthorized,
			noErrorAuth: true,
		},
		{
			name:        "bearer-like unsupported scheme",
			header:      "Bearer2 abc",
			status:      http.StatusUnauthorized,
			noErrorAuth: true,
		},
		{
			name:        "bearer prefix without separator",
			header:      "Bearerfoo abc",
			status:      http.StatusUnauthorized,
			noErrorAuth: true,
		},
		{
			name:      "empty bearer credentials",
			header:    "Bearer ",
			status:    http.StatusBadRequest,
			challenge: []string{`Bearer realm="api"`, `error="invalid_request"`},
		},
		{
			name:      "invalid token",
			header:    "Bearer not-a-token",
			status:    http.StatusUnauthorized,
			challenge: []string{`Bearer realm="api"`, `error="invalid_token"`},
		},
	}

	for _, test := range tests {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		if test.header != "" {
			request.Header.Set(AuthorizationHeader, test.header)
		}
		recorder := httptest.NewRecorder()
		handlerFn.ServeHTTP(recorder, request)

		if recorder.Code != test.status {
			t.Errorf("%s: status = %d, want %d", test.name, recorder.Code, test.status)
		}
		challenge := recorder.Header().Get("WWW-Authenticate")
		if challenge == "" {
			t.Errorf("%s: missing WWW-Authenticate", test.name)
		}
		for _, want := range test.challenge {
			if !strings.Contains(challenge, want) {
				t.Errorf("%s: challenge = %q, want %q", test.name, challenge, want)
			}
		}
		if test.noErrorAuth && strings.Contains(challenge, "error=") {
			t.Errorf("%s: challenge must not carry error information: %q", test.name, challenge)
		}
	}

	if called {
		t.Fatal("rejected requests must not reach the next handler")
	}
}

func TestAuthorizeRejectsMultipleAuthorizationHeaders(t *testing.T) {
	parser := newTokenParser(t)
	handlerFn := NewAuthHandler(AuthConfig{Enabled: true, Parser: parser}).Handle(func(http.ResponseWriter, *http.Request) {})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Add(AuthorizationHeader, "Bearer first")
	request.Header.Add(AuthorizationHeader, "Bearer second")
	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if got := recorder.Header().Get("WWW-Authenticate"); !strings.Contains(got, `error="invalid_request"`) {
		t.Fatalf("challenge = %q, want invalid_request", got)
	}
}

func TestAuthorizeAcceptsLowercaseScheme(t *testing.T) {
	parser := newTokenParser(t)
	signed, err := parser.Generate("user-1", nil)
	if err != nil {
		t.Fatal(err)
	}

	handlerFn := NewAuthHandler(AuthConfig{Enabled: true, Parser: parser}).Handle(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(AuthorizationHeader, "bearer "+signed)
	recorder := httptest.NewRecorder()

	handlerFn.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
}

func TestAuthorizeUsesCallback(t *testing.T) {
	parser := newTokenParser(t)
	handlerFn := NewAuthHandler(AuthConfig{
		Enabled: true,
		Parser:  parser,
		Callback: func(w http.ResponseWriter, r *http.Request, err error) {
			w.Header().Set("X-Reason", err.Error())
			w.WriteHeader(http.StatusForbidden)
		},
	}).Handle(func(http.ResponseWriter, *http.Request) {})

	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
	if got := recorder.Header().Get("X-Reason"); got == "" {
		t.Fatal("callback did not receive the failure")
	}
}

func TestAuthorizeWithoutParserReturnsServerError(t *testing.T) {
	handlerFn := NewAuthHandler(AuthConfig{Enabled: true}).Handle(func(http.ResponseWriter, *http.Request) {})

	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if got := recorder.Header().Get("WWW-Authenticate"); got != "" {
		t.Fatalf("challenge = %q, want none for a server error", got)
	}
}

func TestAuthorizeCallbackDefaultsStatus(t *testing.T) {
	parser := newTokenParser(t)
	handlerFn := NewAuthHandler(AuthConfig{
		Enabled: true,
		Parser:  parser,
		Callback: func(w http.ResponseWriter, _ *http.Request, err error) {
			w.Header().Set("X-Reason", err.Error())
		},
	}).Handle(func(http.ResponseWriter, *http.Request) {})

	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 when the callback does not set one", recorder.Code)
	}
	if got := recorder.Header().Get("X-Reason"); got == "" {
		t.Fatal("callback header was not applied")
	}
}

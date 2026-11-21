package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chihqiang/infra-go/rest/token"
)

// authHandlerSecret matches the secret newTokenParser signs with, so a handler
// built from the secret verifies those tokens.
const authHandlerSecret = "a-sufficiently-long-secret"

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

	handlerFn := NewAuthHandler(AuthConfig{Secret: authHandlerSecret}).Handle(func(w http.ResponseWriter, r *http.Request) {
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

func TestNewAuthHandlerBuildsParserFromSecret(t *testing.T) {
	parser := newTokenParser(t)
	signed, err := parser.Generate("user-1", nil)
	if err != nil {
		t.Fatal(err)
	}

	handlerFn := NewAuthHandler(AuthConfig{Secret: authHandlerSecret}).Handle(func(w http.ResponseWriter, _ *http.Request) {
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

func TestNewAuthHandlerPanicsOnShortSecret(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a secret shorter than 8 characters should panic")
		}
	}()
	NewAuthHandler(AuthConfig{Secret: "short"})
}

func TestAuthHandlerSkipsConfiguredPaths(t *testing.T) {
	handlerFn := NewAuthHandler(AuthConfig{Secret: authHandlerSecret},
		WithAuthSkipPaths("/health", "/public/*"),
	).Handle(func(w http.ResponseWriter, _ *http.Request) {
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

	// Dot segments are cleaned before matching, so they cannot widen a rule.
	bypass := httptest.NewRecorder()
	handlerFn.ServeHTTP(bypass, httptest.NewRequest(http.MethodGet, "/public/../private", nil))
	if bypass.Code != http.StatusUnauthorized {
		t.Fatalf("dot-segment bypass status = %d, want 401", bypass.Code)
	}
}

func TestAuthorizeRejectsBadHeaders(t *testing.T) {
	called := false
	handlerFn := NewAuthHandler(AuthConfig{Secret: authHandlerSecret}).Handle(func(http.ResponseWriter, *http.Request) {
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
	handlerFn := NewAuthHandler(AuthConfig{Secret: authHandlerSecret}).Handle(func(http.ResponseWriter, *http.Request) {})

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

	handlerFn := NewAuthHandler(AuthConfig{Secret: authHandlerSecret}).Handle(func(w http.ResponseWriter, _ *http.Request) {
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
	handlerFn := NewAuthHandler(AuthConfig{Secret: authHandlerSecret}, WithUnauthorizedCallback(
		func(w http.ResponseWriter, r *http.Request, err error) {
			w.Header().Set("X-Reason", err.Error())
			w.WriteHeader(http.StatusForbidden)
		},
	)).Handle(func(http.ResponseWriter, *http.Request) {})

	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
	if got := recorder.Header().Get("X-Reason"); got == "" {
		t.Fatal("callback did not receive the failure")
	}
}

func TestAuthHandlerAcceptsPreviousSecret(t *testing.T) {
	oldParser, err := token.NewParser(token.Config{Secret: "the-previous-secret"})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := oldParser.Generate("user-1", nil)
	if err != nil {
		t.Fatal(err)
	}

	handlerFn := NewAuthHandler(AuthConfig{Secret: authHandlerSecret},
		WithPrevSecret("the-previous-secret")).Handle(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(AuthorizationHeader, "Bearer "+signed)
	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 for a token signed with the previous secret", recorder.Code)
	}
}

func TestNewAuthHandlerRequiresSecret(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("auth without a secret should panic")
		}
	}()
	NewAuthHandler(AuthConfig{})
}

func TestAuthorizeCallbackDefaultsStatus(t *testing.T) {
	handlerFn := NewAuthHandler(AuthConfig{Secret: authHandlerSecret}, WithUnauthorizedCallback(
		func(w http.ResponseWriter, _ *http.Request, err error) {
			w.Header().Set("X-Reason", err.Error())
		},
	)).Handle(func(http.ResponseWriter, *http.Request) {})

	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 when the callback does not set one", recorder.Code)
	}
	if got := recorder.Header().Get("X-Reason"); got == "" {
		t.Fatal("callback header was not applied")
	}
}

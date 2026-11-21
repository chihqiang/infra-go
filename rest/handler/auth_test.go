package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"

	"github.com/chihqiang/ten-go/rest/token"
)

// authHandlerSecret matches the secret newTokenParser signs with, so a handler
// built from the secret verifies those tokens.
const authHandlerSecret = "a-sufficiently-long-secret"

// signSubject mints a token for subject through parser.
func signSubject(t *testing.T, parser *token.Parser, subject string) string {
	t.Helper()

	signed, err := parser.Generate(jwtlib.MapClaims{
		"sub": subject,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func newTokenParser(t *testing.T) *token.Parser {
	t.Helper()

	parser := token.NewParser(token.Config{Secret: "a-sufficiently-long-secret"})
	return parser
}

func TestAuthorizeStoresClaims(t *testing.T) {
	parser := newTokenParser(t)
	signed, err := parser.Generate(jwtlib.MapClaims{
		"sub":   "user-1",
		"uid":   "user-1",
		"scope": "read",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}

	handlerFn := NewAuthHandler(AuthConfig{Secret: authHandlerSecret}).Handle(func(w http.ResponseWriter, r *http.Request) {
		uid, _ := token.Claim(r.Context(), "uid")
		scope, _ := token.Claim(r.Context(), "scope")
		_, _ = w.Write([]byte(uid.(string) + ":" + scope.(string)))
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
	signed := signSubject(t, parser, "user-1")

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
		name   string
		header string
	}{
		{name: "missing credentials"},
		{name: "unsupported scheme", header: "Basic abc"},
		{name: "bearer-like unsupported scheme", header: "Bearer2 abc"},
		{name: "bearer prefix without separator", header: "Bearerfoo abc"},
		{name: "empty bearer credentials", header: "Bearer "},
		{name: "invalid token", header: "Bearer not-a-token"},
	}

	for _, test := range tests {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		if test.header != "" {
			request.Header.Set(AuthorizationHeader, test.header)
		}
		recorder := httptest.NewRecorder()
		handlerFn.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", test.name, recorder.Code)
		}
		if challenge := recorder.Header().Get("WWW-Authenticate"); challenge != "" {
			t.Errorf("%s: WWW-Authenticate = %q, want none", test.name, challenge)
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

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if got := recorder.Header().Get("WWW-Authenticate"); got != "" {
		t.Fatalf("WWW-Authenticate = %q, want none", got)
	}
}

func TestAuthorizeAcceptsLowercaseScheme(t *testing.T) {
	parser := newTokenParser(t)
	signed := signSubject(t, parser, "user-1")

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
	oldParser := token.NewParser(token.Config{Secret: "the-previous-secret"})
	signed := signSubject(t, oldParser, "user-1")

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

func TestAuthHandlerUsesConfigTokenExtractor(t *testing.T) {
	parser := newTokenParser(t)
	signed := signSubject(t, parser, "user-1")

	handlerFn := NewAuthHandler(AuthConfig{
		Secret: authHandlerSecret,
		TokenExtractor: func(r *http.Request) (string, error) {
			if value := r.Header.Get("X-Auth-Token"); value != "" {
				return value, nil
			}
			return "", token.ErrMissingToken
		},
	}).Handle(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Auth-Token", signed)
	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
}

func TestAuthHandlerUsesWithTokenExtractor(t *testing.T) {
	parser := newTokenParser(t)
	signed := signSubject(t, parser, "user-2")

	handlerFn := NewAuthHandler(
		AuthConfig{
			Secret: authHandlerSecret,
			// The option must win over the config value.
			TokenExtractor: func(*http.Request) (string, error) {
				return "", token.ErrMissingToken
			},
		},
		WithTokenExtractor(func(r *http.Request) (string, error) {
			if value := r.URL.Query().Get("access_token"); value != "" {
				return value, nil
			}
			return "", token.ErrMissingToken
		}),
	).Handle(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/?access_token="+signed, nil)
	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
}

func TestAuthHandlerNilTokenExtractorFallsBackToBearer(t *testing.T) {
	parser := newTokenParser(t)
	signed := signSubject(t, parser, "user-3")

	handlerFn := NewAuthHandler(
		AuthConfig{Secret: authHandlerSecret},
		WithTokenExtractor(nil),
	).Handle(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(AuthorizationHeader, "Bearer "+signed)
	recorder := httptest.NewRecorder()
	handlerFn.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 when the extractor is nil", recorder.Code)
	}
}

func TestAuthHandlerTokenExtractorErrors(t *testing.T) {
	tests := []struct {
		name      string
		extractor TokenExtractor
	}{
		{
			name:      "missing token",
			extractor: func(*http.Request) (string, error) { return "", token.ErrMissingToken },
		},
		{
			name:      "malformed token",
			extractor: func(*http.Request) (string, error) { return "", ErrMalformedAuthorization },
		},
		{
			name:      "custom error",
			extractor: func(*http.Request) (string, error) { return "", errors.New("token not found") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlerFn := NewAuthHandler(
				AuthConfig{Secret: authHandlerSecret},
				WithTokenExtractor(tt.extractor),
			).Handle(func(http.ResponseWriter, *http.Request) {})

			recorder := httptest.NewRecorder()
			handlerFn.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", recorder.Code)
			}
		})
	}
}

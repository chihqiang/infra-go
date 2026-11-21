package rest

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/chihqiang/infra-go/rest/chain"
	"github.com/chihqiang/infra-go/rest/handler"
	"github.com/chihqiang/infra-go/rest/token"
)

func TestServerRouteRegistrationAndGroups(t *testing.T) {
	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}

	server.AddRoutes(Route{
		Method: http.MethodGet,
		Path:   "/health",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("ok"))
		}),
	})
	server.Group("/api").Group("v1").HandleFunc(http.MethodGet, "/users/{id}",
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(r.PathValue("id")))
		})

	tests := []struct {
		path string
		want string
	}{
		{path: "/health", want: "ok"},
		{path: "/api/v1/users/42", want: "42"},
	}

	for _, test := range tests {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
		if recorder.Code != http.StatusOK || recorder.Body.String() != test.want {
			t.Errorf("%s: got (%d, %q), want (%d, %q)",
				test.path, recorder.Code, recorder.Body.String(), http.StatusOK, test.want)
		}
	}
}

func TestRouteGroupMiddlewareInheritance(t *testing.T) {
	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}

	setHeader := func(name string) chain.Middleware {
		return func(next http.HandlerFunc) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(name, "applied")
				next(w, r)
			}
		}
	}

	parent := server.Group("/api")
	parent.Use(setHeader("X-Parent"))
	child := parent.Group("/v1")
	child.Use(setHeader("X-Child"))
	child.AddRoute(Route{
		Method:  http.MethodGet,
		Path:    "/users",
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	})
	parent.HandleFunc(http.MethodGet, "/status", func(http.ResponseWriter, *http.Request) {})
	server.HandleFunc(http.MethodGet, "/health", func(http.ResponseWriter, *http.Request) {})

	tests := []struct {
		path        string
		parentValue string
		childValue  string
	}{
		{path: "/api/v1/users", parentValue: "applied", childValue: "applied"},
		{path: "/api/status", parentValue: "applied"},
		{path: "/health"},
	}

	for _, test := range tests {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
		if got := recorder.Header().Get("X-Parent"); got != test.parentValue {
			t.Errorf("%s: parent middleware header = %q, want %q", test.path, got, test.parentValue)
		}
		if got := recorder.Header().Get("X-Child"); got != test.childValue {
			t.Errorf("%s: child middleware header = %q, want %q", test.path, got, test.childValue)
		}
	}
}

func TestWithRouteMiddlewareOnlyAffectsItsRoute(t *testing.T) {
	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}
	server.AddRoute(Route{
		Method: http.MethodGet,
		Path:   "/open",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("open"))
		}),
	}, WithRouteMiddleware(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Guarded", "yes")
			next(w, r)
		}
	}))

	guarded := httptest.NewRecorder()
	server.ServeHTTP(guarded, httptest.NewRequest(http.MethodGet, "/open", nil))
	if guarded.Header().Get("X-Guarded") != "yes" {
		t.Fatal("route middleware did not run")
	}

	other, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}
	other.AddRoute(Route{
		Method:  http.MethodGet,
		Path:    "/open",
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	})
	plain := httptest.NewRecorder()
	other.ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/open", nil))
	if plain.Header().Get("X-Guarded") != "" {
		t.Fatal("route middleware leaked to another server")
	}
}

func TestWithRouteAuthProtectsGroupRoute(t *testing.T) {
	parser, err := token.NewParser(token.Config{Secret: "a-sufficiently-long-secret"})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := parser.Generate("user-1", nil)
	if err != nil {
		t.Fatal(err)
	}

	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}
	server.Group("/api").AddRoute(Route{
		Method: http.MethodGet,
		Path:   "/profile",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, _ := token.ClaimsFrom(r.Context())
			_, _ = w.Write([]byte(claims.Subject))
		}),
	}, WithRouteAuth("a-sufficiently-long-secret"))

	anonymous := httptest.NewRecorder()
	server.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/api/profile", nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401", anonymous.Code)
	}

	authorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/profile", nil)
	request.Header.Set(handler.AuthorizationHeader, "Bearer "+signed)
	server.ServeHTTP(authorized, request)
	if authorized.Code != http.StatusOK || authorized.Body.String() != "user-1" {
		t.Fatalf("authorized response = %d %q", authorized.Code, authorized.Body.String())
	}
}

func TestWithRouteSignatureProtectsOnlySelectedRoute(t *testing.T) {
	key := []byte("signing-key")
	body := []byte(`{"amount":10}`)
	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}
	server.AddRoute(Route{
		Method: http.MethodPost,
		Path:   "/signed",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	}, WithRouteSignature([][]byte{key}))
	server.HandleFunc(http.MethodPost, "/open", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	unsigned := httptest.NewRecorder()
	server.ServeHTTP(unsigned, httptest.NewRequest(http.MethodPost, "/signed", bytes.NewReader(body)))
	if unsigned.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned route status = %d, want 401", unsigned.Code)
	}

	request := httptest.NewRequest(http.MethodPost, "/signed", bytes.NewReader(body))
	timestamp := time.Now().Unix()
	request.Header.Set(handler.HeaderTimestamp, strconv.FormatInt(timestamp, 10))
	request.Header.Set(handler.HeaderSignature, handler.SignRequest(key, timestamp, request, body))
	signed := httptest.NewRecorder()
	server.ServeHTTP(signed, request)
	if signed.Code != http.StatusNoContent {
		t.Fatalf("signed route status = %d, want 204", signed.Code)
	}

	open := httptest.NewRecorder()
	server.ServeHTTP(open, httptest.NewRequest(http.MethodPost, "/open", bytes.NewReader(body)))
	if open.Code != http.StatusNoContent {
		t.Fatalf("unprotected route status = %d, want 204", open.Code)
	}
}

func TestServerRoutesRecordsRegistrationOrder(t *testing.T) {
	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodGet, "/health", func(http.ResponseWriter, *http.Request) {})
	server.Group("/api").HandleFunc(http.MethodPost, "/users", func(http.ResponseWriter, *http.Request) {})

	routes := server.Routes()
	if len(routes) != 2 {
		t.Fatalf("routes = %d, want 2", len(routes))
	}
	if routes[0].Method != http.MethodGet || routes[0].Path != "/health" {
		t.Fatalf("first route = %s %s", routes[0].Method, routes[0].Path)
	}
	if routes[1].Path != "/api/users" {
		t.Fatalf("grouped route path = %q, want /api/users", routes[1].Path)
	}

	routes[0].Path = "/mutated"
	if server.Routes()[0].Path != "/health" {
		t.Fatal("Routes() must return a copy")
	}
}

func TestServerPrintRoutes(t *testing.T) {
	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodGet, "/health", func(http.ResponseWriter, *http.Request) {})
	server.Group("/api").HandleFunc(http.MethodPost, "/users", func(http.ResponseWriter, *http.Request) {})

	var buffer bytes.Buffer
	if err := server.PrintRoutes(&buffer); err != nil {
		t.Fatal(err)
	}

	output := buffer.String()
	if len(server.Routes()) != 2 {
		t.Fatalf("routes = %d, want 2", len(server.Routes()))
	}
	for _, want := range []string{"GET /health", "POST /api/users"} {
		if !bytes.Contains(buffer.Bytes(), []byte(want)) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestPrintRoutesHandlesNilWriter(t *testing.T) {
	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}
	server.HandleFunc(http.MethodGet, "/health", func(http.ResponseWriter, *http.Request) {})

	if err := server.PrintRoutes(nil); err != nil {
		t.Fatalf("PrintRoutes(nil) = %v", err)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
}

func TestRouteGroupAddRoutes(t *testing.T) {
	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}
	server.Group("/api").AddRoutes(
		Route{Method: http.MethodGet, Path: "/a", Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})},
		Route{Method: http.MethodGet, Path: "/b", Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})},
	)

	paths := make(map[string]bool)
	for _, route := range server.Routes() {
		paths[route.Path] = true
	}
	for _, want := range []string{"/api/a", "/api/b"} {
		if !paths[want] {
			t.Fatalf("missing grouped route %q", want)
		}
	}
}

func TestRouteGroupInheritsMiddlewareAddedAfterChildCreation(t *testing.T) {
	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}

	parent := server.Group("/api")
	child := parent.Group("/v1")
	parent.Use(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Late", "applied")
			next(w, r)
		}
	})
	child.HandleFunc(http.MethodGet, "/users", func(http.ResponseWriter, *http.Request) {})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/users", nil))
	if got := recorder.Header().Get("X-Late"); got != "applied" {
		t.Fatalf("late parent middleware header = %q, want applied", got)
	}
}

func TestRouteGroupRootMatchesTrailingSlash(t *testing.T) {
	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}
	server.Group("/api").HandleFunc(http.MethodGet, "/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("root"))
	})
	server.Group("/api/v1").AddRoute(Route{
		Method: http.MethodGet,
		Path:   "/",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("v1"))
		}),
	})
	server.Group("/trailing/").HandleFunc(http.MethodGet, "/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("trailing"))
	})

	for _, test := range []struct {
		path string
		body string
	}{
		{path: "/api", body: "root"},
		{path: "/api/", body: "root"},
		{path: "/api/v1", body: "v1"},
		{path: "/api/v1/", body: "v1"},
		{path: "/trailing", body: "trailing"},
		{path: "/trailing/", body: "trailing"},
	} {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
		if recorder.Code != http.StatusOK || recorder.Body.String() != test.body {
			t.Fatalf("%s: response = %d %q, want 200 %q",
				test.path, recorder.Code, recorder.Body.String(), test.body)
		}
	}

	// The alias is exact: unknown paths below the prefix must stay 404.
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/unknown", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("/api/unknown: status = %d, want 404", recorder.Code)
	}
}

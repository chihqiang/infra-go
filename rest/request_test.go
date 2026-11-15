package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chihqiang/infra-go/rest/handler"
)

func TestClientIPUsesDirectPeerByDefault(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.1:5678"
	request.Header.Set("X-Forwarded-For", "203.0.113.9")

	if got := ClientIP(request); got != "10.0.0.1" {
		t.Fatalf("client IP = %q, want the direct peer", got)
	}
	if got := ClientIPFromHeaders(request); got != "203.0.113.9" {
		t.Fatalf("legacy client IP = %q, want 203.0.113.9", got)
	}
}

func TestRequestIDReadsMiddlewareValue(t *testing.T) {
	var got string
	handler.RequestIDHandler(func(_ http.ResponseWriter, r *http.Request) {
		got = RequestID(r)
	}).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if got == "" {
		t.Fatal("request ID is empty after RequestIDHandler")
	}
	if RequestID(httptest.NewRequest(http.MethodGet, "/", nil)) != "" {
		t.Fatal("request ID should be empty without the middleware")
	}
}

type bindPayload struct {
	Name string `json:"name" form:"name" header:"X-Name" path:"name"`
}

func TestBindHelpersDecodeEachSource(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/?name=query", strings.NewReader(`{"name":"json"}`))
	request.Header.Set("Content-Type", "application/json")

	var fromJSON bindPayload
	if err := Bind(request, &fromJSON); err != nil {
		t.Fatal(err)
	}
	if fromJSON.Name != "json" {
		t.Fatalf("json bind name = %q, want json", fromJSON.Name)
	}

	get := httptest.NewRequest(http.MethodGet, "/?name=query", nil)
	var fromQuery bindPayload
	if err := BindQuery(get, &fromQuery); err != nil {
		t.Fatal(err)
	}
	if fromQuery.Name != "query" {
		t.Fatalf("query bind name = %q, want query", fromQuery.Name)
	}

	header := httptest.NewRequest(http.MethodGet, "/", nil)
	header.Header.Set("X-Name", "header")
	var fromHeader bindPayload
	if err := BindHeader(header, &fromHeader); err != nil {
		t.Fatal(err)
	}
	if fromHeader.Name != "header" {
		t.Fatalf("header bind name = %q, want header", fromHeader.Name)
	}

	pathRequest := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	pathRequest.SetPathValue("name", "path")
	var fromPath bindPayload
	if err := BindPath(pathRequest, &fromPath); err != nil {
		t.Fatal(err)
	}
	if fromPath.Name != "path" {
		t.Fatalf("path bind name = %q, want path", fromPath.Name)
	}
}

func TestBindFailureCarriesRequestStatus(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not json"))
	request.Header.Set("Content-Type", "application/json")

	err := Bind(request, &bindPayload{})
	if err == nil {
		t.Fatal("malformed body should fail to bind")
	}
	if got := StatusOf(err); got != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", got)
	}
}

func TestClientIPTrustedIgnoresSpoofedHeaders(t *testing.T) {
	trusted, err := ParseTrustedProxies([]string{"10.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "203.0.113.5:5678"
	request.Header.Set("X-Forwarded-For", "1.2.3.4")

	if got := ClientIPTrusted(request, trusted); got != "203.0.113.5" {
		t.Fatalf("client IP = %q, want the direct peer", got)
	}
}

func TestBindPathNestedStructWithRouteWildcard(t *testing.T) {
	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}

	type org struct {
		ID string `path:"org_id"`
	}
	var got string
	server.HandleFunc(http.MethodGet, "/orgs/{org_id}", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Org org `path:"org"`
		}
		if err := BindPath(r, &payload); err != nil {
			_ = Error(w, err)
			return
		}
		got = payload.Org.ID
	})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/orgs/42", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	if got != "42" {
		t.Fatalf("nested path id = %q, want 42", got)
	}
}

func TestBindAcceptsJSONWithoutJSONContentType(t *testing.T) {
	for _, contentType := range []string{
		"",
		"application/x-www-form-urlencoded",
		"application/vnd.api+json",
	} {
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"ada"}`))
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}

		var payload bindPayload
		if err := Bind(request, &payload); err != nil {
			t.Fatalf("content type %q: %v", contentType, err)
		}
		if payload.Name != "ada" {
			t.Fatalf("content type %q: name = %q", contentType, payload.Name)
		}
	}
}

func TestContextWithRequestID(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), "req-1")
	if got := RequestIDFromContext(ctx); got != "req-1" {
		t.Fatalf("request ID = %q, want req-1", got)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	if got := RequestID(request); got != "req-1" {
		t.Fatalf("RequestID = %q, want req-1", got)
	}

	if got := RequestIDFromContext(ContextWithRequestID(nil, "req-2")); got != "req-2" {
		t.Fatalf("nil context request ID = %q, want req-2", got)
	}
	if got := RequestIDFromContext(nil); got != "" {
		t.Fatalf("nil context request ID = %q, want empty", got)
	}
}

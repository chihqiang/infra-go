package rest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chihqiang/infra-go/rest/handler"
)

func TestClientIPPrefersProxyHeader(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.1:5678"
	request.Header.Set("X-Forwarded-For", "203.0.113.9")

	if got := ClientIP(request); got != "203.0.113.9" {
		t.Fatalf("client IP = %q, want 203.0.113.9", got)
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

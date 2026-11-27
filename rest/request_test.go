package rest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

func TestPathParam(t *testing.T) {
	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}

	var got string
	server.HandleFunc(http.MethodGet, "/groups/{id}", func(_ http.ResponseWriter, r *http.Request) {
		got = PathParam(r, "id")
	})
	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/groups/42", nil))

	if got != "42" {
		t.Fatalf("path param = %q, want 42", got)
	}
	if PathParam(nil, "id") != "" {
		t.Fatal("path param for nil request should be empty")
	}
}

func TestQueryParam(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/groups?page=2&page=3", nil)
	if got := QueryParam(request, "page"); got != "2" {
		t.Fatalf("query param = %q, want first value 2", got)
	}
	if got := QueryParam(request, "missing"); got != "" {
		t.Fatalf("missing query param = %q, want empty", got)
	}
	if QueryParam(nil, "page") != "" {
		t.Fatal("query param for nil request should be empty")
	}
}

func TestPathParamValue(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/groups/42", nil)
	request.SetPathValue("id", "42")

	id, err := PathParamValue(request, "id").Int()
	if err != nil {
		t.Fatal(err)
	}
	if id != 42 {
		t.Fatalf("path param value = %d, want 42", id)
	}

	if !PathParamValue(request, "missing").IsEmpty() {
		t.Fatal("missing path param value should be empty")
	}
	if !PathParamValue(nil, "id").IsEmpty() {
		t.Fatal("path param value for nil request should be empty")
	}
}

func TestQueryParamValue(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/groups?page=2&page=3", nil)

	page, err := QueryParamValue(request, "page").Int()
	if err != nil {
		t.Fatal(err)
	}
	if page != 2 {
		t.Fatalf("query param value = %d, want first value 2", page)
	}

	if !QueryParamValue(request, "missing").IsEmpty() {
		t.Fatal("missing query param value should be empty")
	}
	if !QueryParamValue(nil, "page").IsEmpty() {
		t.Fatal("query param value for nil request should be empty")
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
			_ = ErrorJSON(w, err)
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

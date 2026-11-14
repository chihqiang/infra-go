package binding

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type jsonPayload struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

type xmlPayload struct {
	Name string `xml:"name"`
}

type formPayload struct {
	Name   string   `form:"name"`
	Limit  int      `form:"limit,optional"`
	Tags   []string `form:"tag,optional"`
	Active bool     `form:"active,optional"`
}

type headerPayload struct {
	Token string `header:"X-Token"`
	Agent string `header:"User-Agent,optional"`
}

type pathPayload struct {
	ID     string `path:"id"`
	Action string `path:"action,optional"`
}

func TestBinderSelectsByContentType(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		contentType string
		body        string
		want        Binding
	}{
		{name: "json", method: http.MethodPost, contentType: "application/json; charset=utf-8", body: `{}`, want: JSON},
		{name: "xml", method: http.MethodPost, contentType: "application/xml", body: `<x/>`, want: XML},
		{name: "form", method: http.MethodPost, contentType: "application/x-www-form-urlencoded", body: "a=1", want: Form},
		{name: "body without content type uses form", method: http.MethodPost, body: "a=1", want: Form},
		{name: "no body uses query", method: http.MethodGet, want: Query},
	}

	for _, test := range tests {
		req := httptest.NewRequest(test.method, "/?a=1", strings.NewReader(test.body))
		if test.contentType != "" {
			req.Header.Set("Content-Type", test.contentType)
		}
		if test.body == "" {
			req.Body = http.NoBody
		}
		if got := Binder(req); got != test.want {
			t.Errorf("%s: binder = %T, want %T", test.name, got, test.want)
		}
	}
}

func TestBindRejectsUnsupportedMediaType(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"a":1}`))
	req.Header.Set("Content-Type", "text/plain")

	err := Bind(req, &jsonPayload{})
	var bindingErr *Error
	if !errors.As(err, &bindingErr) {
		t.Fatalf("error = %v, want *binding.Error", err)
	}
	if bindingErr.StatusCode() != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", bindingErr.StatusCode())
	}
	if !errors.Is(err, ErrUnsupportedMediaType) {
		t.Fatalf("error = %v, want ErrUnsupportedMediaType", err)
	}
}

func TestJSONBind(t *testing.T) {
	var payload jsonPayload
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"name":"ada","age":36}`))
	req.Header.Set("Content-Type", "application/json")

	if err := Bind(req, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Name != "ada" || payload.Age != 36 {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestJSONBindRejectsMalformedEmptyAndTrailingBody(t *testing.T) {
	for _, body := range []string{`{"name":`, "", `{"name":"a"}{"name":"b"}`} {
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if err := Bind(req, &jsonPayload{}); err == nil {
			t.Errorf("body %q should fail to bind", body)
		}
	}
}

func TestXMLBind(t *testing.T) {
	var payload xmlPayload
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`<xmlPayload><name>ada</name></xmlPayload>`))
	req.Header.Set("Content-Type", "text/xml")

	if err := Bind(req, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Name != "ada" {
		t.Fatalf("name = %q, want ada", payload.Name)
	}
}

func TestQueryBindUsesFormTags(t *testing.T) {
	var payload formPayload
	req := httptest.NewRequest(http.MethodGet, "/?name=ada&limit=5&tag=go&tag=http", nil)

	if err := Bind(req, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Name != "ada" || payload.Limit != 5 {
		t.Fatalf("payload = %+v", payload)
	}
	if len(payload.Tags) != 2 || payload.Tags[1] != "http" {
		t.Fatalf("tags = %v", payload.Tags)
	}
}

func TestFormBindPrefersBodyOverQuery(t *testing.T) {
	var payload formPayload
	form := url.Values{"name": {"from-body"}, "limit": {"7"}}
	req := httptest.NewRequest(http.MethodPost, "/?name=from-query", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if err := Bind(req, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Name != "from-body" || payload.Limit != 7 {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestMultipartFormBind(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("name", "ada"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("tag", "go"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	var payload formPayload
	req := httptest.NewRequest(http.MethodPost, "/?limit=3", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	if err := Bind(req, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Name != "ada" || payload.Limit != 3 || len(payload.Tags) != 1 {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestHeaderBindIsCaseInsensitive(t *testing.T) {
	var payload headerPayload
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("x-token", "secret")

	if err := BindHeader(req, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Token != "secret" {
		t.Fatalf("token = %q, want secret", payload.Token)
	}
}

func TestHeaderBindCombinesRepeatedValues(t *testing.T) {
	type payload struct {
		Accept string `header:"Accept-Encoding"`
	}
	var bound payload
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Add("Accept-Encoding", "gzip")
	req.Header.Add("Accept-Encoding", "br")

	if err := BindHeader(req, &bound); err != nil {
		t.Fatal(err)
	}
	if bound.Accept != "gzip, br" {
		t.Fatalf("accept = %q, want gzip, br", bound.Accept)
	}
}

func TestPathBind(t *testing.T) {
	var payload pathPayload
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	req.SetPathValue("id", "42")

	if err := BindPath(req, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ID != "42" {
		t.Fatalf("id = %q, want 42", payload.ID)
	}
}

func TestBindRejectsNilDestination(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")

	if err := Bind(req, nil); err == nil {
		t.Fatal("binding a nil destination should fail")
	}
}

type validatedPayload struct {
	Name string `json:"name"`
}

func (p *validatedPayload) Validate() error {
	if p.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

func TestBindRunsValidation(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")

	err := Bind(req, &validatedPayload{})
	if err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("error = %v, want validation failure", err)
	}
}

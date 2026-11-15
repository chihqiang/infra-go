package rest

import (
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chihqiang/infra-go/rest/handler"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func decodeEnvelope(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, body)
	}
	return decoded
}

func TestJSONBaseResponse(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		wantCode float64
		wantMsg  string
		wantData any
	}{
		{name: "success value", input: map[string]string{"name": "ada"}, wantCode: 0, wantMsg: "ok", wantData: map[string]any{"name": "ada"}},
		{name: "code msg pointer", input: New(4001, "invalid"), wantCode: 4001, wantMsg: "invalid"},
		{name: "code msg struct", input: CodeMsg{Code: 4002, Msg: "conflict"}, wantCode: 4002, wantMsg: "conflict"},
		{name: "grpc status", input: status.New(codes.NotFound, "missing"), wantCode: 5, wantMsg: "missing"},
		{name: "grpc error", input: status.Error(codes.Unknown, "boom"), wantCode: 2, wantMsg: "boom"},
		{name: "plain error", input: stderrors.New("failed"), wantCode: -1, wantMsg: "failed"},
	}

	for _, test := range tests {
		recorder := httptest.NewRecorder()
		JSONBaseResponse(recorder, test.input)

		if recorder.Code != 200 {
			t.Fatalf("%s: status = %d, want 200", test.name, recorder.Code)
		}
		envelope := decodeEnvelope(t, recorder.Body.Bytes())
		if envelope["code"] != test.wantCode {
			t.Errorf("%s: code = %v, want %v", test.name, envelope["code"], test.wantCode)
		}
		if envelope["msg"] != test.wantMsg {
			t.Errorf("%s: msg = %v, want %v", test.name, envelope["msg"], test.wantMsg)
		}
		if test.wantData != nil {
			data, ok := envelope["data"].(map[string]any)
			if !ok || data["name"] != test.wantData.(map[string]any)["name"] {
				t.Errorf("%s: data = %v", test.name, envelope["data"])
			}
		} else if _, present := envelope["data"]; present {
			t.Errorf("%s: data should be omitted, got %v", test.name, envelope["data"])
		}
	}
}

type namedValue struct {
	Name string `json:"name" xml:"name"`
}

func TestXMLBaseResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	XMLBaseResponse(recorder, namedValue{Name: "ada"})

	if got := recorder.Header().Get("Content-Type"); got != XMLContentType {
		t.Fatalf("content type = %q, want %q", got, XMLContentType)
	}
	body := recorder.Body.String()
	for _, want := range []string{
		`<xml version="1.0" encoding="UTF-8">`,
		"<code>0</code>",
		"<msg>ok</msg>",
		"<data><name>ada</name></data>",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("xml body missing %q:\n%s", want, body)
		}
	}
}

func TestJSONBaseResponseCtx(t *testing.T) {
	recorder := httptest.NewRecorder()
	JSONBaseResponseCtx(httptest.NewRequest("GET", "/", nil).Context(), recorder, "pong")

	envelope := decodeEnvelope(t, recorder.Body.Bytes())
	if envelope["data"] != "pong" || envelope["code"] != float64(BusinessCodeOK) {
		t.Fatalf("envelope = %v", envelope)
	}
}

func TestOkXML(t *testing.T) {
	recorder := httptest.NewRecorder()
	OkXML(recorder, namedValue{Name: "ada"})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != XMLContentType {
		t.Fatalf("content type = %q, want %q", got, XMLContentType)
	}
	body := recorder.Body.String()
	for _, want := range []string{
		`<xml version="1.0" encoding="UTF-8">`,
		"<code>0</code>",
		"<msg>ok</msg>",
		"<data><name>ada</name></data>",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("xml body missing %q:\n%s", want, body)
		}
	}
}

func TestWriteXMLUsesGivenStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteXML(recorder, http.StatusAccepted, namedValue{Name: "ada"})

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", recorder.Code)
	}
}

func TestXMLMarshalFailureReturnsInternalServerError(t *testing.T) {
	recorder := httptest.NewRecorder()
	OkXML(recorder, make(chan int))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
}

func TestOkHTML(t *testing.T) {
	recorder := httptest.NewRecorder()
	OkHTML(recorder, "<h1>ok</h1>")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != HTMLContentType {
		t.Fatalf("content type = %q, want %q", got, HTMLContentType)
	}
	if got := recorder.Body.String(); got != "<h1>ok</h1>" {
		t.Fatalf("body = %q", got)
	}
}

func TestWriteHTMLUsesGivenStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteHTML(recorder, http.StatusNotFound, "missing")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestJSONWriters(t *testing.T) {
	tests := []struct {
		name   string
		write  func(http.ResponseWriter, any) error
		status int
	}{
		{name: "ok", write: OkJSON, status: http.StatusOK},
		{name: "created", write: CreatedJSON, status: http.StatusCreated},
	}

	for _, test := range tests {
		recorder := httptest.NewRecorder()
		if err := test.write(recorder, map[string]string{"status": "fine"}); err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if recorder.Code != test.status {
			t.Errorf("%s: status = %d, want %d", test.name, recorder.Code, test.status)
		}
		if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
			t.Errorf("%s: content type = %q", test.name, got)
		}
		if got := recorder.Body.String(); got != "{\"code\":0,\"msg\":\"ok\",\"data\":{\"status\":\"fine\"}}\n" {
			t.Errorf("%s: body = %q", test.name, got)
		}
	}
}

func TestNoContent(t *testing.T) {
	recorder := httptest.NewRecorder()
	NoContent(recorder)

	if recorder.Code != http.StatusNoContent || recorder.Body.Len() != 0 {
		t.Fatalf("response = %d, body %q", recorder.Code, recorder.Body.String())
	}
}

func TestErrorMapsStatusAndHidesInternalDetails(t *testing.T) {
	clientRecorder := httptest.NewRecorder()
	if err := Error(clientRecorder, ErrNotFound); err != nil {
		t.Fatal(err)
	}
	if clientRecorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", clientRecorder.Code)
	}
	envelope := decodeEnvelope(t, clientRecorder.Body.Bytes())
	if envelope["code"] != float64(BusinessCodeError) || envelope["msg"] == "" {
		t.Fatalf("envelope = %v", envelope)
	}

	serverRecorder := httptest.NewRecorder()
	if err := Error(serverRecorder, errDatabase{}); err != nil {
		t.Fatal(err)
	}
	if serverRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", serverRecorder.Code)
	}
	envelope = decodeEnvelope(t, serverRecorder.Body.Bytes())
	if envelope["msg"] == "database exploded" {
		t.Fatal("internal error details leaked into the response")
	}
}

type errDatabase struct{}

func (errDatabase) Error() string { return "database exploded" }

func TestErrorKeepsBusinessCodeMessage(t *testing.T) {
	recorder := httptest.NewRecorder()
	if err := Error(recorder, New(4001, "invalid order")); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	envelope := decodeEnvelope(t, recorder.Body.Bytes())
	if envelope["code"] != float64(4001) || envelope["msg"] != "invalid order" {
		t.Fatalf("envelope = %v", envelope)
	}
}

func TestJSONBaseResponseCtxCarriesRequestID(t *testing.T) {
	recorder := httptest.NewRecorder()
	var body []byte
	var code int

	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	handler.RequestIDHandler(func(w http.ResponseWriter, r *http.Request) {
		JSONBaseResponseCtx(r.Context(), w, namedValue{Name: "ada"})
		body = w.(*httptest.ResponseRecorder).Body.Bytes()
		code = w.(*httptest.ResponseRecorder).Code
	}).ServeHTTP(recorder, request)

	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	envelope := decodeEnvelope(t, body)
	requestID, ok := envelope["request_id"].(string)
	if !ok || requestID == "" {
		t.Fatalf("request_id missing from envelope: %v", envelope)
	}
	if got := recorder.Header().Get(handler.RequestIDHeader); got != requestID {
		t.Fatalf("envelope request_id = %q, response header = %q", requestID, got)
	}
}

func TestEnvelopeOmitsRequestIDWithoutMiddleware(t *testing.T) {
	recorder := httptest.NewRecorder()
	JSONBaseResponse(recorder, namedValue{Name: "ada"})

	envelope := decodeEnvelope(t, recorder.Body.Bytes())
	if _, present := envelope["request_id"]; present {
		t.Fatalf("request_id should be omitted: %v", envelope)
	}
}

func TestErrorCtxCarriesRequestID(t *testing.T) {
	var body []byte
	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	handler.RequestIDHandler(func(w http.ResponseWriter, r *http.Request) {
		_ = ErrorCtx(r.Context(), w, ErrNotFound)
		body = w.(*httptest.ResponseRecorder).Body.Bytes()
	}).ServeHTTP(httptest.NewRecorder(), request)

	envelope := decodeEnvelope(t, body)
	if envelope["request_id"] == nil || envelope["request_id"] == "" {
		t.Fatalf("request_id missing from error envelope: %v", envelope)
	}
}

func TestXMLBaseResponseCtxCarriesRequestID(t *testing.T) {
	var body string
	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	handler.RequestIDHandler(func(w http.ResponseWriter, r *http.Request) {
		XMLBaseResponseCtx(r.Context(), w, namedValue{Name: "ada"})
		body = w.(*httptest.ResponseRecorder).Body.String()
	}).ServeHTTP(httptest.NewRecorder(), request)

	if !strings.Contains(body, "<request_id>") {
		t.Fatalf("xml envelope missing request_id:\n%s", body)
	}
}

func TestOkHTMLCtxWritesHTML(t *testing.T) {
	recorder := httptest.NewRecorder()
	OkHTMLCtx(httptest.NewRequest(http.MethodGet, "/", nil).Context(), recorder, "<p>ok</p>")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != HTMLContentType {
		t.Fatalf("content type = %q", got)
	}
	if recorder.Body.String() != "<p>ok</p>" {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}

func TestOkJSONCtxCarriesRequestID(t *testing.T) {
	recorder := httptest.NewRecorder()
	handlerFn := handler.RequestIDHandler(func(w http.ResponseWriter, r *http.Request) {
		if err := OkJSONCtx(r.Context(), w, "pong"); err != nil {
			t.Fatal(err)
		}
	})
	handlerFn.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	envelope := decodeEnvelope(t, recorder.Body.Bytes())
	if envelope["code"] != float64(BusinessCodeOK) || envelope["data"] != "pong" {
		t.Fatalf("envelope = %v", envelope)
	}
	if id, _ := envelope["request_id"].(string); id == "" {
		t.Fatal("request_id is missing from the envelope")
	}
}

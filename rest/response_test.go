package rest

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chihqiang/ten-go/rest/handler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decodeEnvelope(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, body)
	}
	return decoded
}

type namedValue struct {
	Name string `json:"name" xml:"name"`
}

func TestOkJSONCtx(t *testing.T) {
	recorder := httptest.NewRecorder()
	if err := OkJSONCtx(httptest.NewRequest("GET", "/", nil).Context(), recorder, "pong"); err != nil {
		t.Fatal(err)
	}
	if got, want := recorder.Body.String(), "{\"code\":0,\"msg\":\"ok\",\"data\":\"pong\"}\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestOkXML(t *testing.T) {
	recorder := httptest.NewRecorder()
	if err := OkXML(recorder, namedValue{Name: "ada"}); err != nil {
		t.Fatal(err)
	}

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != XMLContentType {
		t.Fatalf("content type = %q, want %q", got, XMLContentType)
	}
	want := `<xml version="1.0" encoding="UTF-8"><code>0</code><msg>ok</msg><data><name>ada</name></data></xml>`
	if got := recorder.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestWriteXMLUsesGivenStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	if err := WriteXML(recorder, http.StatusAccepted, namedValue{Name: "ada"}); err != nil {
		t.Fatal(err)
	}

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", recorder.Code)
	}
}

func TestXMLMarshalFailureReturnsInternalServerError(t *testing.T) {
	recorder := httptest.NewRecorder()
	if err := OkXML(recorder, make(chan int)); err == nil {
		t.Fatal("expected XML marshal error")
	}

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
}

func TestWriteHTML(t *testing.T) {
	recorder := httptest.NewRecorder()
	if err := WriteHTML(recorder, http.StatusOK, "<h1>ok</h1>"); err != nil {
		t.Fatal(err)
	}

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
	if err := WriteHTML(recorder, http.StatusNotFound, "missing"); err != nil {
		t.Fatal(err)
	}

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestJSONWriters(t *testing.T) {
	tests := []struct {
		name   string
		write  func(http.ResponseWriter, any) error
		status int
		want   string
	}{
		{
			name:   "ok",
			write:  OkJSON,
			status: http.StatusOK,
			want:   "{\"code\":0,\"msg\":\"ok\",\"data\":{\"status\":\"fine\"}}\n",
		},
		{
			name: "created",
			write: func(w http.ResponseWriter, v any) error {
				return WriteJSON(context.Background(), w, http.StatusCreated, v)
			},
			status: http.StatusCreated,
			want:   "{\"status\":\"fine\"}\n",
		},
	}

	for _, test := range tests {
		recorder := httptest.NewRecorder()
		if err := test.write(recorder, map[string]string{"status": "fine"}); err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if recorder.Code != test.status {
			t.Errorf("%s: status = %d, want %d", test.name, recorder.Code, test.status)
		}
		if got := recorder.Header().Get("Content-Type"); got != JSONContentType {
			t.Errorf("%s: content type = %q", test.name, got)
		}
		if got := recorder.Body.String(); got != test.want {
			t.Errorf("%s: body = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestWriteJSONReturnsWriteError(t *testing.T) {
	err := WriteJSON(context.Background(), &shortWriter{}, http.StatusOK, "body")
	require.Error(t, err)
}

func TestErrorMapsStatusAndHidesInternalDetails(t *testing.T) {
	clientRecorder := httptest.NewRecorder()
	if err := ErrorJSON(clientRecorder, ErrResourceNotFound); err != nil {
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
	if err := ErrorJSON(serverRecorder, errDatabase{}); err != nil {
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
	if err := ErrorJSON(recorder, NewCodeError(4001, "invalid order")); err != nil {
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

func TestErrorJSONCtxCarriesRequestID(t *testing.T) {
	var body []byte
	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	handler.NewRequestIDHandler(handler.RequestIDConfig{Enabled: true}).Handle(func(w http.ResponseWriter, r *http.Request) {
		_ = ErrorJSONCtx(r.Context(), w, ErrResourceNotFound)
		body = w.(*httptest.ResponseRecorder).Body.Bytes()
	}).ServeHTTP(httptest.NewRecorder(), request)

	envelope := decodeEnvelope(t, body)
	if envelope["request_id"] == nil || envelope["request_id"] == "" {
		t.Fatalf("request_id missing from error envelope: %v", envelope)
	}
}

// TestOkCtxWrapsSuccessIntoBaseResponse keeps success and error payloads on the
// same code/msg/data envelope.
func TestOkCtxWrapsSuccessIntoBaseResponse(t *testing.T) {
	var jsonBody []byte
	var xmlBody string
	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	handler.NewRequestIDHandler(handler.RequestIDConfig{Enabled: true}).Handle(func(w http.ResponseWriter, r *http.Request) {
		recorder := w.(*httptest.ResponseRecorder)
		if err := OkJSONCtx(r.Context(), w, namedValue{Name: "ada"}); err != nil {
			t.Error(err)
		}
		jsonBody = recorder.Body.Bytes()

		xmlRecorder := httptest.NewRecorder()
		if err := OkXMLCtx(r.Context(), xmlRecorder, namedValue{Name: "ada"}); err != nil {
			t.Error(err)
		}
		xmlBody = xmlRecorder.Body.String()
	}).ServeHTTP(httptest.NewRecorder(), request)

	envelope := decodeEnvelope(t, jsonBody)
	if envelope["code"] != float64(BusinessCodeOK) || envelope["msg"] != BusinessMsgOK {
		t.Fatalf("json envelope = %v", envelope)
	}
	if envelope["request_id"] == nil || envelope["request_id"] == "" {
		t.Fatalf("request_id missing from success envelope: %v", envelope)
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok || data["name"] != "ada" {
		t.Fatalf("json data = %v", envelope["data"])
	}

	for _, want := range []string{"<code>0</code>", "<msg>ok</msg>", "<name>ada</name>", "<request_id>"} {
		if !strings.Contains(xmlBody, want) {
			t.Fatalf("xml success envelope missing %q:\n%s", want, xmlBody)
		}
	}
}

func TestErrorXMLCtxCarriesRequestID(t *testing.T) {
	var body string
	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	handler.NewRequestIDHandler(handler.RequestIDConfig{Enabled: true}).Handle(func(w http.ResponseWriter, r *http.Request) {
		if err := ErrorXMLCtx(r.Context(), w, ErrResourceNotFound); err != nil {
			t.Error(err)
		}
		body = w.(*httptest.ResponseRecorder).Body.String()
	}).ServeHTTP(httptest.NewRecorder(), request)

	if !strings.Contains(body, "<request_id>") || !strings.Contains(body, "rest: resource not found") {
		t.Fatalf("xml error envelope missing request ID or message:\n%s", body)
	}
}

func TestErrorXMLUsesStatusAndEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()
	if err := ErrorXML(recorder, ErrResourceNotFound); err != nil {
		t.Fatal(err)
	}

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != XMLContentType {
		t.Fatalf("content type = %q, want %q", got, XMLContentType)
	}
	for _, want := range []string{"<code>-1</code>", "<msg>rest: resource not found</msg>"} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Fatalf("xml error envelope missing %q:\n%s", want, recorder.Body.String())
		}
	}
}

// TestError_CodeMsg5xxKeepsBusinessMessage is a regression test: 5xx responses
// replaced every message with http.StatusText, so a business 503 such as a
// maintenance window lost its code and message.
func TestError_CodeMsg5xxKeepsBusinessMessage(t *testing.T) {
	w := httptest.NewRecorder()
	require.NoError(t, ErrorJSON(w, NewCodeError(http.StatusServiceUnavailable, "maintenance window")))

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	var body BaseResponse[any]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, http.StatusServiceUnavailable, body.Code)
	assert.Equal(t, "maintenance window", body.Msg)
}

// TestError_StatusError5xxKeepsMessage covers explicitly chosen 5xx statuses:
// they are intentional, so the message is not replaced by the status text.
func TestError_StatusError5xxKeepsMessage(t *testing.T) {
	w := httptest.NewRecorder()
	require.NoError(t, ErrorJSON(w, NewStatusError(http.StatusServiceUnavailable, "maintenance window")))

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	var body BaseResponse[any]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, BusinessCodeError, body.Code)
	assert.Equal(t, "maintenance window", body.Msg)
}

// TestError_Unknown5xxStillHidden keeps internal error details out of the response.
func TestError_Unknown5xxStillHidden(t *testing.T) {
	w := httptest.NewRecorder()
	require.NoError(t, ErrorJSON(w, stderrors.New("db password leaked here")))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "db password")
	assert.Contains(t, w.Body.String(), http.StatusText(http.StatusInternalServerError))
}

// TestError_WrappedCodeMsgKeepsBusinessMessage covers fmt.Errorf("%w") wrapping.
func TestError_WrappedCodeMsgKeepsBusinessMessage(t *testing.T) {
	w := httptest.NewRecorder()
	err := fmt.Errorf("load failed: %w", NewCodeError(http.StatusBadGateway, "upstream down"))
	require.NoError(t, ErrorJSON(w, err))

	var body BaseResponse[any]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, http.StatusBadGateway, body.Code)
	assert.Equal(t, "upstream down", body.Msg)
}

// TestOkJSONFillsRequestIDWithoutContext covers the common handler form that
// only has the ResponseWriter: the envelope takes the ID from the response
// header written by RequestIDHandler instead of leaving request_id empty.
func TestOkJSONFillsRequestIDWithoutContext(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	recorder := httptest.NewRecorder()
	handler.NewRequestIDHandler(handler.RequestIDConfig{Enabled: true}).Handle(
		func(w http.ResponseWriter, _ *http.Request) {
			require.NoError(t, OkJSON(w, namedValue{Name: "ada"}))
		},
	).ServeHTTP(recorder, request)

	envelope := decodeEnvelope(t, recorder.Body.Bytes())
	id := recorder.Header().Get(handler.RequestIDHeader)
	require.NotEmpty(t, id)
	assert.Equal(t, id, envelope["request_id"])
}

// TestErrorJSONFillsRequestIDWithoutContext is the error-envelope counterpart.
func TestErrorJSONFillsRequestIDWithoutContext(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	recorder := httptest.NewRecorder()
	handler.NewRequestIDHandler(handler.RequestIDConfig{Enabled: true}).Handle(
		func(w http.ResponseWriter, _ *http.Request) {
			require.NoError(t, ErrorJSON(w, ErrResourceNotFound))
		},
	).ServeHTTP(recorder, request)

	envelope := decodeEnvelope(t, recorder.Body.Bytes())
	assert.Equal(t, recorder.Header().Get(handler.RequestIDHeader), envelope["request_id"])
}

// TestRequestIDFromContextWinsOverHeader keeps an explicit context authoritative.
func TestRequestIDFromContextWinsOverHeader(t *testing.T) {
	recorder := httptest.NewRecorder()
	recorder.Header().Set(handler.RequestIDHeader, "header-id")
	ctx := handler.ContextWithRequestID(context.Background(), "ctx-id")
	require.NoError(t, OkJSONCtx(ctx, recorder, namedValue{Name: "ada"}))

	envelope := decodeEnvelope(t, recorder.Body.Bytes())
	assert.Equal(t, "ctx-id", envelope["request_id"])
}

// TestError_WrappedStatusErrorKeepsDetail is a regression test: wrapping a
// sentinel with fmt.Errorf("%w: detail") used to print only the sentinel text,
// dropping the detail the caller had just added.
func TestError_WrappedStatusErrorKeepsDetail(t *testing.T) {
	w := httptest.NewRecorder()
	err := fmt.Errorf("%w: app_key already exists", ErrConflict)
	require.NoError(t, ErrorJSON(w, err))

	assert.Equal(t, http.StatusConflict, w.Code)
	var body BaseResponse[any]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, BusinessCodeError, body.Code)
	assert.Contains(t, body.Msg, "app_key already exists")
}

// TestError_WrappedStatusError4xxKeepsDetail covers the 401 path, which is
// where losing the detail hurts the most (the reason is never in the sentinel).
func TestError_WrappedStatusError4xxKeepsDetail(t *testing.T) {
	w := httptest.NewRecorder()
	require.NoError(t, ErrorJSON(w, fmt.Errorf("%w: account is disabled", ErrUnauthorized)))

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "account is disabled")
}

// TestError_WrappedStatusError5xxStillHidden ensures the fix does not leak
// internals: wrapping a 5xx sentinel must not expose the wrapped detail.
func TestError_WrappedStatusError5xxStillHidden(t *testing.T) {
	w := httptest.NewRecorder()
	require.NoError(t, ErrorJSON(w, fmt.Errorf("%w: db password leaked here", ErrServerUnavailable)))

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.NotContains(t, w.Body.String(), "db password")
	assert.Contains(t, w.Body.String(), http.StatusText(http.StatusServiceUnavailable))
}

// TestError_StatusErrorSubclassKeepsDetail covers a custom error that reports a
// status through StatusCoder without being a *StatusError.
func TestError_StatusErrorSubclassKeepsDetail(t *testing.T) {
	w := httptest.NewRecorder()
	require.NoError(t, ErrorJSON(w, customStatusError{}))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "field app_key is invalid")
}

type customStatusError struct{}

func (customStatusError) Error() string   { return "field app_key is invalid" }
func (customStatusError) StatusCode() int { return http.StatusBadRequest }

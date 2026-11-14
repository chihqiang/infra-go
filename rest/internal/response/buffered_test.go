package response

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBufferedWriterReplaysResponse(t *testing.T) {
	buffered := NewBufferedWriter()
	buffered.Header().Set("X-Trace", "abc")
	buffered.WriteHeader(http.StatusCreated)
	_, _ = buffered.Write([]byte("created"))

	recorder := httptest.NewRecorder()
	if err := buffered.WriteTo(recorder); err != nil {
		t.Fatal(err)
	}

	if recorder.Code != http.StatusCreated || recorder.Body.String() != "created" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("X-Trace"); got != "abc" {
		t.Fatalf("header = %q, want abc", got)
	}
}

func TestBufferedWriterDefaultsToOK(t *testing.T) {
	buffered := NewBufferedWriter()
	_, _ = buffered.Write([]byte("ok"))

	if got := buffered.Status(); got != http.StatusOK {
		t.Fatalf("status = %d, want 200", got)
	}
}

func TestBufferedWriterKeepsFirstStatus(t *testing.T) {
	buffered := NewBufferedWriter()
	buffered.WriteHeader(http.StatusAccepted)
	buffered.WriteHeader(http.StatusInternalServerError)

	if got := buffered.Status(); got != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", got)
	}
}

type shortWriteWriter struct {
	http.ResponseWriter
	limit int
}

func (w *shortWriteWriter) Write(p []byte) (int, error) {
	if len(p) > w.limit {
		return w.limit, nil
	}
	return len(p), nil
}

func TestBufferedWriterReportsShortWrite(t *testing.T) {
	writer := NewBufferedWriter()
	_, _ = writer.Write([]byte("hello"))

	inner := &shortWriteWriter{ResponseWriter: httptest.NewRecorder(), limit: 1}
	if err := writer.WriteTo(inner); err == nil {
		t.Fatal("short write should be reported")
	}
}

package response

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatusWriterRecordsExplicitStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := NewStatusWriter(recorder)

	writer.WriteHeader(http.StatusTeapot)

	if writer.Status() != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", writer.Status())
	}
	if recorder.Code != http.StatusTeapot {
		t.Fatalf("delegated status = %d, want 418", recorder.Code)
	}
}

func TestStatusWriterDefaultsToOKAfterWrite(t *testing.T) {
	writer := NewStatusWriter(httptest.NewRecorder())
	_, _ = writer.Write([]byte("ok"))

	if writer.Status() != http.StatusOK {
		t.Fatalf("status = %d, want 200", writer.Status())
	}
}

func TestStatusWriterKeepsFirstStatus(t *testing.T) {
	writer := NewStatusWriter(httptest.NewRecorder())
	writer.WriteHeader(http.StatusAccepted)
	writer.WriteHeader(http.StatusInternalServerError)

	if writer.Status() != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", writer.Status())
	}
}

func TestStatusWriterUnwrap(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := NewStatusWriter(recorder)

	if writer.Unwrap() != recorder {
		t.Fatal("Unwrap() did not return the wrapped writer")
	}
}

func TestStatusWriterDelegatesFlushAndHijack(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := NewStatusWriter(recorder)

	flusher, ok := any(writer).(http.Flusher)
	if !ok {
		t.Fatal("StatusWriter does not implement http.Flusher")
	}
	flusher.Flush()
	if !recorder.Flushed {
		t.Fatal("Flush did not reach the underlying writer")
	}

	if _, ok := any(writer).(http.Hijacker); !ok {
		t.Fatal("StatusWriter does not implement http.Hijacker")
	}
}

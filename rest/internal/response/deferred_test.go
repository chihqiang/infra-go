package response

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeferredWriterUsesFallbackStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := NewDeferredWriter(recorder)
	_, _ = writer.Write([]byte("denied"))

	writer.Flush(http.StatusNotFound)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if recorder.Body.String() != "denied" {
		t.Fatalf("body = %q, want denied", recorder.Body.String())
	}
}

func TestDeferredWriterPrefersHandlerStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := NewDeferredWriter(recorder)
	writer.WriteHeader(http.StatusTooManyRequests)
	writer.WriteHeader(http.StatusInternalServerError)
	_, _ = writer.Write([]byte("slow down"))

	writer.Flush(http.StatusNotFound)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusTooManyRequests)
	}
}

func TestDeferredWriterFlushesOnce(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := NewDeferredWriter(recorder)

	writer.Flush(http.StatusNoContent)
	writer.Flush(http.StatusNotFound)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestDeferredWriterUnwrap(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := NewDeferredWriter(recorder)

	if writer.Unwrap() != recorder {
		t.Fatal("Unwrap() did not return the wrapped writer")
	}
}

func TestDeferredWriterKeepsInterimResponseOutOfFinalStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := NewDeferredWriter(recorder)

	writer.WriteHeader(http.StatusEarlyHints)
	writer.WriteHeader(http.StatusNotFound)
	writer.Flush(http.StatusOK)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

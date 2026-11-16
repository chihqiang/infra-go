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

type countingHeaderWriter struct {
	http.ResponseWriter
	headers int
}

func (w *countingHeaderWriter) WriteHeader(status int) {
	w.headers++
	w.ResponseWriter.WriteHeader(status)
}

func TestStatusWriterForwardsOnlyFirstWriteHeader(t *testing.T) {
	inner := &countingHeaderWriter{ResponseWriter: httptest.NewRecorder()}
	writer := NewStatusWriter(inner)

	writer.WriteHeader(http.StatusAccepted)
	writer.WriteHeader(http.StatusInternalServerError)

	if inner.headers != 1 {
		t.Fatalf("forwarded headers = %d, want 1", inner.headers)
	}
}

// interimAwareWriter models net/http: a 1xx response is sent immediately and
// does not commit the final status.
type interimAwareWriter struct {
	http.ResponseWriter
	final   int
	interim []int
}

func (w *interimAwareWriter) WriteHeader(status int) {
	if status >= 100 && status < 200 {
		w.interim = append(w.interim, status)
		return
	}
	if w.final == 0 {
		w.final = status
	}
}

func TestStatusWriterKeepsInterimResponseOutOfFinalStatus(t *testing.T) {
	inner := &interimAwareWriter{ResponseWriter: httptest.NewRecorder()}
	writer := NewStatusWriter(inner)

	writer.WriteHeader(http.StatusEarlyHints)
	writer.WriteHeader(http.StatusNotFound)

	if writer.Status() != http.StatusNotFound || inner.final != http.StatusNotFound {
		t.Fatalf("status = %d / %d, want 404", writer.Status(), inner.final)
	}
	if len(inner.interim) != 1 || inner.interim[0] != http.StatusEarlyHints {
		t.Fatalf("interim = %v, want [103]", inner.interim)
	}
}

func TestStatusWriterCommitted(t *testing.T) {
	writer := NewStatusWriter(httptest.NewRecorder())
	if writer.Committed() {
		t.Fatal("a fresh writer must not be committed")
	}
	writer.WriteHeader(http.StatusNoContent)
	if !writer.Committed() {
		t.Fatal("an explicit status must mark the writer committed")
	}
}

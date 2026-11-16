package response

import (
	"bufio"
	"net"
	"net/http"
)

// StatusWriter records the response status while delegating to the inner writer.
type StatusWriter struct {
	http.ResponseWriter
	status   int
	hijacked bool
}

// NewStatusWriter wraps w so the written status can be inspected afterwards.
func NewStatusWriter(w http.ResponseWriter) *StatusWriter {
	return &StatusWriter{ResponseWriter: w}
}

// WriteHeader records the first final status and forwards it. Informational
// (1xx) responses are forwarded without consuming the final-status slot, and
// later final calls are dropped, avoiding "superfluous response.WriteHeader"
// noise from the server.
func (s *StatusWriter) WriteHeader(status int) {
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		s.ResponseWriter.WriteHeader(status)
		return
	}
	if s.status != 0 {
		return
	}
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

// Write forwards p, recording an implicit 200 status first.
func (s *StatusWriter) Write(p []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(p)
}

// Status returns the recorded status, defaulting to 200.
func (s *StatusWriter) Status() int {
	if s.status == 0 {
		return http.StatusOK
	}
	return s.status
}

// Committed reports whether a final status or body was written (or the
// connection was hijacked), which means the response can no longer be replaced.
func (s *StatusWriter) Committed() bool {
	return s.status != 0 || s.hijacked
}

// Unwrap exposes the underlying writer to http.ResponseController.
func (s *StatusWriter) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}

// Flush implements http.Flusher so streaming handlers (SSE, long polling) keep
// working behind logging, tracing and breaker middleware.
func (s *StatusWriter) Flush() {
	_ = http.NewResponseController(s.ResponseWriter).Flush()
}

// Hijack implements http.Hijacker so protocol upgrades (WebSocket, CONNECT)
// keep working behind logging, tracing and breaker middleware.
func (s *StatusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, writer, err := http.NewResponseController(s.ResponseWriter).Hijack()
	if err == nil {
		s.hijacked = true
	}
	return conn, writer, err
}

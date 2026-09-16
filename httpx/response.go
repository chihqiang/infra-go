package httpx

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/chihqiang/infra-go/logger"
)

// This file implements the unified HTTP response layer:
//   - The Response[T] envelope, the CodeError business error and the business/HTTP
//     status code constants
//   - JSON / XML / HTML / SSE response output (the Ok*/Write* families)
//   - Error responses (WriteHTTPError*) and redirects (Redirect*)

// --- Business code constants ---

const (
	// CodeOK is the success business code.
	CodeOK = 0
	// MsgOK is the success business message.
	MsgOK = "ok"
	// CodeDefaultError is the default error business code.
	CodeDefaultError = -1
)

// --- HTTP status code constants ---

const (
	// CodeBadRequest means invalid request parameters.
	CodeBadRequest = 400
	// CodeUnauthorized means unauthenticated.
	CodeUnauthorized = 401
	// CodeForbidden means no permission.
	CodeForbidden = 403
	// CodeNotFound means the resource does not exist.
	CodeNotFound = 404
	// CodeRequestEntityTooLarge means the request body is too large.
	CodeRequestEntityTooLarge = 413
	// CodeInternalError means an internal server error.
	CodeInternalError = 500
	// CodeNotImplemented means not implemented.
	CodeNotImplemented = 501
	// CodeServiceUnavailable means the service is unavailable.
	CodeServiceUnavailable = 503
	// CodeTimeout means the request timed out.
	CodeTimeout = 504
)

// --- Content-Type constants ---

const (
	// ContentTypeJSON is the JSON content type.
	ContentTypeJSON = "application/json; charset=utf-8"
	// ContentTypeXML is the XML content type.
	ContentTypeXML = "application/xml; charset=utf-8"
	// ContentTypeHTML is the HTML content type.
	ContentTypeHTML = "text/html; charset=utf-8"
	// ContentTypeSSE is the Server-Sent Events content type.
	ContentTypeSSE = "text/event-stream; charset=utf-8"

	xmlVersion  = "1.0"
	xmlEncoding = "UTF-8"
)

// --- Response struct ---

// Response is the unified response envelope; the data field is generic so it can
// hold any type.
//
// Usage:
//
//	type User struct { Name string `json:"name"` }
//	resp := httpx.Response[User]{
//	    Code: httpx.CodeOK,
//	    Msg:  httpx.MsgOK,
//	    Data: User{Name: "Alice"},
//	}
type Response[T any] struct {
	// Code is the business status code; 0 means success.
	Code int `json:"code" xml:"code"`
	// Msg is the human-readable message.
	Msg string `json:"msg" xml:"msg"`
	// Data is the response payload.
	Data T `json:"data,omitempty" xml:"data,omitempty"`
	// RequestID is the request ID (optional), extracted from the context and omitted
	// when absent.
	RequestID string `json:"request_id,omitempty" xml:"request_id,omitempty"`
}

// xmlResponse is a response envelope carrying the XML declaration.
type xmlResponse[T any] struct {
	XMLName  xml.Name `xml:"xml"`
	Version  string   `xml:"version,attr"`
	Encoding string   `xml:"encoding,attr"`
	Response[T]
}

// --- CodeError ---

// CodeError is an error carrying a business status code.
// It implements the error interface and can be used for unified error propagation.
type CodeError struct {
	// Code is the business status code.
	Code int
	// Msg is the error message.
	Msg string
	// Cause is the underlying error.
	Cause error
}

// Error returns the error message.
func (e *CodeError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Msg, e.Cause)
	}
	return e.Msg
}

// Unwrap returns the underlying error, supporting errors.Is / errors.As.
func (e *CodeError) Unwrap() error {
	return e.Cause
}

// NewCodeError creates a CodeError carrying business code code and message msg.
func NewCodeError(code int, msg string) *CodeError {
	return &CodeError{Code: code, Msg: msg}
}

// NewCodeErrorWithCause creates a CodeError with the underlying error cause, making
// it traceable via errors.Is/As.
func NewCodeErrorWithCause(code int, msg string, cause error) *CodeError {
	return &CodeError{Code: code, Msg: msg, Cause: cause}
}

// --- Smart wrapping ---

// wrapResponse automatically wraps the given value into the unified response based on
// its type.
//
// Type inference rules:
//   - *CodeError / CodeError → use their Code and Msg
//   - error                  → Code = CodeError, Msg = error.Error()
//   - anything else          → Code = CodeOK, Msg = MsgOK, Data = v
//
// When ctx carries a request_id it is written into the response as well.
func wrapResponse(ctx context.Context, v any) Response[any] {
	var resp Response[any]
	switch data := v.(type) {
	case *CodeError:
		resp.Code = data.Code
		resp.Msg = data.Msg
	case CodeError:
		resp.Code = data.Code
		resp.Msg = data.Msg
	case error:
		resp.Code = CodeDefaultError
		resp.Msg = data.Error()
	default:
		resp.Code = CodeOK
		resp.Msg = MsgOK
		resp.Data = v
	}
	if rid := RequestIDFromContext(ctx); rid != "" {
		resp.RequestID = rid
	}
	return resp
}

// wrapXMLResponse wraps v into a response envelope carrying the XML declaration.
func wrapXMLResponse(ctx context.Context, v any) xmlResponse[any] {
	return xmlResponse[any]{
		Version:  xmlVersion,
		Encoding: xmlEncoding,
		Response: wrapResponse(ctx, v),
	}
}

// --- JSON responses ---

// WriteJSON writes an HTTP response in JSON format.
// This is a low-level function: it performs no wrapping of v and serializes it
// directly.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	if err := writeJSON(w, status, v); err != nil {
		logger.Error("write json response failed", logger.Err(err))
	}
}

// WriteJSONCtx is WriteJSON with a context.
func WriteJSONCtx(ctx context.Context, w http.ResponseWriter, status int, v any) {
	WriteJSON(w, status, v)
}

// OkJSON smart-wraps v and writes the response in JSON format (HTTP 200).
//
// If v is a *CodeError, CodeError or error, the matching error code and message are
// set automatically; otherwise Code=0, Msg="ok", Data=v.
func OkJSON(w http.ResponseWriter, v any) {
	WriteJSON(w, http.StatusOK, wrapResponse(context.Background(), v))
}

// OkJSONCtx is OkJSON with a context.
// When the context carries a request_id (injected via ContextWithRequestID / the
// WithRequestID middleware), it is written into the response as well.
func OkJSONCtx(ctx context.Context, w http.ResponseWriter, v any) {
	WriteJSON(w, http.StatusOK, wrapResponse(ctx, v))
}

// writeJSON performs the actual JSON serialization and write.
func writeJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", ContentTypeJSON)
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

// --- XML responses ---

// WriteXML writes an HTTP response in XML format.
// Like WriteJSON it is a low-level function: it performs no wrapping of v and
// serializes it directly; use OkXML when you want smart wrapping (unified
// code/msg/data).
func WriteXML(w http.ResponseWriter, status int, v any) {
	if err := writeXML(w, status, v); err != nil {
		logger.Error("write xml response failed", logger.Err(err))
	}
}

// WriteXMLCtx is WriteXML with a context.
func WriteXMLCtx(ctx context.Context, w http.ResponseWriter, status int, v any) {
	WriteXML(w, status, v)
}

// OkXML smart-wraps v and writes the response in XML format (HTTP 200).
// The wrapping rules match OkJSON: if v is a *CodeError, CodeError or error, its
// error code and message are used automatically; otherwise Code=0, Msg="ok",
// Data=v; the XML declaration is included as well.
func OkXML(w http.ResponseWriter, v any) {
	WriteXML(w, http.StatusOK, wrapXMLResponse(context.Background(), v))
}

// OkXMLCtx is OkXML with a context.
// When the context carries a request_id, it is written into the response as well.
func OkXMLCtx(ctx context.Context, w http.ResponseWriter, v any) {
	WriteXML(w, http.StatusOK, wrapXMLResponse(ctx, v))
}

// writeXML performs the actual XML serialization and write.
func writeXML(w http.ResponseWriter, status int, v any) error {
	bs, err := xml.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return fmt.Errorf("marshal xml failed, error: %w", err)
	}

	w.Header().Set("Content-Type", ContentTypeXML)
	w.WriteHeader(status)

	if n, err := w.Write(bs); err != nil {
		// http.ErrHandlerTimeout is already handled by http.TimeoutHandler; ignore it here.
		if err != http.ErrHandlerTimeout {
			return fmt.Errorf("write response failed, error: %w", err)
		}
	} else if n < len(bs) {
		return fmt.Errorf("actual bytes: %d, written bytes: %d", len(bs), n)
	}

	return nil
}

// --- HTML responses ---

// WriteHTML writes an HTTP response in HTML format.
// v is a raw HTML string; it is neither escaped nor wrapped and is emitted as-is.
func WriteHTML(w http.ResponseWriter, status int, v string) {
	if err := writeHTML(w, status, v); err != nil {
		logger.Error("write html response failed", logger.Err(err))
	}
}

// WriteHTMLCtx is WriteHTML with a context.
func WriteHTMLCtx(ctx context.Context, w http.ResponseWriter, status int, v string) {
	WriteHTML(w, status, v)
}

// OkHTML writes the response in HTML format (HTTP 200) with v emitted as-is.
func OkHTML(w http.ResponseWriter, v string) {
	WriteHTML(w, http.StatusOK, v)
}

// OkHTMLCtx is OkHTML with a context.
func OkHTMLCtx(ctx context.Context, w http.ResponseWriter, v string) {
	OkHTML(w, v)
}

// writeHTML performs the actual HTML write.
func writeHTML(w http.ResponseWriter, status int, v string) error {
	w.Header().Set("Content-Type", ContentTypeHTML)
	w.WriteHeader(status)

	bs := []byte(v)
	if n, err := w.Write(bs); err != nil {
		// http.ErrHandlerTimeout is already handled by http.TimeoutHandler; ignore it here.
		if err != http.ErrHandlerTimeout {
			return fmt.Errorf("write response failed, error: %w", err)
		}
	} else if n < len(bs) {
		return fmt.Errorf("actual bytes: %d, written bytes: %d", len(bs), n)
	}

	return nil
}

// --- SSE (Server-Sent Events) responses ---

// SSEWriter pushes a Server-Sent Events (SSE) stream to the client.
//
// Once created it can write event frames back-to-back, and every write flushes
// automatically so events reach the client in real time; when an event method returns
// an error (usually because the client has disconnected) you should stop pushing and
// return from the handler.
//
// Usage:
//
//	func StreamHandler(w http.ResponseWriter, r *http.Request) {
//	    sse := httpx.NewSSEWriter(w)
//	    for i := 0; i < 10; i++ {
//	        if err := sse.JSONEvent("ping", fmt.Sprintf("tick %d", i)); err != nil {
//	            return // client disconnected
//	        }
//	    }
//	}
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewSSEWriter creates an SSE writer and sets the response headers SSE requires:
// Content-Type: text/event-stream, Cache-Control: no-cache, Connection: keep-alive,
// and disables reverse-proxy buffering (X-Accel-Buffering: no) so events are not
// delayed by buffering layers such as nginx.
func NewSSEWriter(w http.ResponseWriter) *SSEWriter {
	h := w.Header()
	h.Set("Content-Type", ContentTypeSSE)
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")

	s := &SSEWriter{w: w}
	if f, ok := w.(http.Flusher); ok {
		s.flusher = f
	}
	return s
}

// Event writes one event frame of the given type.
// An empty event means the default message event; when data spans multiple lines it is
// split into multiple data: fields per the SSE spec.
func (s *SSEWriter) Event(event, data string) error {
	var b strings.Builder
	if event != "" {
		s.writeField(&b, "event", event)
	}
	s.writeField(&b, "data", data)
	b.WriteByte('\n')
	return s.write(b.String())
}

// Data writes a data frame with the default event type (message).
func (s *SSEWriter) Data(data string) error {
	return s.Event("", data)
}

// JSONEvent serializes v to JSON and writes it as the event data.
// Clients can parse the received data field with JSON.parse.
func (s *SSEWriter) JSONEvent(event string, v any) error {
	bs, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal sse event data failed, error: %w", err)
	}
	return s.Event(event, string(bs))
}

// Comment writes a comment frame (a line starting with a colon); clients ignore its
// content, so it is commonly used as a keep-alive heartbeat.
func (s *SSEWriter) Comment(text string) error {
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		b.WriteByte(':')
		if line != "" {
			b.WriteByte(' ')
			b.WriteString(line)
		}
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	return s.write(b.String())
}

// Retry tells the browser the reconnection interval (in milliseconds) after a drop.
func (s *SSEWriter) Retry(ms int) error {
	var b strings.Builder
	b.WriteString("retry: ")
	b.WriteString(strconv.Itoa(ms))
	b.WriteString("\n\n")
	return s.write(b.String())
}

// writeField splits a multi-line field value into multiple "field: value" lines per
// the SSE spec.
func (s *SSEWriter) writeField(b *strings.Builder, field, value string) {
	for _, line := range strings.Split(value, "\n") {
		b.WriteString(field)
		b.WriteString(": ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
}

// write writes the data and flushes immediately, making sure events reach the client
// in real time.
func (s *SSEWriter) write(payload string) error {
	if _, err := s.w.Write([]byte(payload)); err != nil {
		return err
	}
	s.Flush()
	return nil
}

// Flush pushes buffered data to the client immediately.
// If the underlying ResponseWriter does not implement http.Flusher, it is a no-op.
func (s *SSEWriter) Flush() {
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

// --- Error response helpers ---

// WriteHTTPError writes an HTTP error response.
// It sets both the HTTP status code and the business code to status, for errors at
// the HTTP layer (such as 400, 404, etc.).
// It is equivalent to WriteHTTPErrorWithCode(w, status, status, msg).
func WriteHTTPError(w http.ResponseWriter, status int, msg string) {
	WriteHTTPErrorWithCode(w, status, status, msg)
}

// WriteHTTPErrorCtx is WriteHTTPError with a context.
// When the context carries a request_id, it is written into the response as well.
// It is equivalent to WriteHTTPErrorWithCodeCtx(ctx, w, status, status, msg).
func WriteHTTPErrorCtx(ctx context.Context, w http.ResponseWriter, status int, msg string) {
	WriteHTTPErrorWithCodeCtx(ctx, w, status, status, msg)
}

// WriteHTTPErrorWithCode writes an HTTP error response and supports separating the
// HTTP status code from the business code.
//
// In a RESTful API the HTTP status code reflects the transport layer state (e.g.
// 400 Bad Request) while the business code reflects business semantics (e.g. 10001
// meaning "username already exists").
// This function lets the two be set independently, for cases that need fine-grained
// business error codes.
//
// Usage:
//
//	// HTTP 400, business code 10001
//	httpx.WriteHTTPErrorWithCode(w, http.StatusBadRequest, 10001, "username already exists")
func WriteHTTPErrorWithCode(w http.ResponseWriter, status int, code int, msg string) {
	WriteJSON(w, status, Response[any]{
		Code: code,
		Msg:  msg,
	})
}

// WriteHTTPErrorWithCodeCtx is WriteHTTPErrorWithCode with a context.
// When the context carries a request_id, it is written into the response as well.
func WriteHTTPErrorWithCodeCtx(ctx context.Context, w http.ResponseWriter, status int, code int, msg string) {
	WriteJSON(w, status, Response[any]{
		Code:      code,
		Msg:       msg,
		RequestID: RequestIDFromContext(ctx),
	})
}

// --- Redirects ---

// Redirect redirects to url with the given status code.
// It sets the Location response header and triggers a browser navigation.
func Redirect(w http.ResponseWriter, r *http.Request, url string, status int) {
	http.Redirect(w, r, url, status)
}

// RedirectCtx is Redirect with a context.
func RedirectCtx(ctx context.Context, w http.ResponseWriter, r *http.Request, url string, status int) {
	Redirect(w, r, url, status)
}

// RedirectTemporary performs a temporary redirect (HTTP 302 Found).
func RedirectTemporary(w http.ResponseWriter, r *http.Request, url string) {
	Redirect(w, r, url, http.StatusFound)
}

// RedirectTemporaryCtx is RedirectTemporary with a context.
func RedirectTemporaryCtx(ctx context.Context, w http.ResponseWriter, r *http.Request, url string) {
	Redirect(w, r, url, http.StatusFound)
}

// RedirectPermanent performs a permanent redirect (HTTP 301 Moved Permanently).
func RedirectPermanent(w http.ResponseWriter, r *http.Request, url string) {
	Redirect(w, r, url, http.StatusMovedPermanently)
}

// RedirectPermanentCtx is RedirectPermanent with a context.
func RedirectPermanentCtx(ctx context.Context, w http.ResponseWriter, r *http.Request, url string) {
	Redirect(w, r, url, http.StatusMovedPermanently)
}

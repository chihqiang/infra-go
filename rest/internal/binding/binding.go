// Package binding decodes HTTP requests into Go structures.
//
// Selection is content-type driven, with per-source binders available for query
// strings, headers and path parameters. Values are mapped through struct tags
// (form, query, header, path) and validated with the binding tag.
package binding

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"
)

// jsonSniffSize bounds the body prefix inspected to recognise a JSON payload
// when the Content-Type is missing or declares a form.
const jsonSniffSize = 512

// Content-Type values handled by the package.
const (
	MIMEJSON              = "application/json"
	MIMEXML               = "application/xml"
	MIMEXML2              = "text/xml"
	MIMEPOSTForm          = "application/x-www-form-urlencoded"
	MIMEMultipartPOSTForm = "multipart/form-data"
)

// Binding decodes a request into a destination value.
type Binding interface {
	// Name identifies the binder, mainly for error messages and tests.
	Name() string
	Bind(req *http.Request, dst any) error
}

// BindingBody is a Binding that can also decode an already-read body.
type BindingBody interface {
	Binding
	BindBody(body []byte, dst any) error
}

// Binders exposed for callers that know the request format, mirroring the
// content types handled by Default.
var (
	JSON          BindingBody = jsonBinding{}
	XML           BindingBody = xmlBinding{}
	Form          Binding     = formBinding{}
	FormPost      Binding     = formPostBinding{}
	FormMultipart Binding     = formMultipartBinding{}
	Query         Binding     = queryBinding{}
	Header        Binding     = headerBinding{}
	Path          Binding     = pathBinding{}
)

// Default returns the binder matching the request method and content type.
// Requests without a body use the query string.
func Default(method, contentType string) Binding {
	if method == http.MethodGet || method == http.MethodHead {
		return Query
	}

	normalized := normalizeMediaType(contentType)
	if isJSONMediaType(normalized) {
		return JSON
	}
	switch normalized {
	case MIMEXML, MIMEXML2:
		return XML
	case MIMEMultipartPOSTForm:
		return FormMultipart
	case MIMEPOSTForm:
		return Form
	default:
		return Form
	}
}

// Bind decodes req into dst with the binder for its content type.
func Bind(req *http.Request, dst any) error {
	if req == nil {
		return badRequest("rest/binding: request must not be nil")
	}
	if !hasBody(req) {
		return Query.Bind(req, dst)
	}

	contentType := mediaType(req)

	// Clients often omit the JSON Content-Type or send curl's default
	// application/x-www-form-urlencoded. Inspect the body prefix so a JSON
	// payload still reaches the JSON binder instead of being parsed as a form.
	if contentType == "" || contentType == MIMEPOSTForm {
		isJSON, err := looksLikeJSONBody(req)
		if err != nil {
			return requestError("rest/binding: read body: %w", err)
		}
		if isJSON {
			return JSON.Bind(req, dst)
		}
	}

	switch {
	case contentType == "":
		return formBodyBinding{}.Bind(req, dst)
	case isJSONMediaType(contentType):
		return JSON.Bind(req, dst)
	case contentType == MIMEXML || contentType == MIMEXML2:
		return XML.Bind(req, dst)
	case contentType == MIMEMultipartPOSTForm:
		return FormMultipart.Bind(req, dst)
	case contentType == MIMEPOSTForm:
		return Form.Bind(req, dst)
	default:
		return unsupportedMediaType(contentType)
	}
}

// isJSONMediaType reports whether contentType is JSON, including the RFC 6839
// structured suffix (for example application/vnd.api+json).
func isJSONMediaType(contentType string) bool {
	return contentType == MIMEJSON || strings.HasSuffix(contentType, "+json")
}

// looksLikeJSONBody reads a small prefix of the body and restores it. The caller
// must only invoke it when the body will otherwise be treated as a form.
func looksLikeJSONBody(req *http.Request) (bool, error) {
	body := req.Body
	buf := make([]byte, jsonSniffSize)
	n := 0
	var readErr error

	for n < len(buf) {
		read, err := body.Read(buf[n:])
		n += read
		if err != nil {
			readErr = err
			break
		}
		if read == 0 {
			break
		}
		if firstNonSpace(buf[:n]) != 0 {
			break
		}
	}

	req.Body = replayBody{
		Reader: io.MultiReader(bytes.NewReader(buf[:n]), body),
		Closer: body,
	}

	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return false, readErr
	}

	first := firstNonSpace(buf[:n])
	return first == '{' || first == '[', nil
}

func firstNonSpace(buf []byte) byte {
	for _, b := range buf {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return b
		}
	}
	return 0
}

// replayBody replays the inspected prefix before the original body.
type replayBody struct {
	io.Reader
	io.Closer
}

// BindQuery decodes the query string into dst using form tags.
func BindQuery(req *http.Request, dst any) error {
	return Query.Bind(req, dst)
}

// BindHeader decodes request headers into dst using header tags.
func BindHeader(req *http.Request, dst any) error {
	return Header.Bind(req, dst)
}

// BindPath decodes path parameters into dst using path tags.
func BindPath(req *http.Request, dst any) error {
	return Path.Bind(req, dst)
}

func hasBody(req *http.Request) bool {
	return req.Body != nil && req.Body != http.NoBody
}

func mediaType(req *http.Request) string {
	return normalizeMediaType(req.Header.Get("Content-Type"))
}

func normalizeMediaType(value string) string {
	if value == "" {
		return ""
	}

	parsed, _, err := mime.ParseMediaType(value)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(value))
	}
	return strings.ToLower(parsed)
}

func checkDestination(dst any) error {
	if dst == nil {
		return badRequest("rest/binding: destination must not be nil")
	}
	value := reflect.ValueOf(dst)
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return badRequest("rest/binding: destination must be a non-nil pointer")
	}
	return nil
}

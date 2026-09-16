// Package binding provides HTTP request data binding capabilities:
//   - Binder interfaces (Binding/BindingBody/BindingUri) and built-in instances
//     (JSON/XML/Form/Query/Header/Uri)
//   - MIME constants for common Content-Types and the Default binder selector
//   - Reflection-based mapping engine (form/query/header/URI → structs) with field metadata
//     cache (mapping.go / meta.go / value.go)
//   - Struct validation based on go-playground/validator (validator.go)
//
// The convenience binding helpers of the httpx main package (Bind*/MustBind*, see
// httpx/request.go) call this package directly; binder instances, MIME constants,
// the Default selector and the validation entry point are all provided here.
package binding

import (
	"mime"
	"net/http"
	"strings"
)

// --- MIME type constants ---

// Common Content-Type MIME types.
const (
	MIMEJSON              = "application/json"
	MIMEXML               = "application/xml"
	MIMEXML2              = "text/xml"
	MIMEPlain             = "text/plain"
	MIMEPOSTForm          = "application/x-www-form-urlencoded"
	MIMEMultipartPOSTForm = "multipart/form-data"
)

// --- Binder interfaces ---

// Binding describes the interface that binds request data into a struct.
// Different data sources (JSON body, query parameters, form fields, etc.) implement it.
type Binding interface {
	// Name returns the binder name.
	Name() string
	// Bind binds the request data into the obj struct.
	Bind(*http.Request, any) error
}

// BindingBody extends the Binding interface to support binding from raw bytes.
// It is used by body-based binders such as JSON and XML.
type BindingBody interface {
	Binding
	// BindBody binds from a byte slice into the obj struct.
	BindBody([]byte, any) error
}

// BindingUri describes the interface that binds from URI path parameters,
// used for route path parameters (e.g. /users/{id}).
// It is used via httpx.BindURI / BindURIWithValues; path parameters are passed as a map.
type BindingUri interface {
	Name() string
	// BindUri binds from a path-parameter map into the obj struct.
	BindUri(map[string][]string, any) error
}

// --- Built-in binder instances ---

var (
	// JSON is a binder based on the JSON body.
	JSON BindingBody = JSONBinding{}
	// XML is a binder based on the XML body.
	XML BindingBody = XMLBinding{}
	// Form is a binder based on form data (including query and post form).
	Form Binding = FormBinding{}
	// Query is a binder based on URL query parameters.
	Query Binding = QueryBinding{}
	// Header is a binder based on HTTP headers.
	Header Binding = HeaderBinding{}
	// Uri is a binder based on URI path parameters.
	Uri BindingUri = URIBinding{}
)

// Default returns a suitable binder based on the request method and Content-Type.
// GET requests always return Form (binding the query); other requests are matched by Content-Type:
// JSON → JSON, XML → XML, form/multipart → Form, unparseable or unknown → Form.
func Default(method, contentType string) Binding {
	if method == http.MethodGet {
		return Form
	}

	// Parse the Content-Type, strip parameters (e.g. ; charset=utf-8) and ignore case,
	// so that common formats like "application/json; charset=utf-8" still match.
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return Form
	}
	mediaType = strings.ToLower(mediaType)

	switch mediaType {
	case MIMEJSON:
		return JSON
	case MIMEXML, MIMEXML2:
		return XML
	case MIMEMultipartPOSTForm:
		return Form
	default: // case MIMEPOSTForm:
		return Form
	}
}

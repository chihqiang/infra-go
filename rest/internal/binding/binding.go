// Package binding decodes HTTP requests into Go structures.
package binding

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"
)

// ErrUnsupportedMediaType is returned when a request body has no matching binder.
var ErrUnsupportedMediaType = errors.New("binding: unsupported media type")

// Binding decodes a request into a destination value.
type Binding interface {
	Bind(req *http.Request, dst any) error
}

// Bind decodes req into dst using the binder for its content type.
// Requests without a body are bound from the query string.
func Bind(req *http.Request, dst any) error {
	return Binder(req).Bind(req, dst)
}

// BindQuery decodes only the query string into dst.
func BindQuery(req *http.Request, dst any) error {
	return Query.Bind(req, dst)
}

// BindHeader decodes only the request headers into dst.
func BindHeader(req *http.Request, dst any) error {
	return Header.Bind(req, dst)
}

// BindPath decodes only the path parameters into dst.
func BindPath(req *http.Request, dst any) error {
	return Path.Bind(req, dst)
}

// Binder selects the binder matching req's content type. Requests without a
// body bind from the query string, and bodies without a declared type are
// treated as forms.
func Binder(req *http.Request) Binding {
	if req == nil || !hasBody(req) {
		return Query
	}

	contentType := mediaType(req)
	if contentType == "" {
		return Form
	}
	if binder, ok := byContentType(contentType); ok {
		return binder
	}
	return unsupportedBinding{}
}

type unsupportedBinding struct{}

func (unsupportedBinding) Bind(*http.Request, any) error {
	return &Error{Status: http.StatusUnsupportedMediaType, Err: ErrUnsupportedMediaType}
}

func hasBody(req *http.Request) bool {
	return req.Body != nil && req.Body != http.NoBody
}

func mediaType(req *http.Request) string {
	value := req.Header.Get("Content-Type")
	if value == "" {
		return ""
	}

	parsed, _, err := mime.ParseMediaType(value)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed)
}

func byContentType(value string) (Binding, bool) {
	switch value {
	case "application/json":
		return JSON, true
	case "application/xml", "text/xml":
		return XML, true
	case "application/x-www-form-urlencoded":
		return Form, true
	case "multipart/form-data":
		return MultipartForm, true
	default:
		return nil, false
	}
}

// Validatable lets a destination validate itself after decoding.
type Validatable interface {
	Validate() error
}

func validate(dst any) error {
	validatable, ok := dst.(Validatable)
	if !ok {
		return nil
	}
	if err := validatable.Validate(); err != nil {
		return wrapError(fmt.Errorf("binding: validation failed: %w", err))
	}
	return nil
}

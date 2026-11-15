package binding

import (
	"errors"
	"io"
	"net/http"
	"net/url"
)

type formBinding struct{}
type formPostBinding struct{}
type formMultipartBinding struct{}

func (formBinding) Name() string { return "form" }

// Bind merges the query string with the request body, body values winning.
func (formBinding) Bind(req *http.Request, dst any) error {
	if err := req.ParseForm(); err != nil {
		return requestError("rest/binding: parse form: %w", err)
	}
	if err := req.ParseMultipartForm(defaultMemory); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		return requestError("rest/binding: parse multipart form: %w", err)
	}

	values := make(map[string][]string, len(req.Form))
	for key, items := range req.URL.Query() {
		values[key] = append([]string(nil), items...)
	}
	for key, items := range req.Form {
		values[key] = items
	}
	if req.MultipartForm != nil {
		for key, items := range req.MultipartForm.Value {
			values[key] = items
		}
	}

	return mapByTag(dst, values, "form")
}

func (formPostBinding) Name() string { return "form-urlencoded" }

// Bind reads only the request body values.
func (formPostBinding) Bind(req *http.Request, dst any) error {
	if err := req.ParseForm(); err != nil {
		return requestError("rest/binding: parse form: %w", err)
	}
	return mapByTag(dst, req.PostForm, "form")
}

type formBodyBinding struct{}

func (formBodyBinding) Name() string { return "form-body" }

// Bind parses a body that was sent without a Content-Type as urlencoded values.
func (formBodyBinding) Bind(req *http.Request, dst any) error {
	if req == nil || req.Body == nil {
		return mapByTag(dst, map[string][]string{}, "form")
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return requestError("rest/binding: read form body: %w", err)
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return badRequest("rest/binding: parse form body: %v", err)
	}
	return mapByTag(dst, values, "form")
}

func (formMultipartBinding) Name() string { return "multipart/form-data" }

// Bind reads multipart values and uploaded files.
func (formMultipartBinding) Bind(req *http.Request, dst any) error {
	if err := req.ParseMultipartForm(defaultMemory); err != nil {
		return requestError("rest/binding: parse multipart form: %w", err)
	}

	values := make(map[string][]string, len(req.URL.Query()))
	for key, items := range req.URL.Query() {
		values[key] = append([]string(nil), items...)
	}
	form := req.MultipartForm
	if form == nil {
		return mapByTag(dst, values, "form")
	}
	for key, items := range form.Value {
		values[key] = items
	}
	return mapWithFiles(dst, values, form.File, "form")
}

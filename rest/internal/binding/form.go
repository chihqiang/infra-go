package binding

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type formBinding struct{}
type formPostBinding struct{}
type formMultipartBinding struct{}

// maxFormBodySize mirrors the 10MB cap net/http applies in ParseForm; it bounds the
// body when it has to be read manually (DELETE/OPTIONS and other methods for which
// ParseForm does not read the body).
const maxFormBodySize = 10 << 20

func (formBinding) Name() string { return "form" }

// Bind merges the query string with the request body, body values winning.
func (formBinding) Bind(req *http.Request, dst any) error {
	if err := req.ParseForm(); err != nil {
		return requestError("rest/binding: parse form: %w", err)
	}
	if err := req.ParseMultipartForm(defaultMemory); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		return requestError("rest/binding: parse multipart form: %w", err)
	}

	values := queryValues(req)
	mergeValues(values, req.Form)
	if req.MultipartForm != nil {
		mergeValues(values, req.MultipartForm.Value)
	}

	// ParseForm reads the body only for POST/PUT/PATCH. For DELETE, OPTIONS and
	// other methods the form body was silently ignored before, so a same-named
	// query parameter won (an attacker-controlled query value could replace the
	// body value).
	if formBodyNotParsed(req) {
		bodyValues, err := readFormBodyValues(req)
		if err != nil {
			return err
		}
		mergeValues(values, bodyValues)
	}

	return mapByTag(dst, values, "form")
}

func (formPostBinding) Name() string { return "form-urlencoded" }

// Bind reads only the request body values.
func (formPostBinding) Bind(req *http.Request, dst any) error {
	if err := req.ParseForm(); err != nil {
		return requestError("rest/binding: parse form: %w", err)
	}
	values := make(map[string][]string, len(req.PostForm))
	mergeValues(values, req.PostForm)
	if formBodyNotParsed(req) {
		bodyValues, err := readFormBodyValues(req)
		if err != nil {
			return err
		}
		mergeValues(values, bodyValues)
	}
	return mapByTag(dst, values, "form")
}

type formBodyBinding struct{}

func (formBodyBinding) Name() string { return "form-body" }

// Bind parses a body that was sent without a Content-Type as urlencoded values.
// As with formBinding, the query string is merged in and body values win.
func (formBodyBinding) Bind(req *http.Request, dst any) error {
	if req == nil || req.Body == nil {
		return mapByTag(dst, map[string][]string{}, "form")
	}
	body, err := readLimitedBody(req.Body, maxFormBodySize)
	if err != nil {
		return err
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return badRequest("rest/binding: parse form body: %v", err)
	}
	merged := queryValues(req)
	mergeValues(merged, values)
	return mapByTag(dst, merged, "form")
}

func (formMultipartBinding) Name() string { return "multipart/form-data" }

// Bind reads multipart values and uploaded files.
func (formMultipartBinding) Bind(req *http.Request, dst any) error {
	if err := req.ParseMultipartForm(defaultMemory); err != nil {
		return requestError("rest/binding: parse multipart form: %w", err)
	}

	values := queryValues(req)
	form := req.MultipartForm
	if form == nil {
		return mapByTag(dst, values, "form")
	}
	mergeValues(values, form.Value)
	return mapWithFiles(dst, values, form.File, "form")
}

// queryValues copies the URL query into a fresh map.
func queryValues(req *http.Request) map[string][]string {
	if req == nil || req.URL == nil {
		return make(map[string][]string)
	}
	values := make(map[string][]string, len(req.URL.Query()))
	for key, items := range req.URL.Query() {
		values[key] = append([]string(nil), items...)
	}
	return values
}

// mergeValues copies src into dst, replacing the lists of duplicate keys.
func mergeValues(dst, src map[string][]string) {
	for key, items := range src {
		dst[key] = items
	}
}

// formBodyNotParsed reports whether the request method is one for which
// http.Request.ParseForm does not read urlencoded body data.
func formBodyNotParsed(req *http.Request) bool {
	switch req.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return false
	default:
		return req.Body != nil
	}
}

// readFormBodyValues reads and parses the urlencoded body of a request whose method
// makes ParseForm skip the body.
func readFormBodyValues(req *http.Request) (map[string][]string, error) {
	body, err := readLimitedBody(req.Body, maxFormBodySize)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, nil
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, badRequest("rest/binding: parse form body: %v", err)
	}
	return values, nil
}

// readLimitedBody reads at most limit bytes and reports a 413 error when the body
// is larger, so a manual read cannot buffer an unbounded payload.
func readLimitedBody(body io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, requestError("rest/binding: read form body: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, &Error{
			Status: http.StatusRequestEntityTooLarge,
			Err:    fmt.Errorf("rest/binding: form body exceeds %d bytes", limit),
		}
	}
	return data, nil
}

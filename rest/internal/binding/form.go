package binding

import (
	"fmt"
	"net/http"
)

// Form decodes the query string and URL-encoded body, with body values winning.
var Form Binding = formBinding{}

// MultipartForm decodes the query string and multipart body values.
var MultipartForm Binding = multipartFormBinding{}

const defaultMultipartMemory = 32 << 20

type formBinding struct{}

func (formBinding) Bind(req *http.Request, dst any) error {
	if err := checkDestination(dst); err != nil {
		return err
	}
	if err := req.ParseForm(); err != nil {
		return wrapError(fmt.Errorf("binding: parse form: %w", err))
	}

	values := mergeValues(valuesToMap(req.URL.Query()), valuesToMap(req.PostForm))
	return bindValues(tagForm, values, dst)
}

type multipartFormBinding struct{}

func (multipartFormBinding) Bind(req *http.Request, dst any) error {
	if err := checkDestination(dst); err != nil {
		return err
	}
	if err := req.ParseMultipartForm(defaultMultipartMemory); err != nil {
		return wrapError(fmt.Errorf("binding: parse multipart form: %w", err))
	}

	values := valuesToMap(req.URL.Query())
	if req.MultipartForm != nil {
		values = mergeValues(values, valuesToMap(req.MultipartForm.Value))
	}
	return bindValues(tagForm, values, dst)
}

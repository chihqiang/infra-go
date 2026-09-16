package binding

import (
	"errors"
	"net/http"
)

// defaultMemory is the maximum memory used for form parsing (32MB).
const defaultMemory = 32 << 20

// FormBinding is a binder based on form data (including query and post form).
type FormBinding struct{}

// Name returns the binder name.
func (FormBinding) Name() string {
	return "form"
}

// Bind binds the request form data into obj and validates it.
func (FormBinding) Bind(req *http.Request, obj any) error {
	if err := req.ParseForm(); err != nil {
		return err
	}
	if err := req.ParseMultipartForm(defaultMemory); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		return err
	}
	if err := mapForm(obj, req.Form); err != nil {
		return err
	}
	return validate(obj)
}

package binding

import "net/http"

// QueryBinding is a binder based on URL query parameters.
type QueryBinding struct{}

// Name returns the binder name.
func (QueryBinding) Name() string {
	return "query"
}

// Bind binds the URL query parameters into obj and validates it.
func (QueryBinding) Bind(req *http.Request, obj any) error {
	if err := mapForm(obj, req.URL.Query()); err != nil {
		return err
	}
	return validate(obj)
}

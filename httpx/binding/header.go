package binding

import (
	"net/http"
	"net/textproto"
	"reflect"
)

// HeaderBinding is a binder based on HTTP headers.
type HeaderBinding struct{}

// Name returns the binder name.
func (HeaderBinding) Name() string {
	return "header"
}

// Bind binds the HTTP headers into obj and validates it.
func (HeaderBinding) Bind(req *http.Request, obj any) error {
	if err := mapHeader(obj, req.Header); err != nil {
		return err
	}
	return validate(obj)
}

// headerSource is an HTTP header data source.
type headerSource map[string][]string

var _ setter = headerSource(nil)

// TrySet sets the value from the header data source, converting the key to canonical MIME form.
func (hs headerSource) TrySet(value reflect.Value, fm *fieldMeta, key string, opt setOptions) (bool, error) {
	return setByForm(value, fm, hs, textproto.CanonicalMIMEHeaderKey(key), opt)
}

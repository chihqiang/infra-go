package binding

import "net/http"

// Query decodes the URL query string into dst using form tags.
var Query Binding = queryBinding{}

type queryBinding struct{}

func (queryBinding) Bind(req *http.Request, dst any) error {
	if err := checkDestination(dst); err != nil {
		return err
	}
	return bindValues(tagForm, valuesToMap(req.URL.Query()), dst)
}

package binding

import (
	"net/http"
	"strings"
)

// Header decodes request headers into dst using header tags.
var Header Binding = headerBinding{}

type headerBinding struct{}

func (headerBinding) Bind(req *http.Request, dst any) error {
	if err := checkDestination(dst); err != nil {
		return err
	}

	// Repeated headers are combined with ", " the way HTTP layers them.
	values := make(map[string]any, len(req.Header))
	for key, items := range req.Header {
		if len(items) == 0 {
			continue
		}
		values[key] = strings.Join(items, ", ")
	}

	return bindValues(tagHeader, values, dst)
}

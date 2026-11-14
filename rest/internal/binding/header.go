package binding

import (
	"net/http"
	"strings"
)

type headerBinding struct{}

func (headerBinding) Name() string { return "header" }

// Bind reads request headers; keys are matched case-insensitively.
func (headerBinding) Bind(req *http.Request, dst any) error {
	if req == nil {
		return badRequest("rest/binding: request must not be nil")
	}

	values := make(map[string][]string, len(req.Header))
	for key, items := range req.Header {
		values[key] = items
		values[strings.ToLower(key)] = items
	}
	return mapByTag(dst, values, "header")
}

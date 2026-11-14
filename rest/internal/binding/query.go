package binding

import "net/http"

type queryBinding struct{}

func (queryBinding) Name() string { return "query" }

func (queryBinding) Bind(req *http.Request, dst any) error {
	if req == nil || req.URL == nil {
		return badRequest("rest/binding: request must not be nil")
	}
	return mapByTag(dst, req.URL.Query(), "form")
}

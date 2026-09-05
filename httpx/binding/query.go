package binding

import "net/http"

// QueryBinding 基于 URL query 参数的绑定器。
type QueryBinding struct{}

// Name 返回绑定器名称。
func (QueryBinding) Name() string {
	return "query"
}

// Bind 将 URL query 参数绑定到 obj，并校验。
func (QueryBinding) Bind(req *http.Request, obj any) error {
	if err := mapForm(obj, req.URL.Query()); err != nil {
		return err
	}
	return validate(obj)
}

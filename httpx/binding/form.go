package binding

import (
	"errors"
	"net/http"
)

// defaultMemory 表单解析的最大内存（32MB）。
const defaultMemory = 32 << 20

// FormBinding 基于 Form 表单的绑定器（包含 query 和 post form）。
type FormBinding struct{}

// Name 返回绑定器名称。
func (FormBinding) Name() string {
	return "form"
}

// Bind 将请求表单数据绑定到 obj，并校验。
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

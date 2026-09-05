package binding

import (
	"net/http"
	"net/textproto"
	"reflect"
)

// HeaderBinding 基于 HTTP header 的绑定器。
type HeaderBinding struct{}

// Name 返回绑定器名称。
func (HeaderBinding) Name() string {
	return "header"
}

// Bind 将 HTTP header 绑定到 obj，并校验。
func (HeaderBinding) Bind(req *http.Request, obj any) error {
	if err := mapHeader(obj, req.Header); err != nil {
		return err
	}
	return validate(obj)
}

// headerSource HTTP header 数据源。
type headerSource map[string][]string

var _ setter = headerSource(nil)

// TrySet 从 header 数据源设置值，key 转为 Canonical MIME 格式。
func (hs headerSource) TrySet(value reflect.Value, fm *fieldMeta, key string, opt setOptions) (bool, error) {
	return setByForm(value, fm, hs, textproto.CanonicalMIMEHeaderKey(key), opt)
}

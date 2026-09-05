package binding

// URIBinding 基于 URI 路径参数的绑定器。
type URIBinding struct{}

// Name 返回绑定器名称。
func (URIBinding) Name() string {
	return "uri"
}

// BindUri 将路径参数 map 绑定到 obj，并校验。
func (URIBinding) BindUri(m map[string][]string, obj any) error {
	if err := mapURI(obj, m); err != nil {
		return err
	}
	return validate(obj)
}

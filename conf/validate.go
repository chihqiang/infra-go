package conf

// Validator 定义配置验证接口。
// 如果配置结构体实现了该接口，在加载配置后会自动调用 Validate 方法进行自定义验证。
type Validator interface {
	// Validate 验证配置值是否合法，返回 nil 表示通过。
	Validate() error
}

// validate 检查 v 是否实现了 Validator 接口，如果实现了则调用其 Validate 方法。
func validate(v any) error {
	if val, ok := v.(Validator); ok {
		return val.Validate()
	}
	return nil
}

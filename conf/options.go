package conf

type (
	// Option 定义配置加载选项的函数类型。
	Option func(opt *options)

	// options 配置加载选项。
	options struct {
		// env 是否展开配置文件中的环境变量引用（如 ${VAR}）。
		env bool
	}
)

// UseEnv 设置在解析配置文件内容时展开环境变量引用。
// 配置文件中可以使用 ${VAR} 或 $VAR 来引用环境变量。
func UseEnv() Option {
	return func(opt *options) {
		opt.env = true
	}
}

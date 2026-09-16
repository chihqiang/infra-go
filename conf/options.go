package conf

type (
	// Option is the function type defining configuration loading options.
	Option func(opt *options)

	// options holds the configuration loading options.
	options struct {
		// env reports whether to expand environment variable references
		// (e.g. ${VAR}) in the configuration file.
		env bool
	}
)

// UseEnv enables expansion of environment variable references while parsing the
// configuration file content. The config file may use ${VAR} or $VAR to reference
// environment variables; the ${VAR:-default} syntax is supported as well, falling back
// to default when VAR is unset or empty.
func UseEnv() Option {
	return func(opt *options) {
		opt.env = true
	}
}

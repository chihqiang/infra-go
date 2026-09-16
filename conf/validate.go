package conf

// Validator defines the configuration validation interface.
// If a config struct implements this interface, the Validate method is called
// automatically after the configuration is loaded, for custom validation.
type Validator interface {
	// Validate validates whether the configuration values are legal; nil means it passed.
	Validate() error
}

// validate checks whether v implements the Validator interface and, if so, calls its
// Validate method.
func validate(v any) error {
	if val, ok := v.(Validator); ok {
		return val.Validate()
	}
	return nil
}

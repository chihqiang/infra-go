package binding

// URIBinding is a binder based on URI path parameters.
type URIBinding struct{}

// Name returns the binder name.
func (URIBinding) Name() string {
	return "uri"
}

// BindUri binds the path-parameter map into obj and validates it.
func (URIBinding) BindUri(m map[string][]string, obj any) error {
	if err := mapURI(obj, m); err != nil {
		return err
	}
	return validate(obj)
}

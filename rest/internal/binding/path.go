package binding

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
)

// Path decodes path parameters into dst using path tags, falling back to json tags.
var Path Binding = pathBinding{}

type pathBinding struct{}

func (pathBinding) Bind(req *http.Request, dst any) error {
	if err := checkDestination(dst); err != nil {
		return err
	}

	value := reflect.ValueOf(dst)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return errors.New("binding: destination must be a non-nil struct pointer")
	}
	target := value.Elem()
	if target.Kind() != reflect.Struct {
		return errors.New("binding: destination must be a non-nil struct pointer")
	}

	values := make(map[string]any)
	collectPathValues(req, target.Type(), values)
	return bindValues(tagPath, values, dst)
}

func collectPathValues(req *http.Request, structType reflect.Type, values map[string]any) {
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if !field.IsExported() {
			continue
		}
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			collectPathValues(req, field.Type, values)
			continue
		}

		key := pathKey(field)
		if key == "" || key == "-" {
			continue
		}
		if value := req.PathValue(key); value != "" {
			values[key] = value
		}
	}
}

func pathKey(field reflect.StructField) string {
	for _, tag := range []string{tagPath, "json"} {
		value, ok := field.Tag.Lookup(tag)
		if !ok {
			continue
		}
		if key := strings.Split(value, ",")[0]; key != "" {
			return key
		}
	}
	return field.Name
}

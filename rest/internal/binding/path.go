package binding

import (
	"net/http"
	"reflect"
)

type pathBinding struct{}

func (pathBinding) Name() string { return "path" }

// Bind reads path parameters through Request.PathValue.
func (pathBinding) Bind(req *http.Request, dst any) error {
	if err := checkDestination(dst); err != nil {
		return err
	}
	if req == nil {
		return badRequest("rest/binding: request must not be nil")
	}

	values := make(map[string][]string)
	collectPathValues(req, reflect.TypeOf(dst), values)
	return mapByTag(dst, values, "path")
}

func collectPathValues(req *http.Request, structType reflect.Type, values map[string][]string) {
	for structType.Kind() == reflect.Pointer {
		structType = structType.Elem()
	}
	if structType.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			collectPathValues(req, field.Type, values)
			continue
		}
		if !field.IsExported() {
			continue
		}

		key, ok := fieldKey(field, "path")
		if !ok {
			continue
		}
		if value := req.PathValue(key); value != "" {
			values[key] = []string{value}
		}
	}
}

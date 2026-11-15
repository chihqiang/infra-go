package binding

import (
	"net/http"
	"reflect"
)

// maxPathDepth bounds the type walk for recursive struct definitions.
const maxPathDepth = 8

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
	collectPathValues(req, reflect.TypeOf(dst), values, "", 0)
	return mapByTag(dst, values, "path")
}

// collectPathValues resolves path values for nested structs. http.ServeMux only
// accepts Go identifiers as wildcard names, so nested parameters are declared
// with their leaf name ("{id}") and mapped back to the dotted key ("org.id")
// that the mapper expects.
func collectPathValues(req *http.Request, structType reflect.Type, values map[string][]string, prefix string, depth int) {
	if depth > maxPathDepth {
		return
	}
	for structType.Kind() == reflect.Pointer {
		structType = structType.Elem()
	}
	if structType.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if field.Anonymous {
			collectPathValues(req, field.Type, values, prefix, depth+1)
			continue
		}
		if !field.IsExported() {
			continue
		}

		key, ok := fieldKey(field, "path")
		if !ok {
			continue
		}
		fullKey := key
		if prefix != "" {
			fullKey = prefix + key
		}

		if isNestedStruct(field.Type) {
			collectPathValues(req, field.Type, values, fullKey+".", depth+1)
			continue
		}

		value := req.PathValue(fullKey)
		if value == "" && fullKey != key {
			value = req.PathValue(key)
		}
		if value != "" {
			values[fullKey] = []string{value}
		}
	}
}

package binding

import (
	"fmt"
	"net/textproto"
	"net/url"
	"reflect"
	"strings"

	"github.com/chihqiang/infra-go/core/mapping"
)

const (
	tagForm   = "form"
	tagHeader = "header"
	tagPath   = "path"
)

func bindValues(tagName string, values map[string]any, dst any) error {
	if err := checkDestination(dst); err != nil {
		return err
	}

	unmarshaler := mapping.NewUnmarshaler(tagName, mapping.WithCanonicalKeyFunc(canonicalKey(tagName)))
	if err := unmarshaler.Unmarshal(normalizeSlices(tagName, dst, values), dst); err != nil {
		return wrapError(fmt.Errorf("binding: decode %s: %w", tagName, err))
	}

	return validate(dst)
}

// valuesToMap flattens single values and keeps repeats as slices.
func valuesToMap(values url.Values) map[string]any {
	result := make(map[string]any, len(values))
	for key, items := range values {
		switch len(items) {
		case 0:
			continue
		case 1:
			result[key] = items[0]
		default:
			result[key] = items
		}
	}
	return result
}

func mergeValues(base, override map[string]any) map[string]any {
	if len(base) == 0 {
		return override
	}
	for key, value := range override {
		base[key] = value
	}
	return base
}

// canonicalKey picks a normalization function so tag names match loosely.
func canonicalKey(tagName string) func(string) string {
	if tagName == tagHeader {
		return textproto.CanonicalMIMEHeaderKey
	}
	return strings.ToLower
}

// normalizeSlices wraps single values for slice fields so a request with one
// repeated key still decodes into a slice.
func normalizeSlices(tagName string, dst any, values map[string]any) map[string]any {
	sliceKeys := sliceFieldKeys(tagName, reflect.TypeOf(dst))
	if len(sliceKeys) == 0 {
		return values
	}

	canonical := canonicalKey(tagName)
	for key, value := range values {
		text, ok := value.(string)
		if !ok || !sliceKeys[canonical(key)] {
			continue
		}
		values[key] = []string{text}
	}
	return values
}

func sliceFieldKeys(tagName string, structType reflect.Type) map[string]bool {
	keys := make(map[string]bool)
	collectSliceFieldKeys(tagName, structType, keys)
	return keys
}

func collectSliceFieldKeys(tagName string, structType reflect.Type, keys map[string]bool) {
	for structType.Kind() == reflect.Pointer {
		structType = structType.Elem()
	}
	if structType.Kind() != reflect.Struct {
		return
	}

	canonical := canonicalKey(tagName)
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			collectSliceFieldKeys(tagName, field.Type, keys)
			continue
		}
		if !field.IsExported() {
			continue
		}

		fieldType := field.Type
		for fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}
		if fieldType.Kind() != reflect.Slice || fieldType.Elem().Kind() == reflect.Uint8 {
			continue
		}

		if key := tagKey(field, tagName); key != "" && key != "-" {
			keys[canonical(key)] = true
		}
	}
}

func tagKey(field reflect.StructField, tagName string) string {
	value, ok := field.Tag.Lookup(tagName)
	if !ok {
		return field.Name
	}
	return strings.Split(value, ",")[0]
}

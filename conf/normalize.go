package conf

// This file normalises a parsed config map into an unmarshallable state:
//   - normalizeMap/normalizeValue/...: unify every kind of value produced by YAML
//     into json.Number
//   - unmarshalMap: unmarshal the normalised map into the target struct
//     (case-insensitive field name matching is done by mapping's canonicalKey;
//     map keys are not rewritten here so that map field data is not damaged)

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/chihqiang/infra-go/mapping"
)

// normalizeMap recursively normalises the values in a map:
// - converts every numeric type (int, int64, float64, ...) to json.Number
// - ensures every nested map key is of type string
func normalizeMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = normalizeValue(v)
	}
	return result
}

// normalizeValue recursively normalises a value.
func normalizeValue(v any) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case bool, string:
		return val
	case int:
		return json.Number(strconv.FormatInt(int64(val), 10))
	case int8:
		return json.Number(strconv.FormatInt(int64(val), 10))
	case int16:
		return json.Number(strconv.FormatInt(int64(val), 10))
	case int32:
		return json.Number(strconv.FormatInt(int64(val), 10))
	case int64:
		return json.Number(strconv.FormatInt(val, 10))
	case uint:
		return json.Number(strconv.FormatUint(uint64(val), 10))
	case uint8:
		return json.Number(strconv.FormatUint(uint64(val), 10))
	case uint16:
		return json.Number(strconv.FormatUint(uint64(val), 10))
	case uint32:
		return json.Number(strconv.FormatUint(uint64(val), 10))
	case uint64:
		return json.Number(strconv.FormatUint(val, 10))
	case float32:
		return json.Number(strconv.FormatFloat(float64(val), 'f', -1, 32))
	case float64:
		return json.Number(strconv.FormatFloat(val, 'f', -1, 64))
	case json.Number:
		return val
	case map[string]any:
		return normalizeMap(val)
	case map[any]any:
		return normalizeAnyKeyMap(val)
	case []any:
		return normalizeSlice(val)
	case []map[string]any:
		slice := make([]any, len(val))
		for i, item := range val {
			slice[i] = normalizeMap(item)
		}
		return slice
	case []map[any]any:
		slice := make([]any, len(val))
		for i, item := range val {
			slice[i] = normalizeAnyKeyMap(item)
		}
		return slice
	default:
		return fmt.Sprintf("%v", val)
	}
}

// normalizeAnyKeyMap converts a map[any]any into a map[string]any.
func normalizeAnyKeyMap(m map[any]any) map[string]any {
	if m == nil {
		return nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[fmt.Sprintf("%v", k)] = normalizeValue(v)
	}
	return result
}

// normalizeSlice normalises every element of a slice.
func normalizeSlice(s []any) []any {
	if s == nil {
		return nil
	}
	result := make([]any, len(s))
	for i, v := range s {
		result[i] = normalizeValue(v)
	}
	return result
}

// unmarshalMap unmarshals a parsed config map into v.
// It uses mapping.WithCanonicalKeyFunc for case-insensitive field name matching and is
// shared by Load / LoadFromJSONBytes / LoadFromYAMLBytes.
//
// Note: the keys of the input map are deliberately **not** lowercased up front. Map keys
// are also the data of map-typed fields, and lowercasing everything would silently
// corrupt user data (labels: {AppName: x} would be written as appname, and when both
// AppName and appname exist the outcome depends on map iteration order).
// Case-insensitive matching is done on the mapping side.
func unmarshalMap(m map[string]any, v any) error {
	return mapping.UnmarshalJsonMap(m, v, mapping.WithCanonicalKeyFunc(strings.ToLower))
}

package cast

import (
	"reflect"
	"strings"
)

// --- Slice conversion ---

// ToIntSlice converts any to []int, returning an empty slice if the conversion fails.
// Supports []int, []any (converted element by element) and comma-separated strings.
func ToIntSlice(v any) []int {
	val, _ := ToIntSliceE(v)
	return val
}

// ToIntSliceE converts any to []int and returns the result along with an error.
func ToIntSliceE(v any) ([]int, error) {
	switch val := v.(type) {
	case nil:
		return []int{}, nil
	case []int:
		return val, nil
	case []any:
		result := make([]int, len(val))
		for i, item := range val {
			n, err := ToIntE(item)
			if err != nil {
				return nil, err
			}
			result[i] = n
		}
		return result, nil
	case []string:
		result := make([]int, len(val))
		for i, s := range val {
			n, err := ToIntE(s)
			if err != nil {
				return nil, err
			}
			result[i] = n
		}
		return result, nil
	case string:
		if val == "" {
			return []int{}, nil
		}
		parts := strings.Split(val, ",")
		result := make([]int, len(parts))
		for i, s := range parts {
			n, err := ToIntE(strings.TrimSpace(s))
			if err != nil {
				return nil, err
			}
			result[i] = n
		}
		return result, nil
	default:
		return nil, castErr(reflect.TypeOf(v).String(), "[]int")
	}
}

// ToStringSlice converts any to []string, returning an empty slice if the conversion fails.
// Supports []string, []any (converted element by element) and comma-separated strings.
func ToStringSlice(v any) []string {
	val, _ := ToStringSliceE(v)
	return val
}

// ToStringSliceE converts any to []string and returns the result along with an error.
func ToStringSliceE(v any) ([]string, error) {
	switch val := v.(type) {
	case nil:
		return []string{}, nil
	case []string:
		return val, nil
	case []any:
		result := make([]string, len(val))
		for i, item := range val {
			s, err := ToStringE(item)
			if err != nil {
				return nil, err
			}
			result[i] = s
		}
		return result, nil
	case []byte:
		return []string{string(val)}, nil
	case string:
		if val == "" {
			return []string{}, nil
		}
		parts := strings.Split(val, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts, nil
	default:
		return nil, castErr(reflect.TypeOf(v).String(), "[]string")
	}
}

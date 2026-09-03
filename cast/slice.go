package cast

import (
	"reflect"
	"strings"
)

// --- 切片转换 ---

// ToIntSlice 将 any 转换为 []int，转换失败返回空切片。
// 支持 []int、[]any（逐元素转换）、字符串（逗号分隔）。
func ToIntSlice(v any) []int {
	val, _ := ToIntSliceE(v)
	return val
}

// ToIntSliceE 将 any 转换为 []int，返回转换结果和错误。
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

// ToStringSlice 将 any 转换为 []string，转换失败返回空切片。
// 支持 []string、[]any（逐元素转换）、字符串（逗号分隔）。
func ToStringSlice(v any) []string {
	val, _ := ToStringSliceE(v)
	return val
}

// ToStringSliceE 将 any 转换为 []string，返回转换结果和错误。
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

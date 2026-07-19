package cast

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// --- 错误定义 ---

// ErrCastFailed 类型转换失败。
type ErrCastFailed struct {
	From string // 原始值的类型
	To   string // 目标类型
}

// Error 返回错误信息。
func (e *ErrCastFailed) Error() string {
	return fmt.Sprintf("cast: failed to cast %s to %s", e.From, e.To)
}

func castErr(from, to string) error {
	return &ErrCastFailed{From: from, To: to}
}

// --- 基本类型转换 ---

// ToInt 将 any 转换为 int，转换失败返回零值。
// 支持 int 系列、float 系列、string、bool、json.Number。
func ToInt(v any) int {
	val, _ := ToIntE(v)
	return val
}

// ToInt64 将 any 转换为 int64，转换失败返回零值。
func ToInt64(v any) int64 {
	val, _ := ToInt64E(v)
	return val
}

// ToUint 将 any 转换为 uint，转换失败返回零值。
func ToUint(v any) uint {
	val, _ := ToUintE(v)
	return val
}

// ToUint64 将 any 转换为 uint64，转换失败返回零值。
func ToUint64(v any) uint64 {
	val, _ := ToUint64E(v)
	return val
}

// ToFloat32 将 any 转换为 float32，转换失败返回零值。
func ToFloat32(v any) float32 {
	val, _ := ToFloat32E(v)
	return val
}

// ToFloat64 将 any 转换为 float64，转换失败返回零值。
func ToFloat64(v any) float64 {
	val, _ := ToFloat64E(v)
	return val
}

// ToString 将 any 转换为 string，转换失败返回空字符串。
// 支持 string、[]byte、json.Number、fmt.Stringer 以及基本数值类型。
func ToString(v any) string {
	val, _ := ToStringE(v)
	return val
}

// ToBool 将 any 转换为 bool，转换失败返回 false。
// 支持 bool、string（"true"/"1"）、int（非零为 true）。
func ToBool(v any) bool {
	val, _ := ToBoolE(v)
	return val
}

// --- 带 error 的转换函数 ---

// ToIntE 将 any 转换为 int，返回转换结果和错误。
func ToIntE(v any) (int, error) {
	switch val := v.(type) {
	case nil:
		return 0, nil
	case int:
		return val, nil
	case int8:
		return int(val), nil
	case int16:
		return int(val), nil
	case int32:
		return int(val), nil
	case int64:
		return int(val), nil
	case uint:
		return int(val), nil
	case uint8:
		return int(val), nil
	case uint16:
		return int(val), nil
	case uint32:
		return int(val), nil
	case uint64:
		return int(val), nil
	case float32:
		return int(val), nil
	case float64:
		return int(val), nil
	case bool:
		if val {
			return 1, nil
		}
		return 0, nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(val), 0, 0)
		if err != nil {
			return 0, castErr("string", "int")
		}
		return int(n), nil
	case json.Number:
		n, err := val.Int64()
		if err != nil {
			return 0, castErr("json.Number", "int")
		}
		return int(n), nil
	default:
		return 0, castErr(reflect.TypeOf(v).String(), "int")
	}
}

// ToInt64E 将 any 转换为 int64，返回转换结果和错误。
func ToInt64E(v any) (int64, error) {
	switch val := v.(type) {
	case nil:
		return 0, nil
	case int:
		return int64(val), nil
	case int8:
		return int64(val), nil
	case int16:
		return int64(val), nil
	case int32:
		return int64(val), nil
	case int64:
		return val, nil
	case uint:
		return int64(val), nil
	case uint8:
		return int64(val), nil
	case uint16:
		return int64(val), nil
	case uint32:
		return int64(val), nil
	case uint64:
		return int64(val), nil
	case float32:
		return int64(val), nil
	case float64:
		return int64(val), nil
	case bool:
		if val {
			return 1, nil
		}
		return 0, nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(val), 0, 64)
		if err != nil {
			return 0, castErr("string", "int64")
		}
		return n, nil
	case json.Number:
		n, err := val.Int64()
		if err != nil {
			return 0, castErr("json.Number", "int64")
		}
		return n, nil
	default:
		return 0, castErr(reflect.TypeOf(v).String(), "int64")
	}
}

// ToUintE 将 any 转换为 uint，返回转换结果和错误。
func ToUintE(v any) (uint, error) {
	n, err := ToUint64E(v)
	if err != nil {
		return 0, err
	}
	return uint(n), nil
}

// ToUint64E 将 any 转换为 uint64，返回转换结果和错误。
func ToUint64E(v any) (uint64, error) {
	switch val := v.(type) {
	case nil:
		return 0, nil
	case int:
		if val < 0 {
			return 0, castErr("int(negative)", "uint64")
		}
		return uint64(val), nil
	case int8:
		if val < 0 {
			return 0, castErr("int8(negative)", "uint64")
		}
		return uint64(val), nil
	case int16:
		if val < 0 {
			return 0, castErr("int16(negative)", "uint64")
		}
		return uint64(val), nil
	case int32:
		if val < 0 {
			return 0, castErr("int32(negative)", "uint64")
		}
		return uint64(val), nil
	case int64:
		if val < 0 {
			return 0, castErr("int64(negative)", "uint64")
		}
		return uint64(val), nil
	case uint:
		return uint64(val), nil
	case uint8:
		return uint64(val), nil
	case uint16:
		return uint64(val), nil
	case uint32:
		return uint64(val), nil
	case uint64:
		return val, nil
	case float32:
		if val < 0 {
			return 0, castErr("float32(negative)", "uint64")
		}
		return uint64(val), nil
	case float64:
		if val < 0 {
			return 0, castErr("float64(negative)", "uint64")
		}
		return uint64(val), nil
	case bool:
		if val {
			return 1, nil
		}
		return 0, nil
	case string:
		n, err := strconv.ParseUint(strings.TrimSpace(val), 0, 64)
		if err != nil {
			return 0, castErr("string", "uint64")
		}
		return n, nil
	case json.Number:
		n, err := strconv.ParseUint(val.String(), 10, 64)
		if err != nil {
			return 0, castErr("json.Number", "uint64")
		}
		return n, nil
	default:
		return 0, castErr(reflect.TypeOf(v).String(), "uint64")
	}
}

// ToFloat32E 将 any 转换为 float32，返回转换结果和错误。
func ToFloat32E(v any) (float32, error) {
	f, err := ToFloat64E(v)
	if err != nil {
		return 0, err
	}
	return float32(f), nil
}

// ToFloat64E 将 any 转换为 float64，返回转换结果和错误。
func ToFloat64E(v any) (float64, error) {
	switch val := v.(type) {
	case nil:
		return 0, nil
	case int:
		return float64(val), nil
	case int8:
		return float64(val), nil
	case int16:
		return float64(val), nil
	case int32:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case uint:
		return float64(val), nil
	case uint8:
		return float64(val), nil
	case uint16:
		return float64(val), nil
	case uint32:
		return float64(val), nil
	case uint64:
		return float64(val), nil
	case float32:
		return float64(val), nil
	case float64:
		return val, nil
	case bool:
		if val {
			return 1, nil
		}
		return 0, nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		if err != nil {
			return 0, castErr("string", "float64")
		}
		return f, nil
	case json.Number:
		f, err := val.Float64()
		if err != nil {
			return 0, castErr("json.Number", "float64")
		}
		return f, nil
	default:
		return 0, castErr(reflect.TypeOf(v).String(), "float64")
	}
}

// ToStringE 将 any 转换为 string，返回转换结果和错误。
func ToStringE(v any) (string, error) {
	switch val := v.(type) {
	case nil:
		return "", nil
	case string:
		return val, nil
	case []byte:
		return string(val), nil
	case bool:
		return strconv.FormatBool(val), nil
	case int:
		return strconv.FormatInt(int64(val), 10), nil
	case int8:
		return strconv.FormatInt(int64(val), 10), nil
	case int16:
		return strconv.FormatInt(int64(val), 10), nil
	case int32:
		return strconv.FormatInt(int64(val), 10), nil
	case int64:
		return strconv.FormatInt(val, 10), nil
	case uint:
		return strconv.FormatUint(uint64(val), 10), nil
	case uint8:
		return strconv.FormatUint(uint64(val), 10), nil
	case uint16:
		return strconv.FormatUint(uint64(val), 10), nil
	case uint32:
		return strconv.FormatUint(uint64(val), 10), nil
	case uint64:
		return strconv.FormatUint(val, 10), nil
	case float32:
		return strconv.FormatFloat(float64(val), 'f', -1, 32), nil
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64), nil
	case json.Number:
		return val.String(), nil
	case fmt.Stringer:
		return val.String(), nil
	case error:
		return val.Error(), nil
	default:
		// 尝试 JSON 序列化
		b, err := json.Marshal(v)
		if err != nil {
			return "", castErr(reflect.TypeOf(v).String(), "string")
		}
		return string(b), nil
	}
}

// ToBoolE 将 any 转换为 bool，返回转换结果和错误。
// 字符串支持：1/t/T/true/TRUE/True → true，0/f/F/false/FALSE/False → false。
func ToBoolE(v any) (bool, error) {
	switch val := v.(type) {
	case nil:
		return false, nil
	case bool:
		return val, nil
	case int:
		return val != 0, nil
	case int8:
		return val != 0, nil
	case int16:
		return val != 0, nil
	case int32:
		return val != 0, nil
	case int64:
		return val != 0, nil
	case uint:
		return val != 0, nil
	case uint8:
		return val != 0, nil
	case uint16:
		return val != 0, nil
	case uint32:
		return val != 0, nil
	case uint64:
		return val != 0, nil
	case float32:
		return val != 0, nil
	case float64:
		return val != 0, nil
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(val))
		if err != nil {
			return false, castErr("string", "bool")
		}
		return b, nil
	case json.Number:
		n, err := val.Int64()
		if err != nil {
			return false, castErr("json.Number", "bool")
		}
		return n != 0, nil
	default:
		return false, castErr(reflect.TypeOf(v).String(), "bool")
	}
}

// --- 时间类型转换 ---

// ToDuration 将 any 转换为 time.Duration，转换失败返回零值。
// 支持数值类型（纳秒）和字符串（如 "5s"、"100ms"）。
func ToDuration(v any) time.Duration {
	val, _ := ToDurationE(v)
	return val
}

// ToDurationE 将 any 转换为 time.Duration，返回转换结果和错误。
// 数值类型按纳秒处理，字符串按 time.ParseDuration 解析。
func ToDurationE(v any) (time.Duration, error) {
	switch val := v.(type) {
	case nil:
		return 0, nil
	case time.Duration:
		return val, nil
	case int:
		return time.Duration(val), nil
	case int8:
		return time.Duration(val), nil
	case int16:
		return time.Duration(val), nil
	case int32:
		return time.Duration(val), nil
	case int64:
		return time.Duration(val), nil
	case uint:
		return time.Duration(val), nil
	case uint8:
		return time.Duration(val), nil
	case uint16:
		return time.Duration(val), nil
	case uint32:
		return time.Duration(val), nil
	case uint64:
		return time.Duration(val), nil
	case float32:
		return time.Duration(val), nil
	case float64:
		return time.Duration(val), nil
	case string:
		d, err := time.ParseDuration(strings.TrimSpace(val))
		if err != nil {
			return 0, castErr("string", "time.Duration")
		}
		return d, nil
	case json.Number:
		n, err := val.Int64()
		if err != nil {
			return 0, castErr("json.Number", "time.Duration")
		}
		return time.Duration(n), nil
	default:
		return 0, castErr(reflect.TypeOf(v).String(), "time.Duration")
	}
}

// ToTime 将 any 转换为 time.Time，转换失败返回零值。
// 支持字符串（RFC3339、Unix 时间戳）和数值（Unix 时间戳）。
func ToTime(v any) time.Time {
	val, _ := ToTimeE(v)
	return val
}

// ToTimeE 将 any 转换为 time.Time，返回转换结果和错误。
// 字符串优先按 RFC3339 解析，失败后尝试按 Unix 时间戳解析。
// 数值类型按 Unix 时间戳（秒）处理。
func ToTimeE(v any) (time.Time, error) {
	switch val := v.(type) {
	case nil:
		return time.Time{}, nil
	case time.Time:
		return val, nil
	case string:
		s := strings.TrimSpace(val)
		// 尝试 RFC3339
		t, err := time.Parse(time.RFC3339, s)
		if err == nil {
			return t, nil
		}
		// 尝试 Unix 时间戳
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Unix(n, 0), nil
		}
		return time.Time{}, castErr("string", "time.Time")
	case int:
		return time.Unix(int64(val), 0), nil
	case int8:
		return time.Unix(int64(val), 0), nil
	case int16:
		return time.Unix(int64(val), 0), nil
	case int32:
		return time.Unix(int64(val), 0), nil
	case int64:
		return time.Unix(val, 0), nil
	case uint:
		return time.Unix(int64(val), 0), nil
	case uint8:
		return time.Unix(int64(val), 0), nil
	case uint16:
		return time.Unix(int64(val), 0), nil
	case uint32:
		return time.Unix(int64(val), 0), nil
	case uint64:
		return time.Unix(int64(val), 0), nil
	case float32:
		return time.Unix(int64(val), 0), nil
	case float64:
		return time.Unix(int64(val), 0), nil
	case json.Number:
		n, err := val.Int64()
		if err != nil {
			return time.Time{}, castErr("json.Number", "time.Time")
		}
		return time.Unix(n, 0), nil
	default:
		return time.Time{}, castErr(reflect.TypeOf(v).String(), "time.Time")
	}
}

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

// --- 泛型转换 ---

// To 将 any 转换为目标类型 T。
// T 可以是基本类型或其指针类型，转换失败返回零值。
// 这是一个泛型辅助函数，方便在需要特定类型时使用。
//
// 用法：
//
//	n := cast.To[int]("123")       // 123
//	s := cast.To[string](456)      // "456"
//	b := cast.To[bool]("true")     // true
//	d := cast.To[time.Duration]("5s") // 5s
func To[T any](v any) T {
	var zero T
	switch any(zero).(type) {
	case int:
		return any(ToInt(v)).(T)
	case int8:
		n, _ := ToIntE(v)
		return any(int8(n)).(T)
	case int16:
		n, _ := ToIntE(v)
		return any(int16(n)).(T)
	case int32:
		n, _ := ToIntE(v)
		return any(int32(n)).(T)
	case int64:
		return any(ToInt64(v)).(T)
	case uint:
		return any(ToUint(v)).(T)
	case uint8:
		n, _ := ToUint64E(v)
		return any(uint8(n)).(T)
	case uint16:
		n, _ := ToUint64E(v)
		return any(uint16(n)).(T)
	case uint32:
		n, _ := ToUint64E(v)
		return any(uint32(n)).(T)
	case uint64:
		return any(ToUint64(v)).(T)
	case float32:
		return any(ToFloat32(v)).(T)
	case float64:
		return any(ToFloat64(v)).(T)
	case string:
		return any(ToString(v)).(T)
	case bool:
		return any(ToBool(v)).(T)
	case time.Duration:
		return any(ToDuration(v)).(T)
	case time.Time:
		return any(ToTime(v)).(T)
	default:
		// 尝试 JSON 序列化/反序列化
		if v == nil {
			return zero
		}
		b, err := json.Marshal(v)
		if err != nil {
			return zero
		}
		var result T
		if err := json.Unmarshal(b, &result); err != nil {
			return zero
		}
		return result
	}
}

package cast

import (
	"encoding/json"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// --- 整数转换（int） ---

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
		// 防止大于 int 最大值时静默溢出为负值
		if uint64(val) > uint64(math.MaxInt) {
			return 0, castErr("uint", "int")
		}
		return int(val), nil
	case uint8:
		return int(val), nil
	case uint16:
		return int(val), nil
	case uint32:
		return int(val), nil
	case uint64:
		if val > uint64(math.MaxInt) {
			return 0, castErr("uint64", "int")
		}
		return int(val), nil
	case float32:
		if isNonFinite(float64(val)) {
			return 0, castErr("float32", "int")
		}
		return int(val), nil
	case float64:
		if isNonFinite(val) {
			return 0, castErr("float64", "int")
		}
		return int(val), nil
	case bool:
		if val {
			return 1, nil
		}
		return 0, nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(val), 10, 0)
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
		// 防止大于 int64 最大值时静默溢出为负值
		if uint64(val) > math.MaxInt64 {
			return 0, castErr("uint", "int64")
		}
		return int64(val), nil
	case uint8:
		return int64(val), nil
	case uint16:
		return int64(val), nil
	case uint32:
		return int64(val), nil
	case uint64:
		if val > math.MaxInt64 {
			return 0, castErr("uint64", "int64")
		}
		return int64(val), nil
	case float32:
		if isNonFinite(float64(val)) {
			return 0, castErr("float32", "int64")
		}
		return int64(val), nil
	case float64:
		if isNonFinite(val) {
			return 0, castErr("float64", "int64")
		}
		return int64(val), nil
	case bool:
		if val {
			return 1, nil
		}
		return 0, nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
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

// --- 无符号整数转换（uint） ---

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
		if isNonFinite(float64(val)) {
			return 0, castErr("float32(negative)", "uint64")
		}
		if val < 0 {
			return 0, castErr("float32(negative)", "uint64")
		}
		return uint64(val), nil
	case float64:
		if isNonFinite(val) {
			return 0, castErr("float64(negative)", "uint64")
		}
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
		n, err := strconv.ParseUint(strings.TrimSpace(val), 10, 64)
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

// --- 浮点转换（float） ---

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

package cast

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
)

// --- 布尔转换 ---

// ToBool 将 any 转换为 bool，转换失败返回 false。
// 支持 bool、string（"true"/"1"）、int（非零为 true）。
func ToBool(v any) bool {
	val, _ := ToBoolE(v)
	return val
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

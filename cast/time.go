package cast

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"time"
)

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

// --- 时间转换（time.Time） ---

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

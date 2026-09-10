package cast

import (
	"encoding/json"
	"time"
)

// --- 泛型转换 ---

// To 将 any 转换为目标类型 T。
// T 可以是基本类型或其指针类型，转换失败返回零值。
// 需要判断转换是否成功时，使用 ToE。
//
// 用法：
//
//	n := cast.To[int]("123")       // 123
//	s := cast.To[string](456)      // "456"
//	b := cast.To[bool]("true")     // true
//	d := cast.To[time.Duration]("5s") // 5s
func To[T any](v any) T {
	val, _ := ToE[T](v)
	return val
}

// narrowInt64E 将 v 转为 int64，并校验其能无损放入 bitSize 位的有符号整数。
// 供 ToE 的 int8/int16/int32 分支使用：直接用 int8(n) 这类窄化转换会静默回绕
// （如 int8(200) == -56），必须在转换前判断范围。
func narrowInt64E(v any, bitSize int, name string) (int64, error) {
	n, err := ToInt64E(v)
	if err != nil {
		return 0, err
	}
	if err := checkIntBitSize(n, bitSize, name); err != nil {
		return 0, err
	}
	return n, nil
}

// narrowUint64E 将 v 转为 uint64，并校验其能无损放入 bitSize 位的无符号整数。
func narrowUint64E(v any, bitSize int, name string) (uint64, error) {
	n, err := ToUint64E(v)
	if err != nil {
		return 0, err
	}
	if err := checkUintBitSize(n, bitSize, name); err != nil {
		return 0, err
	}
	return n, nil
}

// ToE 将 any 转换为目标类型 T，返回转换结果与错误。
// 与 To 相比保留错误信息，便于调用方判断是否成功并回退默认值。
func ToE[T any](v any) (T, error) {
	var zero T
	switch any(zero).(type) {
	case int:
		n, err := ToIntE(v)
		if err != nil {
			return zero, err
		}
		return any(n).(T), nil
	case int8:
		n, err := narrowInt64E(v, 8, "int8")
		if err != nil {
			return zero, err
		}
		return any(int8(n)).(T), nil
	case int16:
		n, err := narrowInt64E(v, 16, "int16")
		if err != nil {
			return zero, err
		}
		return any(int16(n)).(T), nil
	case int32:
		n, err := narrowInt64E(v, 32, "int32")
		if err != nil {
			return zero, err
		}
		return any(int32(n)).(T), nil
	case int64:
		n, err := ToInt64E(v)
		if err != nil {
			return zero, err
		}
		return any(n).(T), nil
	case uint:
		n, err := ToUintE(v)
		if err != nil {
			return zero, err
		}
		return any(n).(T), nil
	case uint8:
		n, err := narrowUint64E(v, 8, "uint8")
		if err != nil {
			return zero, err
		}
		return any(uint8(n)).(T), nil
	case uint16:
		n, err := narrowUint64E(v, 16, "uint16")
		if err != nil {
			return zero, err
		}
		return any(uint16(n)).(T), nil
	case uint32:
		n, err := narrowUint64E(v, 32, "uint32")
		if err != nil {
			return zero, err
		}
		return any(uint32(n)).(T), nil
	case uint64:
		n, err := ToUint64E(v)
		if err != nil {
			return zero, err
		}
		return any(n).(T), nil
	case float32:
		n, err := ToFloat32E(v)
		if err != nil {
			return zero, err
		}
		return any(n).(T), nil
	case float64:
		n, err := ToFloat64E(v)
		if err != nil {
			return zero, err
		}
		return any(n).(T), nil
	case string:
		s, err := ToStringE(v)
		if err != nil {
			return zero, err
		}
		return any(s).(T), nil
	case bool:
		b, err := ToBoolE(v)
		if err != nil {
			return zero, err
		}
		return any(b).(T), nil
	case time.Duration:
		d, err := ToDurationE(v)
		if err != nil {
			return zero, err
		}
		return any(d).(T), nil
	case time.Time:
		t, err := ToTimeE(v)
		if err != nil {
			return zero, err
		}
		return any(t).(T), nil
	default:
		// 尝试 JSON 序列化/反序列化
		if v == nil {
			return zero, nil
		}
		b, err := json.Marshal(v)
		if err != nil {
			return zero, err
		}
		var result T
		if err := json.Unmarshal(b, &result); err != nil {
			return zero, err
		}
		return result, nil
	}
}

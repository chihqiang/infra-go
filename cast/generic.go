package cast

import (
	"encoding/json"
	"time"
)

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

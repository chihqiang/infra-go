package cast

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
)

// --- 字符串转换 ---

// ToString 将 any 转换为 string，转换失败返回空字符串。
// 支持 string、[]byte、json.Number、fmt.Stringer 以及基本数值类型。
func ToString(v any) string {
	val, _ := ToStringE(v)
	return val
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

package cast

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
)

// --- String conversion ---

// ToString converts any to string, returning an empty string if the conversion fails.
// Supports string, []byte, json.Number, fmt.Stringer and the basic numeric types.
func ToString(v any) string {
	val, _ := ToStringE(v)
	return val
}

// ToStringE converts any to string and returns the result along with an error.
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
		// fall back to JSON marshal
		b, err := json.Marshal(v)
		if err != nil {
			return "", castErr(reflect.TypeOf(v).String(), "string")
		}
		return string(b), nil
	}
}

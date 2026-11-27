package cast

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// --- Time type conversion ---

// ToDuration converts any to time.Duration, returning the zero value if the conversion fails.
// Supports numeric types (nanoseconds) and strings (such as "5s" or "100ms").
func ToDuration(v any) time.Duration {
	val, _ := ToDurationE(v)
	return val
}

// ToDurationE converts any to time.Duration and returns the result along with an error.
// Numeric types are treated as nanoseconds; strings are parsed with time.ParseDuration.
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

// --- Time conversion (time.Time) ---

// timeLayouts lists the layouts ToTimeE tries for a string, in order. RFC3339
// and RFC3339Nano cover the "2006-01-02T15:04:05Z07:00" form, including
// fractional seconds; time.DateTime and time.DateOnly cover a plain date and
// time, and a date on its own.
var timeLayouts = [...]string{
	time.RFC3339Nano,
	time.RFC3339,
	time.DateTime,
	time.DateOnly,
}

// ToTime converts any to time.Time, returning the zero value if the conversion fails.
// Supports strings (see ToTimeE for the accepted layouts or as a Unix timestamp)
// and numeric types (Unix timestamp).
func ToTime(v any) time.Time {
	val, _ := ToTimeE(v)
	return val
}

// ToTimeE converts any to time.Time and returns the result along with an error.
// A string is tried against every layout in timeLayouts and then parsed as a
// Unix timestamp; a layout without a zone is read as UTC. Numeric types are
// treated as Unix timestamps (seconds).
func ToTimeE(v any) (time.Time, error) {
	switch val := v.(type) {
	case nil:
		return time.Time{}, nil
	case time.Time:
		return val, nil
	case string:
		text := strings.TrimSpace(val)
		for _, layout := range timeLayouts {
			if parsed, err := time.Parse(layout, text); err == nil {
				return parsed, nil
			}
		}
		// A digit-only string is read as a Unix timestamp.
		if n, err := strconv.ParseInt(text, 10, 64); err == nil {
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

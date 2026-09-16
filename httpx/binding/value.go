package binding

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/chihqiang/infra-go/cast"
)

// Error definitions.
var (
	// errUnknownType indicates an unknown type whose value cannot be set.
	errUnknownType = errors.New("unknown type")
)

// setWithProperType sets a value according to the target type.
// Booleans use cast.ToBoolE and Duration uses cast.ToDurationE,
// while integers and floats keep strconv to support bit-width overflow checks.
// Type pre-detection (isTime/isDuration/isFileHeader) comes from the cached fieldMeta,
// avoiding runtime value.Interface().(type) assertions.
func setWithProperType(val string, value reflect.Value, fm *fieldMeta, opt setOptions) error {
	// String values are not trimmed, keeping the original data
	if value.Kind() != reflect.String {
		val = strings.TrimSpace(val)
	}

	switch value.Kind() {
	case reflect.Int:
		return setIntField(val, 0, value)
	case reflect.Int8:
		return setIntField(val, 8, value)
	case reflect.Int16:
		return setIntField(val, 16, value)
	case reflect.Int32:
		return setIntField(val, 32, value)
	case reflect.Int64:
		// time.Duration is backed by int64
		if fm != nil && fm.isDuration {
			return setTimeDuration(val, value)
		}
		return setIntField(val, 64, value)
	case reflect.Uint:
		return setUintField(val, 0, value)
	case reflect.Uint8:
		return setUintField(val, 8, value)
	case reflect.Uint16:
		return setUintField(val, 16, value)
	case reflect.Uint32:
		return setUintField(val, 32, value)
	case reflect.Uint64:
		return setUintField(val, 64, value)
	case reflect.Bool:
		return setBoolField(val, value)
	case reflect.Float32:
		return setFloatField(val, 32, value)
	case reflect.Float64:
		return setFloatField(val, 64, value)
	case reflect.String:
		value.SetString(val)
	case reflect.Struct:
		if fm != nil && fm.isTime {
			return setTimeField(val, fm, value)
		}
		if fm != nil && fm.isFileHeader {
			return nil
		}
		// Other structs fall back to JSON parsing
		return json.Unmarshal([]byte(val), value.Addr().Interface())
	case reflect.Map:
		return json.Unmarshal([]byte(val), value.Addr().Interface())
	case reflect.Ptr:
		if !value.Elem().IsValid() {
			value.Set(reflect.New(value.Type().Elem()))
		}
		return setWithProperType(val, value.Elem(), fm, opt)
	default:
		return errUnknownType
	}
	return nil
}

// setIntField sets a signed integer field.
// strconv.ParseInt is kept to support bit-width overflow checks.
func setIntField(val string, bitSize int, field reflect.Value) error {
	if val == "" {
		val = "0"
	}
	intVal, err := strconv.ParseInt(val, 10, bitSize)
	if err == nil {
		field.SetInt(intVal)
	}
	return err
}

// setUintField sets an unsigned integer field.
// strconv.ParseUint is kept to support bit-width overflow checks.
func setUintField(val string, bitSize int, field reflect.Value) error {
	if val == "" {
		val = "0"
	}
	uintVal, err := strconv.ParseUint(val, 10, bitSize)
	if err == nil {
		field.SetUint(uintVal)
	}
	return err
}

// setBoolField sets a boolean field.
// It converts the value using cast.ToBoolE.
func setBoolField(val string, field reflect.Value) error {
	if val == "" {
		field.SetBool(false)
		return nil
	}
	b, err := cast.ToBoolE(val)
	if err != nil {
		return err
	}
	field.SetBool(b)
	return nil
}

// setFloatField sets a floating-point field.
// strconv.ParseFloat is kept to support bit-width overflow checks.
func setFloatField(val string, bitSize int, field reflect.Value) error {
	if val == "" {
		val = "0.0"
	}
	floatVal, err := strconv.ParseFloat(val, bitSize)
	if err == nil {
		field.SetFloat(floatVal)
	}
	return err
}

// setTimeField sets a time.Time field.
// The layout can be set with the `time_format` tag and defaults to RFC3339.
// The time-related tag information is already cached in fieldMeta.
func setTimeField(val string, fm *fieldMeta, value reflect.Value) error {
	timeFormat := fm.timeFormat
	if timeFormat == "" {
		timeFormat = time.RFC3339
	}

	if val == "" {
		value.Set(reflect.ValueOf(time.Time{}))
		return nil
	}

	// Support unix timestamps
	switch tf := strings.ToLower(timeFormat); tf {
	case "unix", "unixmilli", "unixmicro", "unixnano":
		tv, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return err
		}
		var t time.Time
		switch tf {
		case "unix":
			t = time.Unix(tv, 0)
		case "unixmilli":
			t = time.UnixMilli(tv)
		case "unixmicro":
			t = time.UnixMicro(tv)
		default:
			t = time.Unix(0, tv)
		}
		value.Set(reflect.ValueOf(t))
		return nil
	}

	l := time.Local
	if fm.timeUTC {
		l = time.UTC
	}
	if locTag := fm.timeLocation; locTag != "" {
		loc, err := time.LoadLocation(locTag)
		if err != nil {
			return err
		}
		l = loc
	}

	t, err := time.ParseInLocation(timeFormat, val, l)
	if err != nil {
		return err
	}
	value.Set(reflect.ValueOf(t))
	return nil
}

// setTimeDuration sets a time.Duration field.
// It converts the value using cast.ToDurationE.
func setTimeDuration(val string, value reflect.Value) error {
	if val == "" {
		value.Set(reflect.ValueOf(time.Duration(0)))
		return nil
	}
	d, err := cast.ToDurationE(val)
	if err != nil {
		return err
	}
	value.Set(reflect.ValueOf(d))
	return nil
}

// setSlice sets a slice field.
func setSlice(vals []string, value reflect.Value, fm *fieldMeta, opt setOptions) error {
	slice := reflect.MakeSlice(value.Type(), len(vals), len(vals))
	for i, s := range vals {
		if err := setWithProperType(s, slice.Index(i), fm, opt); err != nil {
			return err
		}
	}
	value.Set(slice)
	return nil
}

// setFormMap fills the form data directly into a map-typed target.
func setFormMap(ptr any, form map[string][]string) error {
	el := reflect.TypeOf(ptr).Elem()

	if el.Kind() == reflect.Slice {
		ptrMap, ok := ptr.(map[string][]string)
		if !ok {
			return errors.New("can not convert to map slices of strings")
		}
		for k, v := range form {
			ptrMap[k] = append(ptrMap[k], v...)
		}
		return nil
	}

	ptrMap, ok := ptr.(map[string]string)
	if !ok {
		return errors.New("can not convert to map of strings")
	}
	for k, v := range form {
		if len(v) > 0 {
			ptrMap[k] = v[len(v)-1] // take the last value
		}
	}
	return nil
}

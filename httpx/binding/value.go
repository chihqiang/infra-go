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

// 错误定义。
var (
	// errUnknownType 未知类型，无法设置值。
	errUnknownType = errors.New("unknown type")
)

// setWithProperType 根据目标类型设置值。
// 布尔类型使用 cast.ToBoolE，Duration 类型使用 cast.ToDurationE，
// 整数和浮点类型保留 strconv 以支持位宽溢出检查。
// 类型预判（isTime/isDuration/isFileHeader）来自缓存的 fieldMeta，
// 避免运行时 value.Interface().(type) 断言。
func setWithProperType(val string, value reflect.Value, fm *fieldMeta, opt setOptions) error {
	// 字符串类型不去除空格，保留原始数据
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
		// time.Duration 底层是 int64
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
		// 其他结构体尝试 JSON 解析
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

// setIntField 设置有符号整数字段。
// 保留 strconv.ParseInt 以支持位宽溢出检查。
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

// setUintField 设置无符号整数字段。
// 保留 strconv.ParseUint 以支持位宽溢出检查。
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

// setBoolField 设置布尔字段。
// 使用 cast.ToBoolE 进行类型转换。
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

// setFloatField 设置浮点字段。
// 保留 strconv.ParseFloat 以支持位宽溢出检查。
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

// setTimeField 设置 time.Time 字段。
// 支持通过 `time_format` 标签指定格式，默认 RFC3339。
// 时间相关标签信息已缓存在 fieldMeta 中。
func setTimeField(val string, fm *fieldMeta, value reflect.Value) error {
	timeFormat := fm.timeFormat
	if timeFormat == "" {
		timeFormat = time.RFC3339
	}

	if val == "" {
		value.Set(reflect.ValueOf(time.Time{}))
		return nil
	}

	// 支持 unix 时间戳
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

// setTimeDuration 设置 time.Duration 字段。
// 使用 cast.ToDurationE 进行类型转换。
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

// setSlice 设置切片字段。
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

// setFormMap 将表单数据直接填充到 map 类型目标。
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
			ptrMap[k] = v[len(v)-1] // 取最后一个值
		}
	}
	return nil
}

package binding

import (
	"encoding"
	"fmt"
	"mime/multipart"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const defaultMemory = 32 << 20

var timeType = reflect.TypeOf(time.Time{})
var durationType = reflect.TypeOf(time.Duration(0))
var fileHeaderType = reflect.TypeOf((*multipart.FileHeader)(nil))

// mapByTag fills dst from values, reading key names from the given struct tag
// with a json tag fallback.
func mapByTag(dst any, values map[string][]string, tag string) error {
	if err := checkDestination(dst); err != nil {
		return err
	}
	if _, err := fill(reflect.ValueOf(dst), values, nil, tag); err != nil {
		return err
	}
	return validate(dst)
}

// mapWithFiles fills dst from both form values and uploaded files.
func mapWithFiles(dst any, values map[string][]string, files map[string][]*multipart.FileHeader, tag string) error {
	if err := checkDestination(dst); err != nil {
		return err
	}
	if _, err := fill(reflect.ValueOf(dst), values, files, tag); err != nil {
		return err
	}
	return validate(dst)
}

// fill walks value and assigns every matching field, reporting whether anything
// was set. It allocates nil pointers on demand.
func fill(value reflect.Value, values map[string][]string, files map[string][]*multipart.FileHeader, tag string) (bool, error) {
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			created := reflect.New(value.Type().Elem())
			set, err := fill(created.Elem(), values, files, tag)
			if err != nil || !set {
				return false, err
			}
			value.Set(created)
			return true, nil
		}
		return fill(value.Elem(), values, files, tag)
	case reflect.Struct:
		return fillStruct(value, values, files, tag)
	default:
		return false, nil
	}
}

func fillStruct(value reflect.Value, values map[string][]string, files map[string][]*multipart.FileHeader, tag string) (bool, error) {
	structType := value.Type()
	setAny := false

	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		fieldValue := value.Field(i)
		if !fieldValue.CanSet() {
			continue
		}

		if field.Anonymous {
			set, err := fill(fieldValue, values, files, tag)
			if err != nil {
				return false, err
			}
			setAny = setAny || set
			continue
		}

		key, ok := fieldKey(field, tag)
		if !ok {
			continue
		}

		set, err := setField(fieldValue, field, key, values, files)
		if err != nil {
			return false, err
		}
		setAny = setAny || set
	}

	return setAny, nil
}

// fieldKey resolves the lookup key for a field, honouring "-" as skip.
func fieldKey(field reflect.StructField, tag string) (string, bool) {
	for _, name := range []string{tag, "json"} {
		if name == "" {
			continue
		}
		value, found := field.Tag.Lookup(name)
		if !found {
			continue
		}
		key := strings.Split(value, ",")[0]
		if key == "-" {
			return "", false
		}
		if key != "" {
			return key, true
		}
	}
	return field.Name, true
}

func setField(value reflect.Value, field reflect.StructField, key string,
	values map[string][]string, files map[string][]*multipart.FileHeader,
) (bool, error) {
	if value.Kind() == reflect.Struct && value.Type() == timeType {
		raw, ok := firstValue(values, key)
		if !ok {
			return false, nil
		}
		return setTime(value, key, raw)
	}

	if value.Kind() == reflect.Slice && value.Type().Elem() == fileHeaderType {
		return setFiles(value, key, field, files)
	}

	if value.Kind() == reflect.Slice && value.Type().Elem().Kind() != reflect.Uint8 {
		items, ok := values[key]
		if !ok {
			return false, nil
		}
		return setSlice(value, key, items)
	}

	raw, ok := firstValue(values, key)
	if !ok {
		return false, nil
	}
	return setScalar(value, key, raw)
}

func firstValue(values map[string][]string, key string) (string, bool) {
	items, ok := values[key]
	if !ok {
		items, ok = values[strings.ToLower(key)]
	}
	if !ok || len(items) == 0 {
		return "", false
	}
	return items[0], true
}

func setScalar(value reflect.Value, key, raw string) (bool, error) {
	if value.Kind() == reflect.Pointer {
		created := reflect.New(value.Type().Elem())
		set, err := setScalar(created.Elem(), key, raw)
		if err != nil || !set {
			return false, err
		}
		value.Set(created)
		return true, nil
	}

	if value.CanAddr() {
		if unmarshaler, ok := value.Addr().Interface().(encoding.TextUnmarshaler); ok {
			if err := unmarshaler.UnmarshalText([]byte(raw)); err != nil {
				return false, badRequest("rest/binding: field %q: %v", key, err)
			}
			return true, nil
		}
	}

	switch value.Kind() {
	case reflect.String:
		value.SetString(raw)
	case reflect.Bool:
		parsed, err := parseBool(raw)
		if err != nil {
			return false, badRequest("rest/binding: field %q: %v", key, err)
		}
		value.SetBool(parsed)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if value.Type() == durationType {
			parsed, err := time.ParseDuration(raw)
			if err != nil {
				return false, badRequest("rest/binding: field %q: %v", key, err)
			}
			value.SetInt(int64(parsed))
			return true, nil
		}
		parsed, err := strconv.ParseInt(raw, 10, value.Type().Bits())
		if err != nil {
			return false, badRequest("rest/binding: field %q: %v", key, err)
		}
		value.SetInt(parsed)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		parsed, err := strconv.ParseUint(raw, 10, value.Type().Bits())
		if err != nil {
			return false, badRequest("rest/binding: field %q: %v", key, err)
		}
		value.SetUint(parsed)
	case reflect.Float32, reflect.Float64:
		parsed, err := strconv.ParseFloat(raw, value.Type().Bits())
		if err != nil {
			return false, badRequest("rest/binding: field %q: %v", key, err)
		}
		value.SetFloat(parsed)
	case reflect.Slice:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			value.SetBytes([]byte(raw))
			return true, nil
		}
		return false, badRequest("rest/binding: field %q: unsupported slice type %s", key, value.Type())
	default:
		return false, badRequest("rest/binding: field %q: unsupported type %s", key, value.Type())
	}

	return true, nil
}

func setSlice(value reflect.Value, key string, items []string) (bool, error) {
	if len(items) == 0 {
		return false, nil
	}

	slice := reflect.MakeSlice(value.Type(), 0, len(items))
	for _, item := range items {
		element := reflect.New(value.Type().Elem()).Elem()
		if _, err := setScalar(element, key, item); err != nil {
			return false, err
		}
		slice = reflect.Append(slice, element)
	}
	value.Set(slice)
	return true, nil
}

func setFiles(value reflect.Value, key string, field reflect.StructField,
	files map[string][]*multipart.FileHeader,
) (bool, error) {
	if files == nil {
		return false, nil
	}

	uploaded, ok := files[key]
	if !ok || len(uploaded) == 0 {
		return false, nil
	}

	slice := reflect.MakeSlice(value.Type(), 0, len(uploaded))
	for _, header := range uploaded {
		slice = reflect.Append(slice, reflect.ValueOf(header))
	}
	value.Set(slice)
	return true, nil
}

func setTime(value reflect.Value, key, raw string) (bool, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			value.Set(reflect.ValueOf(parsed))
			return true, nil
		}
	}
	return false, badRequest("rest/binding: field %q: cannot parse %q as time", key, raw)
}

func parseBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "t", "true", "yes", "on", "y":
		return true, nil
	case "0", "f", "false", "no", "off", "n", "":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean %q", raw)
	}
}

package mapping

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chihqiang/infra-go/cast"
)

const (
	// ignoreKey 表示忽略该字段。
	ignoreKey = "-"
	// delimiter 用于连接嵌套字段的完整路径名。
	delimiter = '.'
)

var (
	errValueNotSettable = fmt.Errorf("value is not settable, must pass a struct pointer")
	errValueNotStruct   = fmt.Errorf("value type is not struct")
	errTypeMismatch     = fmt.Errorf("type mismatch")
	errUnsupportedType  = fmt.Errorf("unsupported field type")

	durationType = reflect.TypeOf(time.Duration(0))
	intSize      = 32 << (^uint(0) >> 63) // 32 或 64

	// structRequiredCache 缓存结构体是否包含必填字段。
	structRequiredCache = make(map[reflect.Type]bool)
	structCacheLock     sync.RWMutex
)

// Deref 解引用指针类型，返回其基础类型。
func Deref(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

// ValidatePtr 验证 v 是否为有效的非 nil 指针。
func ValidatePtr(v reflect.Value) error {
	if !v.IsValid() || v.Kind() != reflect.Ptr || v.IsNil() {
		return fmt.Errorf("not a valid pointer: %v", v)
	}
	return nil
}

// SetValue 设置目标值，自动处理指针类型。
func SetValue(tp reflect.Type, value, target reflect.Value) {
	value.Set(convertTypeOfPtr(tp, target))
}

// convertTypeOfPtr 处理指针类型的转换。
func convertTypeOfPtr(tp reflect.Type, target reflect.Value) reflect.Value {
	if tp.Kind() == reflect.Ptr && target.CanAddr() {
		tp = tp.Elem()
		target = target.Addr()
	}

	for tp.Kind() == reflect.Ptr {
		p := reflect.New(target.Type())
		p.Elem().Set(target)
		target = p
		tp = tp.Elem()
	}

	return target
}

// maybeNewValue 如果是指针类型且为 nil，则创建新值。
func maybeNewValue(fieldType reflect.Type, value reflect.Value) {
	if fieldType.Kind() == reflect.Ptr && value.IsNil() {
		value.Set(reflect.New(value.Type().Elem()))
	}
}

// ensureValue 确保嵌套成员不为 nil。
func ensureValue(v reflect.Value) reflect.Value {
	for {
		if v.Kind() != reflect.Ptr {
			break
		}
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	return v
}

// joinName 连接字段路径名。
func joinName(parent, child string) string {
	if len(parent) == 0 {
		return child
	}
	if len(child) == 0 {
		return parent
	}
	return parent + string(delimiter) + child
}

// usingDifferentKeys 检查字段是否使用了与当前解析器不同的标签键。
func usingDifferentKeys(key string, field reflect.StructField) bool {
	if len(field.Tag) > 0 {
		if _, ok := field.Tag.Lookup(key); !ok {
			return true
		}
	}
	return false
}

// lookupKey 从 map 中查找指定键的值（支持点号分隔的嵌套键）。
func lookupKey(m map[string]any, key string) (any, bool) {
	if m == nil {
		return nil, false
	}

	keys := readKeys(key)
	return lookupWithChainedKeys(m, keys)
}

// readKeys 将键按点号分隔为多段。
func readKeys(key string) []string {
	return strings.FieldsFunc(key, func(c rune) bool {
		return c == delimiter
	})
}

// lookupWithChainedKeys 按链式键查找值。
func lookupWithChainedKeys(m map[string]any, keys []string) (any, bool) {
	switch len(keys) {
	case 0:
		return nil, false
	case 1:
		v, ok := m[keys[0]]
		return v, ok
	default:
		v, ok := m[keys[0]]
		if !ok {
			return nil, false
		}
		nestedMap, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		return lookupWithChainedKeys(nestedMap, keys[1:])
	}
}

// errAmbiguousKey 表示同一层级存在多个经 canonical 规范化后同名的候选键。
var errAmbiguousKey = errors.New("ambiguous key")

// lookupKeyCanonical 按链式键查找值，未精确命中时按 canonical 形式做不敏感匹配。
//
// 大小写不敏感匹配由此函数完成，而不是由调用方预先改写输入 map 的键：
// map 的键同时也是 map 类型字段的数据，预先小写化会破坏用户数据
// （例如 labels: {AppName: x} 会被静默写成 appname）。
//
// 逐层匹配规则：
//  1. 键段的原始形式精确命中；
//  2. 键段的 canonical 形式精确命中；
//  3. 在候选键中查找满足 canonical(k) == canonical(段) 的键，唯一时返回；
//  4. 多个候选键（仅大小写/形式不同）时返回 errAmbiguousKey，
//     而不是依赖 map 遍历顺序任选其一。
func lookupKeyCanonical(m map[string]any, key string, canonical func(string) string) (any, bool, error) {
	if m == nil {
		return nil, false, nil
	}

	keys := readKeys(key)
	if len(keys) == 0 {
		return nil, false, nil
	}

	current := m
	for i, segment := range keys {
		// 1. 原始键精确匹配（canonicalKey 不会改变键段语义时最常见）
		v, ok := current[segment]
		if !ok {
			want := canonical(segment)
			// 2. canonical 形式精确匹配
			v, ok = current[want]
			if !ok {
				// 3. 按 canonical 形式不敏感扫描
				matched := false
				for k, cv := range current {
					if canonical(k) != want {
						continue
					}
					if matched {
						return nil, false, fmt.Errorf("%w: %q matches multiple keys (%s)", errAmbiguousKey, want, describeKeys(current))
					}
					v, matched = cv, true
				}
				if !matched {
					return nil, false, nil
				}
			}
		}

		if i == len(keys)-1 {
			return v, true, nil
		}

		nested, ok := v.(map[string]any)
		if !ok {
			return nil, false, nil
		}
		current = nested
	}

	return nil, false, nil
}

// describeKeys 返回排序后的键列表，用于生成稳定的错误信息。
func describeKeys(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// convertTypeFromString 将字符串转为指定类型的基本值。
// 布尔类型使用 cast.ToBoolE，浮点类型使用 cast.ToFloat64E，
// 整数类型保留 strconv 以支持位宽溢出检查。
func convertTypeFromString(kind reflect.Kind, str string) (any, error) {
	switch kind {
	case reflect.Bool:
		return cast.ToBoolE(str)
	case reflect.Int:
		return strconv.ParseInt(str, 10, intSize)
	case reflect.Int8:
		return strconv.ParseInt(str, 10, 8)
	case reflect.Int16:
		return strconv.ParseInt(str, 10, 16)
	case reflect.Int32:
		return strconv.ParseInt(str, 10, 32)
	case reflect.Int64:
		return strconv.ParseInt(str, 10, 64)
	case reflect.Uint:
		return strconv.ParseUint(str, 10, intSize)
	case reflect.Uint8:
		return strconv.ParseUint(str, 10, 8)
	case reflect.Uint16:
		return strconv.ParseUint(str, 10, 16)
	case reflect.Uint32:
		return strconv.ParseUint(str, 10, 32)
	case reflect.Uint64:
		return strconv.ParseUint(str, 10, 64)
	case reflect.Float32, reflect.Float64:
		return cast.ToFloat64E(str)
	case reflect.String:
		return str, nil
	default:
		return nil, errUnsupportedType
	}
}

// setMatchedPrimitiveValue 将已转换的值设置到 reflect.Value 上。
func setMatchedPrimitiveValue(kind reflect.Kind, value reflect.Value, v any) error {
	switch kind {
	case reflect.Bool:
		value.SetBool(v.(bool))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(v.(int64))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value.SetUint(v.(uint64))
	case reflect.Float32, reflect.Float64:
		value.SetFloat(v.(float64))
	case reflect.String:
		value.SetString(v.(string))
	default:
		return errUnsupportedType
	}
	return nil
}

// setSameKindValue 设置同类型的值，必要时进行类型转换。
func setSameKindValue(targetType reflect.Type, target reflect.Value, value any) {
	if reflect.ValueOf(value).Type().AssignableTo(targetType) {
		target.Set(reflect.ValueOf(value))
	} else {
		target.Set(reflect.ValueOf(value).Convert(targetType))
	}
}

// validateOptions 验证值是否在允许的选项列表中。
// 使用 cast.ToString 将值转为字符串进行比较。
func validateOptions(val any, options []string, fullName string) error {
	if len(options) == 0 {
		return nil
	}

	checkValue := cast.ToString(val)

	for _, opt := range options {
		if opt == checkValue {
			return nil
		}
	}

	return fmt.Errorf(`value %q of field %q is not in allowed options %v`, checkValue, fullName, options)
}

// validateValueRange 验证数值是否在范围内。
func validateValueRange(mapValue any, opts *fieldOptions, fullName string) error {
	if opts == nil || opts.Range == nil {
		return nil
	}

	fv, err := cast.ToFloat64E(mapValue)
	if err != nil {
		return fmt.Errorf("value of field %q cannot be used for range validation", fullName)
	}

	if !opts.isInRange(fv) {
		return fmt.Errorf("value %v of field %q is out of range", mapValue, fullName)
	}

	return nil
}

// validateRangeForType 按目标类型校验值是否满足 range 约束。
//
// 相比 validateValueRange，它会先按目标类型归一化值：time.Duration 的
// range 以纳秒为单位比较（与其底层 int64 表示一致），因此 "5s" 这类
// duration 文本会先解析为 time.Duration 再比较。
//
// 所有写入字段的路径都必须调用本函数（或 validateValueRange），
// 否则 range 约束会因值的来源或表示形式不同而被静默绕过。
func validateRangeForType(derefedType reflect.Type, mapValue any, opts *fieldOptions, fullName string) error {
	if opts == nil || opts.Range == nil {
		return nil
	}

	if derefedType == durationType {
		d, err := cast.ToDurationE(mapValue)
		if err != nil {
			return fmt.Errorf("value of field %q cannot be used for range validation: %w", fullName, err)
		}
		if !opts.isInRange(float64(d)) {
			return fmt.Errorf("value %v of field %q is out of range", mapValue, fullName)
		}
		return nil
	}

	return validateValueRange(mapValue, opts, fullName)
}

// structValueRequired 检查结构体类型是否包含必填字段。
func structValueRequired(tag string, tp reflect.Type) bool {
	structCacheLock.RLock()
	required, ok := structRequiredCache[tp]
	structCacheLock.RUnlock()
	if ok {
		return required
	}

	required = implicitValueRequiredStruct(tag, tp)
	structCacheLock.Lock()
	structRequiredCache[tp] = required
	structCacheLock.Unlock()

	return required
}

// implicitValueRequiredStruct 递归检查结构体是否包含必填字段。
func implicitValueRequiredStruct(tag string, tp reflect.Type) bool {
	tp = Deref(tp)
	if tp.Kind() != reflect.Struct {
		return true
	}

	for i := 0; i < tp.NumField(); i++ {
		childField := tp.Field(i)
		if !childField.IsExported() {
			continue
		}

		if usingDifferentKeys(tag, childField) {
			return true
		}

		_, opts, err := parseKeyAndOptions(tag, childField)
		if err != nil {
			return true
		}

		if opts == nil {
			childType := Deref(childField.Type)
			if childType.Kind() != reflect.Struct {
				return true
			}
			if childType == durationType {
				return true
			}
			if implicitValueRequiredStruct(tag, childType) {
				return true
			}
		} else if !opts.Optional && len(opts.Default) == 0 && opts.EnvVar == "" {
			return true
		}
	}

	return false
}

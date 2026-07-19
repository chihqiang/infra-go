package mapping

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/chihqiang/infra-go/cast"
)

// jsonTagKey json 标签键名。
const jsonTagKey = "json"

var emptyMap = map[string]any{}

// Unmarshaler 是配置反序列化器，负责将 map[string]any 反序列化到结构体，
// 并处理默认值、环境变量、选项验证、范围验证等扩展功能。
type Unmarshaler struct {
	key          string // 结构体标签键名，通常是 "json"
	fillDefault  bool   // 是否仅填充默认值
	fromString   bool   // 是否从字符串解析所有值
	canonicalKey func(string) string // 键名规范化函数（如转小写）
}

// UnmarshalOption 定义 Unmarshaler 的配置选项。
type UnmarshalOption func(*Unmarshaler)

// WithDefault 设置仅填充默认值模式。
func WithDefault() UnmarshalOption {
	return func(u *Unmarshaler) {
		u.fillDefault = true
	}
}

// WithStringValues 设置从字符串模式解析所有值。
func WithStringValues() UnmarshalOption {
	return func(u *Unmarshaler) {
		u.fromString = true
	}
}

// WithCanonicalKeyFunc 设置键名规范化函数，用于大小写不敏感匹配。
// 例如传入 strings.ToLower 后，配置文件中的 "logMode" 可以匹配字段名 "LogMode"。
func WithCanonicalKeyFunc(f func(string) string) UnmarshalOption {
	return func(u *Unmarshaler) {
		u.canonicalKey = f
	}
}

// NewUnmarshaler 创建一个新的反序列化器。
func NewUnmarshaler(key string, opts ...UnmarshalOption) *Unmarshaler {
	u := &Unmarshaler{key: key}
	for _, opt := range opts {
		opt(u)
	}
	return u
}

// Unmarshal 将 map 数据反序列化到目标结构体 v 中。
func (u *Unmarshaler) Unmarshal(m map[string]any, v any) error {
	rv := reflect.ValueOf(v)
	if err := ValidatePtr(rv); err != nil {
		return err
	}

	elemType := Deref(rv.Type())
	if elemType.Kind() != reflect.Struct {
		return errValueNotStruct
	}

	valElem := rv.Elem()
	if valElem.Kind() == reflect.Ptr {
		target := reflect.New(elemType).Elem()
		SetValue(rv.Type().Elem(), valElem, target)
		valElem = target
	}

	if u.fillDefault {
		return u.fillDefaultStruct(elemType, valElem, "")
	}

	return u.processStruct(elemType, valElem, m, "")
}

// fillDefaultStruct 仅填充默认值和环境变量。
func (u *Unmarshaler) fillDefaultStruct(structType reflect.Type, structValue reflect.Value, fullName string) error {
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if !field.IsExported() {
			continue
		}

		fieldValue := structValue.Field(i)
		if field.Anonymous {
			derefedType := Deref(field.Type)
			if derefedType.Kind() == reflect.Struct {
				maybeNewValue(field.Type, fieldValue)
				if err := u.fillDefaultStruct(derefedType, reflect.Indirect(fieldValue), fullName); err != nil {
					return err
				}
			}
			continue
		}

		key, opts, err := parseKeyAndOptions(u.key, field)
		if err != nil {
			return err
		}
		if key == ignoreKey {
			continue
		}

		fn := joinName(fullName, key)

		// 检查字段是否已有非零值
		if !fieldValue.IsZero() {
			return fmt.Errorf("field %q must be zero value when filling default", fn)
		}

		// 优先从环境变量读取
		if opts != nil && opts.EnvVar != "" {
			if envVal := os.Getenv(opts.EnvVar); envVal != "" {
				if err := u.setEnvValue(field.Type, fieldValue, envVal, opts, fn); err != nil {
					return err
				}
				continue
			}
		}

		// 填充默认值
		if defaultValue, ok := opts.hasDefault(); ok {
			if err := u.setDefaultValue(field.Type, fieldValue, defaultValue, fn); err != nil {
				return err
			}
			continue
		}

		// 对于非指针的嵌套结构体，递归填充
		derefedType := Deref(field.Type)
		if field.Type.Kind() != reflect.Ptr && derefedType.Kind() == reflect.Struct {
			if err := u.fillDefaultStruct(derefedType, fieldValue, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// processStruct 处理结构体的所有字段。
func (u *Unmarshaler) processStruct(structType reflect.Type, structValue reflect.Value, m map[string]any, fullName string) error {
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if !field.IsExported() {
			continue
		}

		fieldValue := structValue.Field(i)

		if field.Anonymous {
			if err := u.processAnonymousField(field, fieldValue, m, fullName); err != nil {
				return err
			}
		} else {
			if err := u.processNamedField(field, fieldValue, m, fullName); err != nil {
				return err
			}
		}
	}
	return nil
}

// processAnonymousField 处理匿名（嵌入）字段。
func (u *Unmarshaler) processAnonymousField(field reflect.StructField, value reflect.Value, m map[string]any, fullName string) error {
	key, opts, err := parseKeyAndOptions(u.key, field)
	if err != nil {
		return err
	}
	if key == ignoreKey {
		return nil
	}

	derefedType := Deref(field.Type)

	// 如果嵌入类型不是结构体，按普通字段处理
	if derefedType.Kind() != reflect.Struct {
		return u.processNamedField(field, value, m, fullName)
	}

	maybeNewValue(field.Type, value)
	indirectValue := reflect.Indirect(value)

	// 处理可选的嵌入结构体
	if opts != nil && opts.isOptional() {
		hasValue := u.hasAnySubField(derefedType, m)
		if !hasValue {
			return nil
		}
	}

	// 递归处理嵌入结构体的字段
	for i := 0; i < derefedType.NumField(); i++ {
		subField := derefedType.Field(i)
		if !subField.IsExported() {
			continue
		}
		if err := u.processField(subField, indirectValue.Field(i), m, fullName); err != nil {
			return err
		}
	}

	return nil
}

// processNamedField 处理命名字段。
func (u *Unmarshaler) processNamedField(field reflect.StructField, value reflect.Value, m map[string]any, fullName string) error {
	if !field.IsExported() {
		return nil
	}
	return u.processField(field, value, m, fullName)
}

// processField 处理单个字段（统一入口）。
func (u *Unmarshaler) processField(field reflect.StructField, value reflect.Value, m map[string]any, fullName string) error {
	if usingDifferentKeys(u.key, field) {
		return nil
	}

	if field.Anonymous {
		return u.processAnonymousField(field, value, m, fullName)
	}

	key, opts, err := parseKeyAndOptions(u.key, field)
	if err != nil {
		return err
	}
	if key == ignoreKey {
		return nil
	}

	fn := joinName(fullName, key)

	// 优先从环境变量读取
	if opts != nil && opts.EnvVar != "" {
		if envVal := os.Getenv(opts.EnvVar); envVal != "" {
			return u.setEnvValue(field.Type, value, envVal, opts, fn)
		}
	}

	// 从配置 map 中查找值
	lookupMapKey := key
	if u.canonicalKey != nil {
		lookupMapKey = u.canonicalKey(key)
	}
	mapValue, hasValue := lookupKey(m, lookupMapKey)

	if !hasValue {
		return u.processFieldWithoutValue(field.Type, value, opts, fn)
	}

	return u.processFieldWithValue(field.Type, value, mapValue, opts, fn)
}

// processFieldWithValue 当配置中存在值时处理字段。
func (u *Unmarshaler) processFieldWithValue(fieldType reflect.Type, value reflect.Value, mapValue any, opts *fieldOptions, fullName string) error {
	if mapValue == nil {
		if opts.isOptional() {
			return nil
		}
		return fmt.Errorf("field %q cannot be nil", fullName)
	}

	if !value.CanSet() {
		return fmt.Errorf("field %q is not settable", fullName)
	}

	maybeNewValue(fieldType, value)

	derefedType := Deref(fieldType)

	// time.Duration 底层类型是 int64，需要优先处理
	if derefedType == durationType {
		return u.setDurationValue(fieldType, value, mapValue, fullName)
	}

	typeKind := derefedType.Kind()

	switch typeKind {
	case reflect.Struct:
		return u.setStructValue(fieldType, value, mapValue, opts, fullName)
	case reflect.Slice:
		return u.setSliceValue(fieldType, value, mapValue, opts, fullName)
	case reflect.Map:
		return u.setMapValue(fieldType, value, mapValue, opts, fullName)
	default:
		return u.setBasicValue(fieldType, value, mapValue, opts, fullName)
	}
}

// processFieldWithoutValue 当配置中不存在值时处理字段。
func (u *Unmarshaler) processFieldWithoutValue(fieldType reflect.Type, value reflect.Value, opts *fieldOptions, fullName string) error {
	// 优先使用默认值
	if defaultValue, ok := opts.hasDefault(); ok {
		return u.setDefaultValue(fieldType, value, defaultValue, fullName)
	}

	derefedType := Deref(fieldType)

	// time.Duration 底层类型是 int64，需要优先处理
	if derefedType == durationType {
		if opts.isOptional() {
			return nil
		}
		return fmt.Errorf("field %q not set", fullName)
	}

	typeKind := derefedType.Kind()

	switch typeKind {
	case reflect.Struct:
		// 对于结构体，检查是否有必填字段
		if !opts.isOptional() {
			required := structValueRequired(u.key, derefedType)
			if required {
				return fmt.Errorf("field %q not set", fullName)
			}
			// 结构体没有必填字段，用空 map 递归处理
			return u.processStruct(derefedType, ensureValue(value), emptyMap, fullName)
		}
	case reflect.Slice, reflect.Map:
		if !opts.isOptional() {
			return nil
		}
	default:
		if !opts.isOptional() {
			return fmt.Errorf("field %q not set", fullName)
		}
	}

	return nil
}

// setBasicValue 设置基本类型字段的值。
func (u *Unmarshaler) setBasicValue(fieldType reflect.Type, value reflect.Value, mapValue any, opts *fieldOptions, fullName string) error {
	derefedType := Deref(fieldType)
	typeKind := derefedType.Kind()

	// 如果是 fromString 模式，将值转为字符串再解析
	if u.fromString || opts.isFromString() {
		strVal, err := cast.ToStringE(mapValue)
		if err != nil {
			return fmt.Errorf("field %q expects string value, but got %T", fullName, mapValue)
		}
		if err := validateOptions(strVal, opts.allowedOptions(), fullName); err != nil {
			return err
		}
		if err := validateValueRange(strVal, opts, fullName); err != nil {
			return err
		}
		return setStringValue(typeKind, value, strVal, fullName)
	}

	// 处理 json.Number 类型
	if numVal, ok := mapValue.(json.Number); ok {
		return u.setNumberValue(fieldType, value, numVal, opts, fullName)
	}

	// 处理原生类型
	valueKind := reflect.TypeOf(mapValue).Kind()
	if typeKind == valueKind {
		if err := validateValueRange(mapValue, opts, fullName); err != nil {
			return err
		}
		if err := validateOptions(mapValue, opts.allowedOptions(), fullName); err != nil {
			return err
		}
		setSameKindValue(derefedType, ensureValue(value), mapValue)
		setValue(fieldType, value, ensureValue(value))
		return nil
	}

	// 尝试将值转为字符串再解析
	return u.setConvertedValue(fieldType, value, mapValue, opts, fullName)
}

// setNumberValue 设置数值类型字段的值（从 json.Number）。
func (u *Unmarshaler) setNumberValue(fieldType reflect.Type, value reflect.Value, numVal json.Number, opts *fieldOptions, fullName string) error {
	derefedType := Deref(fieldType)
	typeKind := derefedType.Kind()

	// 范围验证
	if opts != nil && opts.Range != nil {
		fv, err := numVal.Float64()
		if err != nil {
			return fmt.Errorf("value %q of field %q cannot be converted to float: %w", numVal.String(), fullName, err)
		}
		if !opts.isInRange(fv) {
			return fmt.Errorf("value %s of field %q is out of range", numVal.String(), fullName)
		}
	}

	// 选项验证
	if err := validateOptions(numVal.String(), opts.allowedOptions(), fullName); err != nil {
		return err
	}

	target := reflect.New(derefedType).Elem()
	if err := setStringValue(typeKind, target, numVal.String(), fullName); err != nil {
		return err
	}

	setValue(fieldType, value, target)
	return nil
}

// setConvertedValue 尝试将值转换为目标类型。
func (u *Unmarshaler) setConvertedValue(fieldType reflect.Type, value reflect.Value, mapValue any, opts *fieldOptions, fullName string) error {
	derefedType := Deref(fieldType)
	typeKind := derefedType.Kind()

	strVal, err := cast.ToStringE(mapValue)
	if err != nil {
		return fmt.Errorf("field %q type mismatch, expected %s, got %T", fullName, typeKind, mapValue)
	}

	if err := validateOptions(strVal, opts.allowedOptions(), fullName); err != nil {
		return err
	}

	target := reflect.New(derefedType).Elem()
	if err := setStringValue(typeKind, target, strVal, fullName); err != nil {
		return err
	}

	setValue(fieldType, value, target)
	return nil
}

// setStringValue 将字符串值设置到目标 reflect.Value 上（带验证）。
func setStringValue(kind reflect.Kind, value reflect.Value, str string, fullName string) error {
	if !value.CanSet() {
		return errValueNotSettable
	}
	value = ensureValue(value)
	v, err := convertTypeFromString(kind, str)
	if err != nil {
		return fmt.Errorf("failed to convert value %q of field %q to %s: %w", str, fullName, kind, err)
	}
	return setMatchedPrimitiveValue(kind, value, v)
}

// setStructValue 设置结构体类型字段的值。
func (u *Unmarshaler) setStructValue(fieldType reflect.Type, value reflect.Value, mapValue any, opts *fieldOptions, fullName string) error {
	nestedMap, ok := mapValue.(map[string]any)
	if !ok {
		return fmt.Errorf("field %q expects map type, but got %T", fullName, mapValue)
	}

	derefedType := Deref(fieldType)
	maybeNewValue(fieldType, value)
	indirectValue := reflect.Indirect(value)

	return u.processStruct(derefedType, indirectValue, nestedMap, fullName)
}

// setSliceValue 设置切片类型字段的值。
func (u *Unmarshaler) setSliceValue(fieldType reflect.Type, value reflect.Value, mapValue any, opts *fieldOptions, fullName string) error {
	if !value.CanSet() {
		return errValueNotSettable
	}

	refValue := reflect.ValueOf(mapValue)
	if refValue.Kind() != reflect.Slice {
		return fmt.Errorf("field %q expects slice type, but got %s", fullName, refValue.Kind())
	}
	if refValue.IsNil() {
		return nil
	}

	baseType := Deref(fieldType).Elem()
	dereffedBaseType := Deref(baseType)
	dereffedBaseKind := dereffedBaseType.Kind()
	if refValue.Len() == 0 {
		SetValue(fieldType, value, reflect.MakeSlice(reflect.SliceOf(baseType), 0, 0))
		return nil
	}

	conv := reflect.MakeSlice(reflect.SliceOf(baseType), refValue.Len(), refValue.Cap())

	for i := 0; i < refValue.Len(); i++ {
		ithValue := refValue.Index(i).Interface()
		if ithValue == nil {
			continue
		}

		sliceFullName := fmt.Sprintf("%s[%d]", fullName, i)
		switch dereffedBaseKind {
		case reflect.Struct:
			if dereffedBaseType == durationType {
				if err := u.setDurationValue(baseType, conv.Index(i), ithValue, sliceFullName); err != nil {
					return err
				}
			} else {
				if err := u.fillStructElement(baseType, conv.Index(i), ithValue, sliceFullName); err != nil {
					return err
				}
			}
		case reflect.Slice:
			if err := u.setSliceValue(dereffedBaseType, conv.Index(i), ithValue, opts, sliceFullName); err != nil {
				return err
			}
		default:
			if err := u.fillSliceValue(conv, i, dereffedBaseKind, ithValue, sliceFullName); err != nil {
				return err
			}
		}
	}

	SetValue(fieldType, value, conv)
	return nil
}

// fillStructElement 填充结构体元素。
func (u *Unmarshaler) fillStructElement(baseType reflect.Type, target reflect.Value, value any, fullName string) error {
	nestedMap, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("field %q expects map type, but got %T", fullName, value)
	}

	ptr := reflect.New(Deref(baseType))
	if err := u.processStruct(Deref(baseType), ptr.Elem(), nestedMap, fullName); err != nil {
		return err
	}

	SetValue(baseType, target, ptr.Elem())
	return nil
}

// fillSliceValue 填充切片中的基本类型值。
func (u *Unmarshaler) fillSliceValue(slice reflect.Value, index int, baseKind reflect.Kind, value any, fullName string) error {
	if value == nil {
		return fmt.Errorf("slice element of field %q is nil", fullName)
	}

	ithVal := slice.Index(index)
	ithValType := ithVal.Type()

	switch v := value.(type) {
	case string:
		return setStringValue(baseKind, ithVal, v, fullName)
	case json.Number:
		return setStringValue(baseKind, ithVal, v.String(), fullName)
	case map[string]any:
		switch Deref(ithValType).Kind() {
		case reflect.Struct:
			return u.fillStructElement(ithValType, ithVal, v, fullName)
		case reflect.Map:
			return u.setMapValue(ithValType, ithVal, v, nil, fullName)
		default:
			return errTypeMismatch
		}
	default:
		derefedType := Deref(ithValType)
		if !reflect.TypeOf(value).AssignableTo(derefedType) {
			// 尝试字符串转换
			if strVal, err := cast.ToStringE(value); err == nil {
				return setStringValue(baseKind, ithVal, strVal, fullName)
			}
			return errTypeMismatch
		}
		ithVal.Set(reflect.ValueOf(value))
		return nil
	}
}

// setMapValue 设置 map 类型字段的值。
func (u *Unmarshaler) setMapValue(fieldType reflect.Type, value reflect.Value, mapValue any, opts *fieldOptions, fullName string) error {
	if !value.CanSet() {
		return errValueNotSettable
	}

	fieldKeyType := fieldType.Key()
	fieldElemType := fieldType.Elem()

	mapType := reflect.MapOf(fieldKeyType, fieldElemType)
	refValue := reflect.ValueOf(mapValue)
	if mapType == refValue.Type() {
		value.Set(refValue)
		return nil
	}

	if fieldKeyType != refValue.Type().Key() {
		return fmt.Errorf("map key type mismatch for field %q", fullName)
	}

	targetValue := reflect.MakeMapWithSize(mapType, refValue.Len())
	dereffedElemType := Deref(fieldElemType)
	dereffedElemKind := dereffedElemType.Kind()

	for _, key := range refValue.MapKeys() {
		keythValue := refValue.MapIndex(key)
		keythData := keythValue.Interface()
		mapFullName := fmt.Sprintf("%s[%s]", fullName, key.String())

		switch dereffedElemKind {
		case reflect.Struct:
			if dereffedElemType == durationType {
				var d time.Duration
				if err := u.setDurationValue(fieldElemType, reflect.ValueOf(&d).Elem(), keythData, mapFullName); err != nil {
					return err
				}
				targetValue.SetMapIndex(key, reflect.ValueOf(d))
			} else {
				nestedMap, ok := keythData.(map[string]any)
				if !ok {
					return fmt.Errorf("field %q expects map type", mapFullName)
				}
				target := reflect.New(dereffedElemType)
				if err := u.processStruct(dereffedElemType, target.Elem(), nestedMap, mapFullName); err != nil {
					return err
				}
				SetMapIndexValue(fieldElemType, targetValue, key, target.Elem())
			}
		case reflect.Slice:
			target := reflect.New(dereffedElemType)
			if err := u.setSliceValue(fieldElemType, target.Elem(), keythData, opts, mapFullName); err != nil {
				return err
			}
			targetValue.SetMapIndex(key, target.Elem())
		case reflect.Map:
			nestedMap, ok := keythData.(map[string]any)
			if !ok {
				return fmt.Errorf("field %q expects map type", mapFullName)
			}
			innerValue := reflect.New(fieldElemType).Elem()
			if err := u.setMapValue(fieldElemType, innerValue, nestedMap, opts, mapFullName); err != nil {
				return err
			}
			targetValue.SetMapIndex(key, innerValue)
		default:
			switch v := keythData.(type) {
			case bool:
				if dereffedElemKind != reflect.Bool {
					return errTypeMismatch
				}
				targetValue.SetMapIndex(key, reflect.ValueOf(v))
			case string:
				if dereffedElemKind != reflect.String {
					return errTypeMismatch
				}
				targetValue.SetMapIndex(key, reflect.ValueOf(v))
			case json.Number:
				target := reflect.New(dereffedElemType)
				if err := setStringValue(dereffedElemKind, target.Elem(), v.String(), mapFullName); err != nil {
					return err
				}
				SetMapIndexValue(fieldElemType, targetValue, key, target.Elem())
			default:
				if dereffedElemKind != keythValue.Kind() {
					return errTypeMismatch
				}
				targetValue.SetMapIndex(key, keythValue)
			}
		}
	}

	value.Set(targetValue)
	return nil
}

// SetMapIndexValue 设置 map 索引值，处理指针类型。
func SetMapIndexValue(tp reflect.Type, value, key, target reflect.Value) {
	value.SetMapIndex(key, convertTypeOfPtr(tp, target))
}

// setDurationValue 设置 time.Duration 类型字段的值。
// 使用 cast.ToDurationE 支持数值和字符串类型的 duration 转换。
func (u *Unmarshaler) setDurationValue(fieldType reflect.Type, value reflect.Value, mapValue any, fullName string) error {
	d, err := cast.ToDurationE(mapValue)
	if err != nil {
		return fmt.Errorf("failed to parse duration for field %q: %w", fullName, err)
	}

	SetValue(fieldType, value, reflect.ValueOf(d))
	return nil
}

// setEnvValue 从环境变量值设置字段。
func (u *Unmarshaler) setEnvValue(fieldType reflect.Type, value reflect.Value, envVal string, opts *fieldOptions, fullName string) error {
	if err := validateOptions(envVal, opts.allowedOptions(), fullName); err != nil {
		return err
	}

	derefType := Deref(fieldType)
	derefKind := derefType.Kind()

	switch {
	case derefKind == reflect.String:
		SetValue(fieldType, value, reflect.ValueOf(envVal))
		return nil
	case derefType == durationType:
		d, err := cast.ToDurationE(envVal)
		if err != nil {
			return fmt.Errorf("failed to parse env duration for field %q: %w", fullName, err)
		}
		SetValue(fieldType, value, reflect.ValueOf(d))
		return nil
	default:
		target := reflect.New(derefType).Elem()
		if err := setStringValue(derefKind, target, envVal, fullName); err != nil {
			return fmt.Errorf("failed to parse env value for field %q: %w", fullName, err)
		}
		SetValue(fieldType, value, target)
		return nil
	}
}

// setDefaultValue 设置字段的默认值。
func (u *Unmarshaler) setDefaultValue(fieldType reflect.Type, value reflect.Value, defaultValue string, fullName string) error {
	derefedType := Deref(fieldType)

	if derefedType == durationType {
		d, err := cast.ToDurationE(defaultValue)
		if err != nil {
			return fmt.Errorf("failed to parse default duration for field %q: %w", fullName, err)
		}
		SetValue(fieldType, value, reflect.ValueOf(d))
		return nil
	}

	fieldKind := derefedType.Kind()
	switch fieldKind {
	case reflect.Slice:
		return u.fillSliceWithDefault(derefedType, value, defaultValue, fullName)
	default:
		target := reflect.New(derefedType).Elem()
		if err := setStringValue(fieldKind, target, defaultValue, fullName); err != nil {
			return err
		}
		setValue(fieldType, value, target)
		return nil
	}
}

// fillSliceWithDefault 填充切片的默认值。
func (u *Unmarshaler) fillSliceWithDefault(derefedType reflect.Type, value reflect.Value, defaultValue string, fullName string) error {
	var slice []any
	// 尝试作为 JSON 数组解析
	if err := json.Unmarshal([]byte(defaultValue), &slice); err != nil {
		// 如果不是 JSON 数组，尝试作为分隔字符串
		strVal := defaultValue
		strVal = trimBrackets(strVal)
		if len(strVal) == 0 {
			return nil
		}
		parts := parseSegments(strVal)
		slice = make([]any, len(parts))
		for i, p := range parts {
			slice[i] = p
		}
	}

	return u.setSliceValue(derefedType, value, slice, nil, fullName)
}

// trimBrackets 去除字符串两端的方括号。
func trimBrackets(val string) string {
	val = trimLeftBrackets(val)
	val = trimRightBrackets(val)
	return val
}

func trimLeftBrackets(val string) string {
	for len(val) > 0 && (val[0] == '[' || val[0] == '(') {
		val = val[1:]
	}
	return val
}

func trimRightBrackets(val string) string {
	for len(val) > 0 {
		last := val[len(val)-1]
		if last == ']' || last == ')' {
			val = val[:len(val)-1]
		} else {
			break
		}
	}
	return val
}

// hasAnySubField 检查嵌入结构体的子字段是否在配置 map 中存在。
func (u *Unmarshaler) hasAnySubField(structType reflect.Type, m map[string]any) bool {
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if !field.IsExported() {
			continue
		}
		key, _, err := parseKeyAndOptions(u.key, field)
		if err != nil {
			continue
		}
		if key == ignoreKey {
			continue
		}
		if _, ok := lookupKey(m, key); ok {
			return true
		}
	}
	return false
}

// setValue 内部设置值。
func setValue(fieldType reflect.Type, value, target reflect.Value) {
	SetValue(fieldType, value, target)
}

// UnmarshalKey 使用默认的 json 标签将 m 反序列化到 v 中。
func UnmarshalKey(m map[string]any, v any) error {
	return NewUnmarshaler(jsonTagKey).Unmarshal(m, v)
}

// UnmarshalJsonMap 使用默认的 json 标签将 m 反序列化到 v 中。
func UnmarshalJsonMap(m map[string]any, v any, opts ...UnmarshalOption) error {
	u := NewUnmarshaler(jsonTagKey, opts...)
	return u.Unmarshal(m, v)
}

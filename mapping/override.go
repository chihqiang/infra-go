package mapping

import (
	"fmt"
	"reflect"
)

// OverridePolicy 定义字段覆盖策略。
type OverridePolicy int

const (
	// OverrideNonZero 零值视为"未设置"，不覆盖默认值（当前各模块的默认行为）。
	// 对于需要显式设零值的字段，请在 overrides 中使用指针类型（如 *int、*time.Duration），
	// 指针非 nil 即视为"已设置"，指针指向零值也会覆盖。
	OverrideNonZero OverridePolicy = iota
)

// overrideField 标记哪些字段始终使用 overrides 的值（即使为零值）。
// 各模块在 fillDefault 中对 Password、DSN 等字段做了"空字符串也是有效值"的处理，
// 这些字段在此集合中声明，实现统一的"始终覆盖"语义。
//
// 通过 analyzeAlwaysOverrideFields 在运行时分析 overrides 结构体的标签，
// 字段标签中含 optional 且无 default 的 string/slice 类型视为"始终使用用户值"。
// 这是启发式策略，覆盖了各模块当前的手写逻辑。

// FillAndOverride 先用标签默认值填充 defaults，再用 overrides 中的非零值覆盖。
//
// defaults 必须指向零值结构体（标签中的 default 才会生效）。
// overrides 中的非零字段会覆盖到 defaults；零值字段保留默认值。
//
// 对于 string 和 slice 类型，空值（""/"nil）也视为"未设置"，不覆盖默认值。
// 如果需要将 string 显式设为空，请在 overrides 中使用 *string 指针类型。
//
// 此函数是各模块 fillDefault 的统一替代方案，消除重复的逐字段覆盖代码。
//
// 用法：
//
//	var c Config
//	mapping.FillAndOverride(&c, cfg) // c 先填充默认值，再用 cfg 的非零字段覆盖
func FillAndOverride(defaults any, overrides any) error {
	// 1. 填充默认值
	if err := FillDefault(defaults); err != nil {
		return err
	}

	// 2. 用 overrides 的非零字段覆盖
	return overrideNonZeroFields(defaults, overrides)
}

// MustFillAndOverride 同 FillAndOverride，出错时 panic。
func MustFillAndOverride(defaults any, overrides any) {
	if err := FillAndOverride(defaults, overrides); err != nil {
		panic(err)
	}
}

// overrideNonZeroFields 遍历 overrides 结构体的所有字段，
// 将非零字段覆盖到 target（两者须为同一类型）。
func overrideNonZeroFields(target any, overrides any) error {
	targetVal := reflect.ValueOf(target)
	if err := ValidatePtr(targetVal); err != nil {
		return err
	}

	targetVal = targetVal.Elem()
	overrideVal := reflect.ValueOf(overrides)
	if overrideVal.Kind() == reflect.Ptr {
		overrideVal = overrideVal.Elem()
	}

	targetType := targetVal.Type()
	if targetVal.Type() != overrideVal.Type() {
		return fmt.Errorf("override: type mismatch, target=%s, overrides=%s",
			targetType, overrideVal.Type())
	}

	return overrideStructFields(targetVal, overrideVal, targetType)
}

// overrideStructFields 递归遍历结构体字段，用 overrides 的非零值覆盖 target。
func overrideStructFields(target, overrides reflect.Value, structType reflect.Type) error {
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if !field.IsExported() {
			continue
		}

		targetField := target.Field(i)
		overrideField := overrides.Field(i)

		// 处理匿名嵌入字段
		if field.Anonymous {
			derefedType := Deref(field.Type)
			if derefedType.Kind() == reflect.Struct {
				if err := overrideStructFields(targetField, overrideField, derefedType); err != nil {
					return err
				}
			}
			continue
		}

		// 解析标签选项，判断是否为"始终覆盖"字段
		_, opts, err := parseKeyAndOptions(jsonTagKey, field)
		if err != nil {
			continue
		}

		// 对非指针的嵌套结构体，递归覆盖子字段，保留默认值
		derefedType := Deref(field.Type)
		if field.Type.Kind() != reflect.Ptr && derefedType.Kind() == reflect.Struct {
			if !overrideField.IsZero() {
				if err := overrideStructFields(targetField, overrideField, derefedType); err != nil {
					return err
				}
			}
			continue
		}

		if shouldOverride(targetField, overrideField, opts) {
			targetField.Set(overrideField)
		}
	}
	return nil
}

// shouldOverride 判断是否应该用 override 值覆盖 target 值。
//
// 覆盖规则：
//   - 指针类型：非 nil 即覆盖（支持 *int=0、*string="" 等显式零值）
//   - 布尔类型：true 覆盖（false 视为未设置，需用 *bool 显式设 false）
//   - string 类型：非空覆盖（空字符串视为未设置；标签标为 optional 且无 default 的 string 始终覆盖）
//   - slice/map 类型：非 nil 且非空覆盖
//   - 其他类型：非零值覆盖
func shouldOverride(target, override reflect.Value, opts *fieldOptions) bool {
	if !override.CanInterface() {
		return false
	}

	// 指针类型：非 nil 即覆盖
	if override.Kind() == reflect.Ptr {
		return !override.IsNil()
	}

	switch override.Kind() {
	case reflect.Bool:
		// 布尔类型：true 覆盖
		return override.Bool()

	case reflect.String:
		// string 类型：非空覆盖
		// 标签标为 optional 且无 default 的 string 视为"始终使用用户值"
		if opts != nil && opts.isOptional() {
			if _, hasDefault := opts.hasDefault(); !hasDefault {
				return true
			}
		}
		return override.String() != ""

	case reflect.Slice, reflect.Map:
		// slice/map：非 nil 且非空覆盖
		if override.IsNil() {
			return false
		}
		return override.Len() > 0

	default:
		// 其他类型：非零值覆盖
		return !override.IsZero()
	}
}

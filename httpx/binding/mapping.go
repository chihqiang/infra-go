package binding

import (
	"reflect"
	"strings"
)

// --- Form 映射核心逻辑 ---

// mapURI 将 URI 路径参数映射到结构体。
// 使用 `uri` 标签匹配字段名。
func mapURI(ptr any, m map[string][]string) error {
	return mapFormByTag(ptr, m, "uri")
}

// mapForm 将表单数据映射到结构体。
// 使用 `form` 标签匹配字段名。
func mapForm(ptr any, form map[string][]string) error {
	return mapFormByTag(ptr, form, "form")
}

// mapHeader 将 HTTP header 映射到结构体。
// 使用 `header` 标签匹配字段名。
func mapHeader(ptr any, h map[string][]string) error {
	return mappingByPtr(ptr, headerSource(h), "header")
}

// mapFormByTag 按指定标签将 map[string][]string 映射到结构体。
func mapFormByTag(ptr any, form map[string][]string, tag string) error {
	ptrVal := reflect.ValueOf(ptr)
	var pointed any
	if ptrVal.Kind() == reflect.Ptr {
		ptrVal = ptrVal.Elem()
		pointed = ptrVal.Interface()
	}
	// 如果目标本身是 map[string]string 或 map[string][]string，直接填充
	if ptrVal.Kind() == reflect.Map && ptrVal.Type().Key().Kind() == reflect.String {
		if pointed != nil {
			ptr = pointed
		}
		return setFormMap(ptr, form)
	}

	return mappingByPtr(ptr, formSource(form), tag)
}

// setter 尝试为结构体字段设置值的接口。
type setter interface {
	// TrySet 尝试设置值，返回是否已设置及错误。
	TrySet(value reflect.Value, fm *fieldMeta, key string, opt setOptions) (bool, error)
}

// formSource 表单数据源。
type formSource map[string][]string

var _ setter = formSource(nil)

// TrySet 从表单数据源设置值。
func (form formSource) TrySet(value reflect.Value, fm *fieldMeta, key string, opt setOptions) (bool, error) {
	return setByForm(value, fm, form, key, opt)
}

// setOptions 字段设置选项。
type setOptions struct {
	isDefaultExists bool   // 是否有默认值
	defaultValue    string // 默认值
}

// mappingByPtr 通过反射遍历指针指向的结构体，逐字段设置值。
func mappingByPtr(ptr any, s setter, tag string) error {
	_, err := mapping(reflect.ValueOf(ptr), nil, s, tag)
	return err
}

// mapping 递归遍历结构体字段进行值映射。
// 字段元信息（tag、类型预判）从缓存获取，避免每次绑定的重复反射解析。
// fm 为 nil 时表示根调用（整个目标结构体），直接展开其字段。
func mapping(value reflect.Value, fm *fieldMeta, s setter, tag string) (bool, error) {
	// 跳过显式忽略的字段
	if fm != nil && fm.skip {
		return false, nil
	}

	vKind := value.Kind()

	// 处理指针类型
	if vKind == reflect.Ptr {
		var isNew bool
		vPtr := value
		if value.IsNil() {
			isNew = true
			vPtr = reflect.New(value.Type().Elem())
		}
		isSet, err := mapping(vPtr.Elem(), fm, s, tag)
		if err != nil {
			return false, err
		}
		if isNew && isSet {
			value.Set(vPtr)
		}
		return isSet, nil
	}

	// 非匿名结构体（或根调用），尝试直接设置值
	if fm == nil || vKind != reflect.Struct || !fm.anonymous {
		ok, err := tryToSetValue(value, fm, s, tag)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}

	// 递归处理结构体字段
	if vKind == reflect.Struct {
		var isSet bool
		for _, f := range getStructMeta(value.Type(), tag).fields {
			ok, err := mapping(value.Field(f.index), &f, s, tag)
			if err != nil {
				return false, err
			}
			isSet = isSet || ok
		}
		return isSet, nil
	}
	return false, nil
}

// tryToSetValue 尝试从数据源设置单个字段的值。
func tryToSetValue(value reflect.Value, fm *fieldMeta, s setter, tag string) (bool, error) {
	if fm == nil {
		return false, nil
	}

	// 标签为空时使用字段名
	tagValue := fm.tagKey
	if tagValue == "" {
		tagValue = fm.name
	}
	if tagValue == "" {
		return false, nil
	}

	return s.TrySet(value, fm, tagValue, setOptions{
		isDefaultExists: fm.hasDefault,
		defaultValue:    fm.defaultValue,
	})
}

// setByForm 从 map[string][]string 数据源设置字段值。
func setByForm(value reflect.Value, fm *fieldMeta, form map[string][]string, tagValue string, opt setOptions) (bool, error) {
	vs, ok := form[tagValue]
	if !ok && !opt.isDefaultExists {
		return false, nil
	}

	switch value.Kind() {
	case reflect.Slice:
		if len(vs) == 0 {
			if !opt.isDefaultExists {
				return false, nil
			}
			vs = strings.Split(opt.defaultValue, ",")
		} else if len(vs) == 1 && strings.Contains(vs[0], ",") {
			// 单值含逗号时，自动按逗号分割（适用于 query 参数 tags=a,b,c 的场景）
			vs = strings.Split(vs[0], ",")
		}
		return true, setSlice(vs, value, fm, opt)
	default:
		var val string
		if !ok || len(vs) == 0 || (len(vs) > 0 && vs[0] == "") {
			val = opt.defaultValue
		} else if len(vs) > 0 {
			val = vs[0]
		}
		return true, setWithProperType(val, value, fm, opt)
	}
}

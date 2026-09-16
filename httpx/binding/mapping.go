package binding

import (
	"reflect"
	"strings"
)

// --- Core form mapping logic ---

// mapURI maps URI path parameters onto a struct.
// Field names are matched using the `uri` tag.
func mapURI(ptr any, m map[string][]string) error {
	return mapFormByTag(ptr, m, "uri")
}

// mapForm maps form data onto a struct.
// Field names are matched using the `form` tag.
func mapForm(ptr any, form map[string][]string) error {
	return mapFormByTag(ptr, form, "form")
}

// mapHeader maps HTTP headers onto a struct.
// Field names are matched using the `header` tag.
func mapHeader(ptr any, h map[string][]string) error {
	return mappingByPtr(ptr, headerSource(h), "header")
}

// mapFormByTag maps a map[string][]string onto a struct using the given tag.
func mapFormByTag(ptr any, form map[string][]string, tag string) error {
	ptrVal := reflect.ValueOf(ptr)
	var pointed any
	if ptrVal.Kind() == reflect.Ptr {
		ptrVal = ptrVal.Elem()
		pointed = ptrVal.Interface()
	}
	// If the target itself is a map[string]string or map[string][]string, fill it directly
	if ptrVal.Kind() == reflect.Map && ptrVal.Type().Key().Kind() == reflect.String {
		if pointed != nil {
			ptr = pointed
		}
		return setFormMap(ptr, form)
	}

	return mappingByPtr(ptr, formSource(form), tag)
}

// setter is the interface that tries to set a value on a struct field.
type setter interface {
	// TrySet tries to set the value and reports whether it was set, along with any error.
	TrySet(value reflect.Value, fm *fieldMeta, key string, opt setOptions) (bool, error)
}

// formSource is a form data source.
type formSource map[string][]string

var _ setter = formSource(nil)

// TrySet sets the value from the form data source.
func (form formSource) TrySet(value reflect.Value, fm *fieldMeta, key string, opt setOptions) (bool, error) {
	return setByForm(value, fm, form, key, opt)
}

// setOptions holds field set options.
type setOptions struct {
	isDefaultExists bool   // whether a default value exists
	defaultValue    string // default value
}

// mappingByPtr walks the struct pointed to by ptr via reflection and sets values field by field.
func mappingByPtr(ptr any, s setter, tag string) error {
	_, err := mapping(reflect.ValueOf(ptr), nil, s, tag)
	return err
}

// mapping recursively walks struct fields to map values.
// Field metadata (tag, type pre-detection) is read from the cache to avoid
// repeated reflection parsing on every bind.
// A nil fm means the root call (the whole target struct), whose fields are expanded directly.
func mapping(value reflect.Value, fm *fieldMeta, s setter, tag string) (bool, error) {
	// Skip explicitly ignored fields
	if fm != nil && fm.skip {
		return false, nil
	}

	vKind := value.Kind()

	// Handle pointer types
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

	// For non-anonymous structs (or the root call), try to set the value directly
	if fm == nil || vKind != reflect.Struct || !fm.anonymous {
		ok, err := tryToSetValue(value, fm, s, tag)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}

	// Recurse into struct fields
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

// tryToSetValue tries to set the value of a single field from the data source.
func tryToSetValue(value reflect.Value, fm *fieldMeta, s setter, tag string) (bool, error) {
	if fm == nil {
		return false, nil
	}

	// Fall back to the field name when the tag is empty
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

// setByForm sets a field value from a map[string][]string data source.
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
			// A single value containing commas is split automatically
			// (useful for query parameters such as tags=a,b,c)
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

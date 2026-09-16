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
	// ignoreKey means the field is ignored.
	ignoreKey = "-"
	// delimiter joins the full path name of nested fields.
	delimiter = '.'
)

var (
	errValueNotSettable = fmt.Errorf("value is not settable, must pass a struct pointer")
	errValueNotStruct   = fmt.Errorf("value type is not struct")
	errTypeMismatch     = fmt.Errorf("type mismatch")
	errUnsupportedType  = fmt.Errorf("unsupported field type")

	durationType = reflect.TypeOf(time.Duration(0))
	intSize      = 32 << (^uint(0) >> 63) // 32 or 64

	// structRequiredCache caches whether a struct contains required fields.
	structRequiredCache = make(map[reflect.Type]bool)
	structCacheLock     sync.RWMutex
)

// Deref dereferences pointer types and returns the underlying type.
func Deref(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

// ValidatePtr verifies that v is a valid, non-nil pointer.
func ValidatePtr(v reflect.Value) error {
	if !v.IsValid() || v.Kind() != reflect.Ptr || v.IsNil() {
		return fmt.Errorf("not a valid pointer: %v", v)
	}
	return nil
}

// SetValue sets the target value, handling pointer types automatically.
func SetValue(tp reflect.Type, value, target reflect.Value) {
	value.Set(convertTypeOfPtr(tp, target))
}

// convertTypeOfPtr handles the conversion of pointer types.
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

// maybeNewValue allocates a new value when the type is a pointer and it is nil.
func maybeNewValue(fieldType reflect.Type, value reflect.Value) {
	if fieldType.Kind() == reflect.Ptr && value.IsNil() {
		value.Set(reflect.New(value.Type().Elem()))
	}
}

// ensureValue makes sure nested members are not nil.
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

// joinName joins field path names.
func joinName(parent, child string) string {
	if len(parent) == 0 {
		return child
	}
	if len(child) == 0 {
		return parent
	}
	return parent + string(delimiter) + child
}

// usingDifferentKeys reports whether the field uses a tag key different from the
// current parser's.
func usingDifferentKeys(key string, field reflect.StructField) bool {
	if len(field.Tag) > 0 {
		if _, ok := field.Tag.Lookup(key); !ok {
			return true
		}
	}
	return false
}

// lookupKey looks up a key in the map (dot-separated nested keys are supported).
func lookupKey(m map[string]any, key string) (any, bool) {
	if m == nil {
		return nil, false
	}

	keys := readKeys(key)
	return lookupWithChainedKeys(m, keys)
}

// readKeys splits the key on dots into segments.
func readKeys(key string) []string {
	return strings.FieldsFunc(key, func(c rune) bool {
		return c == delimiter
	})
}

// lookupWithChainedKeys looks up a value following chained keys.
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

// errAmbiguousKey means that several candidate keys at the same level share the
// same canonical form.
var errAmbiguousKey = errors.New("ambiguous key")

// lookupKeyCanonical looks up a value by chained key, falling back to canonical,
// case-insensitive matching when there is no exact hit.
//
// The case-insensitive matching happens here rather than by the caller pre-rewriting
// the input map keys: map keys are also the data of map-typed fields, and
// lower-casing them in advance would corrupt user data
// (e.g. labels: {AppName: x} would silently become appname).
//
// Matching rules per level:
//  1. the raw key segment matches exactly;
//  2. the canonical form of the key segment matches exactly;
//  3. a candidate key satisfying canonical(k) == canonical(segment) is looked up
//     and returned when it is unique;
//  4. several candidates (differing only in case/form) yield errAmbiguousKey,
//     rather than picking one depending on map iteration order.
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
		// 1. Raw key exact match (the common case where canonicalKey preserves the
		// segment's meaning)
		v, ok := current[segment]
		if !ok {
			want := canonical(segment)
			// 2. Canonical form exact match
			v, ok = current[want]
			if !ok {
				// 3. Case-insensitive scan by canonical form
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

// describeKeys returns the sorted key list, used to produce stable error messages.
func describeKeys(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// convertTypeFromString converts a string into a basic value of the given type.
// Bool uses cast.ToBoolE and floats use cast.ToFloat64E; integers keep strconv so
// that bit-width overflow checks are preserved.
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

// setMatchedPrimitiveValue sets an already-converted value onto a reflect.Value.
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

// setSameKindValue sets a value of the same kind, converting it when necessary.
func setSameKindValue(targetType reflect.Type, target reflect.Value, value any) {
	if reflect.ValueOf(value).Type().AssignableTo(targetType) {
		target.Set(reflect.ValueOf(value))
	} else {
		target.Set(reflect.ValueOf(value).Convert(targetType))
	}
}

// validateOptions verifies that the value is in the allowed options list.
// Values are converted to strings with cast.ToString for the comparison.
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

// validateValueRange verifies that the number lies within the range.
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

// validateRangeForType validates the value against the range for the target type.
//
// Unlike validateValueRange it first normalizes the value by target type: a
// time.Duration range is compared in nanoseconds (matching its underlying int64
// representation), so a duration text such as "5s" is parsed into a
// time.Duration before the comparison.
//
// Every path that writes a field must call this function (or validateValueRange),
// otherwise the range constraint is silently bypassed when the value comes from a
// different source or is written in a different representation.
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

// structValueRequired reports whether the struct type contains required fields.
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

// implicitValueRequiredStruct recursively checks whether the struct contains
// required fields.
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

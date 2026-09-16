package mapping

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/chihqiang/infra-go/cast"
)

// jsonTagKey is the json tag key name.
const jsonTagKey = "json"

var emptyMap = map[string]any{}

// Unmarshaler is the configuration deserializer; it maps a map[string]any onto a
// struct and handles extended features such as defaults, environment variables,
// option validation and range validation.
type Unmarshaler struct {
	key          string              // struct tag key name, usually "json"
	fillDefault  bool                // whether to only fill defaults
	fromString   bool                // whether to parse every value from a string
	canonicalKey func(string) string // key canonicalization function (e.g. lower-casing)
}

// UnmarshalOption defines a configuration option for Unmarshaler.
type UnmarshalOption func(*Unmarshaler)

// WithDefault enables the fill-defaults-only mode.
func WithDefault() UnmarshalOption {
	return func(u *Unmarshaler) {
		u.fillDefault = true
	}
}

// WithStringValues enables parsing every value from a string.
func WithStringValues() UnmarshalOption {
	return func(u *Unmarshaler) {
		u.fromString = true
	}
}

// WithCanonicalKeyFunc sets the key canonicalization function, used for
// case-insensitive matching. With strings.ToLower, for example, "logMode" in the
// config file can match the field name "LogMode".
func WithCanonicalKeyFunc(f func(string) string) UnmarshalOption {
	return func(u *Unmarshaler) {
		u.canonicalKey = f
	}
}

// NewDefaultUnmarshaler creates a deserializer that fills defaults.
// It is equivalent to NewUnmarshaler("json", WithDefault())
// and is the recommended entry point for the fillDefault pattern in modules such
// as conf, redisx and orm.
func NewDefaultUnmarshaler() *Unmarshaler {
	return NewUnmarshaler("json", WithDefault())
}

// defaultUnmarshaler is the package-level default deserializer used by
// FillDefault.
// Unmarshaler methods only read their own fields and never mutate state, so this
// instance is safe for concurrent reuse.
var defaultUnmarshaler = NewDefaultUnmarshaler()

// FillDefault fills defaults and environment variables into the given struct.
// The struct's fields must all be zero values beforehand.
// It is equivalent to NewDefaultUnmarshaler().Unmarshal(map[string]any{}, v)
// and is the unified entry point for the fillDefault pattern in modules such as
// conf, redisx, orm, jwt, httpx and logger.
func FillDefault(v any) error {
	return defaultUnmarshaler.Unmarshal(emptyMap, v)
}

// NewUnmarshaler creates a new deserializer.
func NewUnmarshaler(key string, opts ...UnmarshalOption) *Unmarshaler {
	u := &Unmarshaler{key: key}
	for _, opt := range opts {
		opt(u)
	}
	return u
}

// Unmarshal deserializes the map data into the target struct v.
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

// fillDefaultStruct fills only defaults and environment variables.
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

		// Check whether the field already holds a non-zero value
		if !fieldValue.IsZero() {
			return fmt.Errorf("field %q must be zero value when filling default", fn)
		}

		// Prefer the environment variable
		if opts != nil && opts.EnvVar != "" {
			if envVal := os.Getenv(opts.EnvVar); envVal != "" {
				if err := u.setEnvValue(field.Type, fieldValue, envVal, opts, fn); err != nil {
					return err
				}
				continue
			}
		}

		// Fill the default value
		if defaultValue, ok := opts.hasDefault(); ok {
			if err := u.setDefaultValue(field.Type, fieldValue, defaultValue, opts, fn); err != nil {
				return err
			}
			continue
		}

		// For a non-pointer nested struct, fill recursively
		derefedType := Deref(field.Type)
		if field.Type.Kind() != reflect.Ptr && derefedType.Kind() == reflect.Struct {
			if err := u.fillDefaultStruct(derefedType, fieldValue, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// processStruct processes all fields of the struct.
func (u *Unmarshaler) processStruct(structType reflect.Type, structValue reflect.Value, m map[string]any, fullName string) error {
	// Validate the optional dependency names first, so that a misspelled
	// dependency does not silently break conditional optionality
	if err := u.validateOptionalDeps(structType, fullName); err != nil {
		return err
	}

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

// processAnonymousField handles anonymous (embedded) fields.
func (u *Unmarshaler) processAnonymousField(field reflect.StructField, value reflect.Value, m map[string]any, fullName string) error {
	key, opts, err := parseKeyAndOptions(u.key, field)
	if err != nil {
		return err
	}
	if key == ignoreKey {
		return nil
	}

	derefedType := Deref(field.Type)

	// If the embedded type is not a struct, treat it as a regular field
	if derefedType.Kind() != reflect.Struct {
		return u.processNamedField(field, value, m, fullName)
	}

	maybeNewValue(field.Type, value)
	indirectValue := reflect.Indirect(value)

	// Handle an optional embedded struct
	if opts != nil && opts.isOptional() {
		hasValue := u.hasAnySubField(derefedType, m)
		if !hasValue {
			return nil
		}
	}

	// Recursively process the fields of the embedded struct
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

// processNamedField handles a named field.
func (u *Unmarshaler) processNamedField(field reflect.StructField, value reflect.Value, m map[string]any, fullName string) error {
	if !field.IsExported() {
		return nil
	}
	return u.processField(field, value, m, fullName)
}

// processField handles a single field (the unified entry point).
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

	// Prefer the environment variable
	if opts != nil && opts.EnvVar != "" {
		if envVal := os.Getenv(opts.EnvVar); envVal != "" {
			return u.setEnvValue(field.Type, value, envVal, opts, fn)
		}
	}

	// Look the value up in the config map.
	// When canonicalKey is set, matching is case-insensitive rather than requiring
	// the caller to pre-rewrite the map keys (a map key may also be the data of a
	// map-typed field, and rewriting it would corrupt user data).
	var (
		mapValue any
		hasValue bool
	)
	if u.canonicalKey != nil {
		mapValue, hasValue, err = lookupKeyCanonical(m, key, u.canonicalKey)
		if err != nil {
			return fmt.Errorf("field %q: %w", fn, err)
		}
	} else {
		mapValue, hasValue = lookupKey(m, key)
	}

	if !hasValue {
		// Conditional optionality: optional=Dep / optional=!Dep.
		// When the dependency is unmet the field is treated as required, when it
		// is met as optional.
		//
		// The dependency check is skipped when a default exists: the field always
		// gets a value, so the dependency is irrelevant (otherwise `default` +
		// `optional=dep` would wrongly report the field as required whenever the
		// dependency is unmet).
		if opts != nil && opts.OptionalDep != "" {
			if _, hasDefault := opts.hasDefault(); !hasDefault {
				depMet, err := u.optionalDepMet(opts, m)
				if err != nil {
					return fmt.Errorf("field %q: %w", fn, err)
				}
				if !depMet {
					return fmt.Errorf("field %q not set: required because %q is %s",
						fn, opts.OptionalDep, optionalDepStateDesc(opts.OptionalDepNegate))
				}
			}
		}
		return u.processFieldWithoutValue(field.Type, value, opts, fn)
	}

	return u.processFieldWithValue(field.Type, value, mapValue, opts, fn)
}

// optionalDepMet reports whether the conditional-optional dependency is met.
//
// Semantics (matching the documentation):
//   - `optional=Other`  : this field is optional when Other **is set**
//   - `optional=!Other` : this field is optional when Other **is not set**
//
// Dependency names are resolved as **configuration keys** (that is, the
// json/yaml tag key of the dependency field, such as `other`), following the
// same lookup path as field values; whether the dependency points at a real
// field is validated in advance by processStruct.
func (u *Unmarshaler) optionalDepMet(opts *fieldOptions, m map[string]any) (bool, error) {
	var present bool
	var err error
	if u.canonicalKey != nil {
		_, present, err = lookupKeyCanonical(m, opts.OptionalDep, u.canonicalKey)
		if err != nil {
			return false, err
		}
	} else {
		_, present = lookupKey(m, opts.OptionalDep)
	}

	// Dep: dependency present -> optional; !Dep: dependency absent -> optional
	if opts.OptionalDepNegate {
		return !present, nil
	}
	return present, nil
}

// buildDependencyKeys collects the config keys of all struct fields; it is used
// to validate that optional dependencies are valid.
func (u *Unmarshaler) buildDependencyKeys(structType reflect.Type) map[string]struct{} {
	keys := make(map[string]struct{}, structType.NumField())
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if !field.IsExported() {
			continue
		}
		if field.Anonymous {
			// Sub-fields of an anonymous embedded field share the same namespace
			for k := range u.buildDependencyKeys(Deref(field.Type)) {
				keys[k] = struct{}{}
			}
			continue
		}
		key, _, err := parseKeyAndOptions(u.key, field)
		if err != nil || key == ignoreKey {
			continue
		}
		keys[key] = struct{}{}
	}
	return keys
}

// validateOptionalDeps verifies that every optional dependency inside the struct
// points at a configuration key that really exists.
//
// Without the check a misspelled dependency name makes the field
// **permanently required** (the dependency is never found), a silent behavioural
// deviation that is hard to track down.
func (u *Unmarshaler) validateOptionalDeps(structType reflect.Type, fullName string) error {
	var validKeys map[string]struct{}

	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if !field.IsExported() || field.Anonymous {
			continue
		}
		key, opts, err := parseKeyAndOptions(u.key, field)
		if err != nil || key == ignoreKey {
			continue
		}
		if opts == nil || opts.OptionalDep == "" {
			continue
		}

		if validKeys == nil {
			validKeys = u.buildDependencyKeys(structType)
		}
		if _, ok := validKeys[opts.OptionalDep]; !ok {
			return fmt.Errorf(
				"field %q: optional dependency %q does not match any field key in %s",
				joinName(fullName, key), opts.OptionalDep, structType.Name())
		}
	}
	return nil
}

// optionalDepStateDesc produces the dependency state description used in error
// messages.
func optionalDepStateDesc(negate bool) string {
	if negate {
		return "set"
	}
	return "not set"
}

// processFieldWithValue handles a field when a value exists in the config.
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

	// time.Duration's underlying type is int64, so it must be handled first
	if derefedType == durationType {
		// Duration fields used to skip validation entirely, which made the range
		// constraint meaningless.
		if err := validateRangeForType(derefedType, mapValue, opts, fullName); err != nil {
			return err
		}
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

// processFieldWithoutValue handles a field when the config holds no value.
func (u *Unmarshaler) processFieldWithoutValue(fieldType reflect.Type, value reflect.Value, opts *fieldOptions, fullName string) error {
	// Prefer the default value
	if defaultValue, ok := opts.hasDefault(); ok {
		return u.setDefaultValue(fieldType, value, defaultValue, opts, fullName)
	}

	derefedType := Deref(fieldType)

	// time.Duration's underlying type is int64, so it must be handled first
	if derefedType == durationType {
		if opts.isOptional() {
			return nil
		}
		return fmt.Errorf("field %q not set", fullName)
	}

	typeKind := derefedType.Kind()

	switch typeKind {
	case reflect.Struct:
		// For structs, check whether any field is required
		if !opts.isOptional() {
			required := structValueRequired(u.key, derefedType)
			if required {
				return fmt.Errorf("field %q not set", fullName)
			}
			// The struct has no required fields, so recurse with an empty map
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

// setBasicValue sets the value of a basic-typed field.
func (u *Unmarshaler) setBasicValue(fieldType reflect.Type, value reflect.Value, mapValue any, opts *fieldOptions, fullName string) error {
	derefedType := Deref(fieldType)
	typeKind := derefedType.Kind()

	// In fromString mode, convert the value to a string and parse that
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

	// Handle the json.Number type
	if numVal, ok := mapValue.(json.Number); ok {
		return u.setNumberValue(fieldType, value, numVal, opts, fullName)
	}

	// Handle native types
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

	// Try converting the value to a string and parsing that
	return u.setConvertedValue(fieldType, value, mapValue, opts, fullName)
}

// setNumberValue sets a numeric field's value (from a json.Number).
func (u *Unmarshaler) setNumberValue(fieldType reflect.Type, value reflect.Value, numVal json.Number, opts *fieldOptions, fullName string) error {
	derefedType := Deref(fieldType)
	typeKind := derefedType.Kind()

	// Range validation
	if opts != nil && opts.Range != nil {
		fv, err := numVal.Float64()
		if err != nil {
			return fmt.Errorf("value %q of field %q cannot be converted to float: %w", numVal.String(), fullName, err)
		}
		if !opts.isInRange(fv) {
			return fmt.Errorf("value %s of field %q is out of range", numVal.String(), fullName)
		}
	}

	// Option validation
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

// setConvertedValue tries to convert the value to the target type.
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
	// When the value type and the field type differ (e.g. port: "9090" in YAML for
	// an int field), range validation used to be bypassed, so the same semantic
	// value validated differently depending on its representation.
	if err := validateRangeForType(derefedType, strVal, opts, fullName); err != nil {
		return err
	}

	target := reflect.New(derefedType).Elem()
	if err := setStringValue(typeKind, target, strVal, fullName); err != nil {
		return err
	}

	setValue(fieldType, value, target)
	return nil
}

// setStringValue sets a string value onto the target reflect.Value (with validation).
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

// setStructValue sets the value of a struct-typed field.
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

// setSliceValue sets the value of a slice-typed field.
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

// fillStructElement fills a struct element.
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

// fillSliceValue fills a basic-typed value inside a slice.
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
			// Try a string conversion
			if strVal, err := cast.ToStringE(value); err == nil {
				return setStringValue(baseKind, ithVal, strVal, fullName)
			}
			return errTypeMismatch
		}
		ithVal.Set(reflect.ValueOf(value))
		return nil
	}
}

// setMapValue sets the value of a map-typed field.
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

// SetMapIndexValue sets a map index value, handling pointer types.
func SetMapIndexValue(tp reflect.Type, value, key, target reflect.Value) {
	value.SetMapIndex(key, convertTypeOfPtr(tp, target))
}

// setDurationValue sets the value of a time.Duration field.
// cast.ToDurationE supports converting both numeric and string durations.
func (u *Unmarshaler) setDurationValue(fieldType reflect.Type, value reflect.Value, mapValue any, fullName string) error {
	d, err := cast.ToDurationE(mapValue)
	if err != nil {
		return fmt.Errorf("failed to parse duration for field %q: %w", fullName, err)
	}

	SetValue(fieldType, value, reflect.ValueOf(d))
	return nil
}

// setEnvValue sets a field from an environment variable value.
// As on the config value path, environment variables must satisfy the options and
// range constraints too.
func (u *Unmarshaler) setEnvValue(fieldType reflect.Type, value reflect.Value, envVal string, opts *fieldOptions, fullName string) error {
	if err := validateOptions(envVal, opts.allowedOptions(), fullName); err != nil {
		return err
	}

	derefType := Deref(fieldType)
	derefKind := derefType.Kind()

	// Environment variables used to validate only options, not range, so an
	// out-of-range value was accepted when overriding config from the environment
	// (e.g. a port with range=[1:65535] bypassed by PORT=99999).
	if err := validateRangeForType(derefType, envVal, opts, fullName); err != nil {
		return err
	}

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

// setDefaultValue sets the field's default value.
// A default must satisfy the range constraint too: an out-of-range default
// declared in a tag is a configuration error and should surface at load time
// rather than being silently written into the config object.
func (u *Unmarshaler) setDefaultValue(fieldType reflect.Type, value reflect.Value, defaultValue string, opts *fieldOptions, fullName string) error {
	derefedType := Deref(fieldType)

	if err := validateRangeForType(derefedType, defaultValue, opts, fullName); err != nil {
		return err
	}

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

// fillSliceWithDefault fills a slice's default value.
func (u *Unmarshaler) fillSliceWithDefault(derefedType reflect.Type, value reflect.Value, defaultValue string, fullName string) error {
	var slice []any
	// Try to parse it as a JSON array
	if err := json.Unmarshal([]byte(defaultValue), &slice); err != nil {
		// If it is not a JSON array, try a delimited string
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

// trimBrackets strips square brackets from both ends of the string.
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

// hasAnySubField reports whether any sub-field of the embedded struct exists in
// the config map.
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
		if u.canonicalKey != nil {
			if _, ok, err := lookupKeyCanonical(m, key, u.canonicalKey); err == nil && ok {
				return true
			}
			continue
		}
		if _, ok := lookupKey(m, key); ok {
			return true
		}
	}
	return false
}

// setValue sets a value internally.
func setValue(fieldType reflect.Type, value, target reflect.Value) {
	SetValue(fieldType, value, target)
}

// UnmarshalKey deserializes m into v using the default json tag.
func UnmarshalKey(m map[string]any, v any) error {
	return NewUnmarshaler(jsonTagKey).Unmarshal(m, v)
}

// UnmarshalJsonMap deserializes m into v using the default json tag.
func UnmarshalJsonMap(m map[string]any, v any, opts ...UnmarshalOption) error {
	u := NewUnmarshaler(jsonTagKey, opts...)
	return u.Unmarshal(m, v)
}

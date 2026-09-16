package mapping

import (
	"fmt"
	"reflect"
)

// OverridePolicy defines the field override policy.
type OverridePolicy int

const (
	// OverrideNonZero treats a zero value as "unset" and does not override the
	// default (the current default behaviour of every module).
	// For fields that need an explicit zero value, use a pointer type in the
	// overrides (such as *int or *time.Duration): a non-nil pointer counts as
	// "set", and a pointer to a zero value overrides as well.
	OverrideNonZero OverridePolicy = iota
)

// overrideField marks which fields always take the value from overrides (even
// when it is a zero value).
// Various modules' fillDefault treat fields such as Password and DSN as "an empty
// string is also a valid value"; those fields are declared in this set to provide
// a unified "always override" semantics.
//
// analyzeAlwaysOverrideFields inspects the tags of the overrides struct at
// runtime: string/slice fields whose tag carries optional without default are
// treated as "always use the user value".
// This is a heuristic that covers the hand-written logic each module currently has.

// FillAndOverride first fills defaults from the tag defaults, then overrides them
// with the non-zero values of overrides.
//
// defaults must point at a zero-value struct (only then do the tag defaults take
// effect). Non-zero fields in overrides override the defaults; zero-value fields
// keep the default.
//
// For string and slice types, empty values (""/nil) also count as "unset" and do
// not override the default. To set a string explicitly to empty, use a *string
// pointer type in overrides.
//
// A nil overrides (including a typed nil pointer such as (*Config)(nil)) counts as
// "no override provided": only defaults are filled, with no error and no panic.
//
// This function is the unified replacement for each module's fillDefault, removing
// the duplicated field-by-field override code.
//
// Usage:
//
//	var c Config
//	mapping.FillAndOverride(&c, cfg) // c gets defaults, then cfg's non-zero fields override them
func FillAndOverride(defaults any, overrides any) error {
	// 1. Fill in the defaults
	if err := FillDefault(defaults); err != nil {
		return err
	}

	// 2. Override with the non-zero fields of overrides
	return overrideNonZeroFields(defaults, overrides)
}

// MustFillAndOverride is like FillAndOverride but panics on error.
func MustFillAndOverride(defaults any, overrides any) {
	if err := FillAndOverride(defaults, overrides); err != nil {
		panic(err)
	}
}

// overrideNonZeroFields walks all fields of the overrides struct and copies the
// non-zero ones onto target (both must have the same type).
//
// A nil overrides (including a typed nil pointer) counts as "no override" and
// returns nil immediately.
func overrideNonZeroFields(target any, overrides any) error {
	targetVal := reflect.ValueOf(target)
	if err := ValidatePtr(targetVal); err != nil {
		return err
	}

	// A nil overrides makes reflect.ValueOf return an invalid value; calling
	// Type() on it panics (reflect: zero Value has no Type).
	if overrides == nil {
		return nil
	}

	targetVal = targetVal.Elem()

	overrideVal := reflect.ValueOf(overrides)
	if overrideVal.Kind() == reflect.Ptr {
		// A typed nil pointer (such as (*Config)(nil)) counts as "no override":
		// otherwise Elem() yields an invalid value and the Type() call below
		// panics.
		if overrideVal.IsNil() {
			return nil
		}
		overrideVal = overrideVal.Elem()
	}

	targetType := targetVal.Type()
	if targetVal.Type() != overrideVal.Type() {
		return fmt.Errorf("override: type mismatch, target=%s, overrides=%s",
			targetType, overrideVal.Type())
	}

	return overrideStructFields(targetVal, overrideVal, targetType)
}

// overrideStructFields recursively walks struct fields, overriding target with
// overrides' non-zero values.
func overrideStructFields(target, overrides reflect.Value, structType reflect.Type) error {
	// Defensive check: target/overrides must be struct values. Without it, a
	// pointer value passed by mistake makes Field(i) panic
	// (reflect: call of reflect.Value.Field on ptr Value).
	if target.Kind() != reflect.Struct || overrides.Kind() != reflect.Struct {
		return fmt.Errorf("override: expects struct values, got target=%s, overrides=%s",
			target.Kind(), overrides.Kind())
	}

	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if !field.IsExported() {
			continue
		}

		targetField := target.Field(i)
		overrideField := overrides.Field(i)

		// Handle anonymous embedded fields
		if field.Anonymous {
			derefedType := Deref(field.Type)
			if derefedType.Kind() != reflect.Struct {
				continue
			}
			// An anonymous embedded pointer (struct{ *Base }) must be dereferenced
			// first: otherwise the Field(i) call below panics on a Ptr value
			// (reflect: call of reflect.Value.Field on ptr Value).
			if field.Type.Kind() == reflect.Ptr {
				if overrideField.IsNil() {
					// No override source: leave target as is (including "already nil")
					continue
				}
				if err := ensureNonNilPtr(targetField); err != nil {
					return err
				}
				if err := overrideStructFields(targetField.Elem(), overrideField.Elem(), derefedType); err != nil {
					return err
				}
				continue
			}
			if err := overrideStructFields(targetField, overrideField, derefedType); err != nil {
				return err
			}
			continue
		}

		// Parse the tag options to decide whether this is an "always override" field
		_, opts, err := parseKeyAndOptions(jsonTagKey, field)
		if err != nil {
			continue
		}

		// For a non-pointer nested struct, override the sub-fields recursively and
		// keep the defaults
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

// ensureNonNilPtr makes sure v points at an allocated value, allocating one from
// the element type when v is nil.
// It is used for the recursive override of anonymous embedded pointer fields
// (struct{ *Base }) so that defaults are preserved instead of the pointer being
// replaced wholesale.
func ensureNonNilPtr(v reflect.Value) error {
	if !v.CanSet() {
		return fmt.Errorf("override: cannot set embedded pointer field of type %s", v.Type())
	}
	if v.IsNil() {
		v.Set(reflect.New(v.Type().Elem()))
	}
	return nil
}

// shouldOverride decides whether the target value should be overridden by the
// override value.
//
// Override rules:
//   - pointer types: override when non-nil (supports explicit zero values such as
//     *int=0 or *string="")
//   - bool: override when true (false counts as unset; use *bool to set false)
//   - string: override when non-empty (an empty string counts as unset; a string
//     tagged optional without default always overrides)
//   - slice/map: override when non-nil and non-empty
//   - other types: override when non-zero
func shouldOverride(target, override reflect.Value, opts *fieldOptions) bool {
	if !override.CanInterface() {
		return false
	}

	// Pointer types: a non-nil value overrides
	if override.Kind() == reflect.Ptr {
		return !override.IsNil()
	}

	switch override.Kind() {
	case reflect.Bool:
		// Bool: true overrides
		return override.Bool()

	case reflect.String:
		// String: a non-empty value overrides
		// A string tagged optional without default always uses the user value
		if opts != nil && opts.isOptional() {
			if _, hasDefault := opts.hasDefault(); !hasDefault {
				return true
			}
		}
		return override.String() != ""

	case reflect.Slice, reflect.Map:
		// slice/map: override when non-nil and non-empty
		if override.IsNil() {
			return false
		}
		return override.Len() > 0

	default:
		// Other types: override when non-zero
		return !override.IsZero()
	}
}

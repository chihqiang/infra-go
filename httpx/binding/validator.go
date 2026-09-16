package binding

import (
	"errors"
	"reflect"
	"sync"

	"github.com/go-playground/validator/v10"
)

// --- Validator ---

// StructValidator describes a struct validator used to validate after binding.
// It can be replaced with a custom validation entry point via SetValidateFn
// (e.g. to plug in a different validation library).
type StructValidator interface {
	// ValidateStruct validates a struct, returning nil when validation passes.
	ValidateStruct(any) error
	// Engine returns the underlying validation engine.
	Engine() any
}

// DefaultValidator is the default validator, based on go-playground/validator/v10
// with the `binding` tag.
type DefaultValidator struct {
	once     sync.Once
	validate *validator.Validate
}

var _ StructValidator = (*DefaultValidator)(nil)

// ValidateStruct validates a struct.
// It supports structs, pointers to structs, and slices/arrays (validated element by element).
func (v *DefaultValidator) ValidateStruct(obj any) error {
	if obj == nil {
		return nil
	}

	value := reflect.ValueOf(obj)
	switch value.Kind() {
	case reflect.Ptr:
		if value.Elem().Kind() != reflect.Struct {
			return v.ValidateStruct(value.Elem().Interface())
		}
		return v.validateStruct(obj)
	case reflect.Struct:
		return v.validateStruct(obj)
	case reflect.Slice, reflect.Array:
		var errs validator.ValidationErrors
		for i := 0; i < value.Len(); i++ {
			if err := v.ValidateStruct(value.Index(i).Interface()); err != nil {
				var ve validator.ValidationErrors
				if errors.As(err, &ve) {
					errs = append(errs, ve...)
				} else {
					return err
				}
			}
		}
		if len(errs) > 0 {
			return errs
		}
		return nil
	default:
		return nil
	}
}

// validateStruct validates a single struct.
func (v *DefaultValidator) validateStruct(obj any) error {
	v.lazyInit()
	return v.validate.Struct(obj)
}

// Engine returns the underlying validation engine.
func (v *DefaultValidator) Engine() any {
	v.lazyInit()
	return v.validate
}

// lazyInit lazily initializes the validator.
func (v *DefaultValidator) lazyInit() {
	v.once.Do(func() {
		v.validate = validator.New()
		v.validate.SetTagName("binding")
	})
}

// defaultValidator is the default validation instance, used unless replaced via SetValidateFn.
var defaultValidator = &DefaultValidator{}

// validateFn is the current validation entry point; binders call it to validate
// after binding. The built-in defaultValidator is used unless it has been replaced.
var validateFn = func(obj any) error {
	return defaultValidator.ValidateStruct(obj)
}

// SetValidateFn replaces the global validation entry point; a nil fn restores the built-in
// default validator.
func SetValidateFn(fn func(any) error) {
	if fn == nil {
		validateFn = func(obj any) error {
			return defaultValidator.ValidateStruct(obj)
		}
		return
	}
	validateFn = fn
}

// validate validates obj using the current validation entry point.
func validate(obj any) error {
	if validateFn == nil {
		return nil
	}
	return validateFn(obj)
}

// Validate validates a struct using the current validation entry point.
func Validate(obj any) error {
	return validate(obj)
}

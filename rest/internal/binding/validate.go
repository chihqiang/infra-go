package binding

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
)

// StructValidator validates a decoded value. Implementations must tolerate any
// input type and only validate structs, pointers to structs and slices of them.
type StructValidator interface {
	ValidateStruct(dst any) error
	Engine() any
}

// Validator validates decoded values; swap it to use a custom engine.
var Validator StructValidator = &defaultValidator{}

// Validatable is implemented by destinations that validate themselves after the
// binding tags are checked.
type Validatable interface {
	Validate() error
}

func validate(dst any) error {
	if Validator != nil {
		if err := Validator.ValidateStruct(dst); err != nil {
			return badRequestErr(err)
		}
	}
	if validatable, ok := dst.(Validatable); ok {
		if err := validatable.Validate(); err != nil {
			return badRequestErr(err)
		}
	}
	return nil
}

// defaultValidator runs go-playground/validator using the "binding" tag.
type defaultValidator struct {
	// once guards the lazy construction of engine. Bind is called from every
	// request goroutine, so building the engine without synchronization races on
	// the first concurrent requests.
	once   sync.Once
	engine *validator.Validate
}

// ValidateStruct validates structs, pointers to structs and slices of them,
// ignoring every other kind so mapping into scalars stays a no-op.
func (v *defaultValidator) ValidateStruct(dst any) error {
	if dst == nil {
		return nil
	}

	value := reflect.ValueOf(dst)
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return nil
		}
		if value.Elem().Kind() != reflect.Struct {
			return v.ValidateStruct(value.Elem().Interface())
		}
		return v.validator().Struct(dst)
	case reflect.Struct:
		return v.validator().Struct(dst)
	case reflect.Slice, reflect.Array:
		var failures SliceValidationError
		for i := 0; i < value.Len(); i++ {
			if err := v.ValidateStruct(value.Index(i).Interface()); err != nil {
				failures = append(failures, err)
			}
		}
		if len(failures) > 0 {
			return failures
		}
		return nil
	default:
		return nil
	}
}

// SliceValidationError collects per-element validation failures.
type SliceValidationError []error

// Error joins the element failures into one message.
func (e SliceValidationError) Error() string {
	var builder strings.Builder
	for i, err := range e {
		if err == nil {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(fmt.Sprintf("[%d]: %v", i, err))
	}
	return builder.String()
}

func (v *defaultValidator) Engine() any {
	return v.validator()
}

func (v *defaultValidator) validator() *validator.Validate {
	v.once.Do(func() {
		engine := validator.New()
		engine.SetTagName("binding")
		v.engine = engine
	})
	return v.engine
}

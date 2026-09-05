package binding

import (
	"errors"
	"reflect"
	"sync"

	"github.com/go-playground/validator/v10"
)

// --- 验证器 ---

// StructValidator 描述结构体校验器，用于绑定后校验。
// 可通过 SetValidateFn 换成自定义校验入口（如接入不同校验库）。
type StructValidator interface {
	// ValidateStruct 验证结构体，验证通过返回 nil。
	ValidateStruct(any) error
	// Engine 返回底层的验证引擎。
	Engine() any
}

// DefaultValidator 默认校验器，基于 go-playground/validator/v10，标签为 binding。
type DefaultValidator struct {
	once     sync.Once
	validate *validator.Validate
}

var _ StructValidator = (*DefaultValidator)(nil)

// ValidateStruct 验证结构体。
// 支持 struct、指针指向的 struct、以及 slice/array（逐元素验证）。
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

// validateStruct 验证单个结构体。
func (v *DefaultValidator) validateStruct(obj any) error {
	v.lazyInit()
	return v.validate.Struct(obj)
}

// Engine 返回底层的验证引擎。
func (v *DefaultValidator) Engine() any {
	v.lazyInit()
	return v.validate
}

// lazyInit 延迟初始化验证器。
func (v *DefaultValidator) lazyInit() {
	v.once.Do(func() {
		v.validate = validator.New()
		v.validate.SetTagName("binding")
	})
}

// defaultValidator 默认校验实例，未通过 SetValidateFn 替换时使用。
var defaultValidator = &DefaultValidator{}

// validateFn 当前校验入口。绑定器在绑定后校验时调用它；
// 未通过 SetValidateFn 替换时使用内置 defaultValidator。
var validateFn = func(obj any) error {
	return defaultValidator.ValidateStruct(obj)
}

// SetValidateFn 替换全局校验入口；fn 为 nil 时恢复内置默认校验器。
func SetValidateFn(fn func(any) error) {
	if fn == nil {
		validateFn = func(obj any) error {
			return defaultValidator.ValidateStruct(obj)
		}
		return
	}
	validateFn = fn
}

// validate 使用当前校验入口验证 obj。
func validate(obj any) error {
	if validateFn == nil {
		return nil
	}
	return validateFn(obj)
}

// Validate 使用当前校验入口验证结构体。
func Validate(obj any) error {
	return validate(obj)
}

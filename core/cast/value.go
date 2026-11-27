package cast

import (
	"encoding/json"
	"fmt"
	"time"
)

// Value wraps a value of unknown type, such as a path or query parameter, and
// converts it on demand.
//
// The conversions follow the package ones (see ToIntE and friends): nil converts
// to the zero value, a bool is read as 1 or 0, and a byte slice is converted to
// its string form first. On top of that a Value reports whether it is empty and
// offers Or variants that supply a default, which is what a request parameter
// usually needs.
type Value struct {
	value any
}

// NewValue returns a Value wrapping v.
func NewValue(v any) Value {
	return Value{value: v}
}

// Value returns the wrapped value unchanged, for callers that need the type the
// source used rather than one of the conversions.
func (v Value) Value() any {
	return v.value
}

// IsEmpty reports whether the wrapped value carries no data: it is nil, the
// empty string, an empty byte slice or an empty json.Number. Numbers and bools
// are never empty, including zero and false.
func (v Value) IsEmpty() bool {
	switch value := v.value.(type) {
	case nil:
		return true
	case string:
		return value == ""
	case []byte:
		return len(value) == 0
	case json.Number:
		return value == ""
	}
	return false
}

// String returns the wrapped value as text, and is what %v and %s write.
//
// It follows ToStringE, so a value without a text form is marshalled as JSON;
// only a value that cannot be marshalled falls back to fmt.
func (v Value) String() string {
	text, err := ToStringE(v.value)
	if err != nil {
		return fmt.Sprint(v.value)
	}
	return text
}

// StringOr returns the wrapped value as text, or fallback when the value is
// empty (see IsEmpty). It never fails.
func (v Value) StringOr(fallback string) string {
	if v.IsEmpty() {
		return fallback
	}
	return v.String()
}

// Int returns the wrapped value as an int, or reports why it is not one. It
// follows ToIntE.
func (v Value) Int() (int, error) {
	value, err := ToIntE(v.text())
	return value, v.withValue(err)
}

// IntOr returns the wrapped value as an int, or fallback when the value is empty
// or cannot be read as one.
func (v Value) IntOr(fallback int) int {
	if v.IsEmpty() {
		return fallback
	}
	value, err := v.Int()
	if err != nil {
		return fallback
	}
	return value
}

// Int64 returns the wrapped value as an int64, or reports why it is not one. It
// follows ToInt64E.
func (v Value) Int64() (int64, error) {
	value, err := ToInt64E(v.text())
	return value, v.withValue(err)
}

// Int64Or returns the wrapped value as an int64, or fallback when the value is
// empty or cannot be read as one.
func (v Value) Int64Or(fallback int64) int64 {
	if v.IsEmpty() {
		return fallback
	}
	value, err := v.Int64()
	if err != nil {
		return fallback
	}
	return value
}

// Uint returns the wrapped value as a uint, or reports why it is not one. It
// follows ToUintE, so negative values are rejected.
func (v Value) Uint() (uint, error) {
	value, err := ToUintE(v.text())
	return value, v.withValue(err)
}

// UintOr returns the wrapped value as a uint, or fallback when the value is
// empty or cannot be read as one.
func (v Value) UintOr(fallback uint) uint {
	if v.IsEmpty() {
		return fallback
	}
	value, err := v.Uint()
	if err != nil {
		return fallback
	}
	return value
}

// Uint64 returns the wrapped value as a uint64, or reports why it is not one.
// It follows ToUint64E, so negative values are rejected.
func (v Value) Uint64() (uint64, error) {
	value, err := ToUint64E(v.text())
	return value, v.withValue(err)
}

// Uint64Or returns the wrapped value as a uint64, or fallback when the value is
// empty or cannot be read as one.
func (v Value) Uint64Or(fallback uint64) uint64 {
	if v.IsEmpty() {
		return fallback
	}
	value, err := v.Uint64()
	if err != nil {
		return fallback
	}
	return value
}

// Float32 returns the wrapped value as a float32, or reports why it is not one.
// It follows ToFloat32E, which also rejects a value that overflows float32.
func (v Value) Float32() (float32, error) {
	value, err := ToFloat32E(v.text())
	return value, v.withValue(err)
}

// Float32Or returns the wrapped value as a float32, or fallback when the value
// is empty or cannot be read as one.
func (v Value) Float32Or(fallback float32) float32 {
	if v.IsEmpty() {
		return fallback
	}
	value, err := v.Float32()
	if err != nil {
		return fallback
	}
	return value
}

// Float64 returns the wrapped value as a float64, or reports why it is not one.
// It follows ToFloat64E.
func (v Value) Float64() (float64, error) {
	value, err := ToFloat64E(v.text())
	return value, v.withValue(err)
}

// Float64Or returns the wrapped value as a float64, or fallback when the value
// is empty or cannot be read as one.
func (v Value) Float64Or(fallback float64) float64 {
	if v.IsEmpty() {
		return fallback
	}
	value, err := v.Float64()
	if err != nil {
		return fallback
	}
	return value
}

// Bool returns the wrapped value as a bool, or reports why it is not one. It
// follows ToBoolE, so a non-zero number is true and a non-zero json.Number is
// true as well.
func (v Value) Bool() (bool, error) {
	value, err := ToBoolE(v.text())
	return value, v.withValue(err)
}

// BoolOr returns the wrapped value as a bool, or fallback when the value is
// empty or cannot be read as one.
func (v Value) BoolOr(fallback bool) bool {
	if v.IsEmpty() {
		return fallback
	}
	value, err := v.Bool()
	if err != nil {
		return fallback
	}
	return value
}

// Duration returns the wrapped value as a time.Duration, or reports why it is
// not one. It follows ToDurationE: a number is read as nanoseconds and a string
// is parsed as "300ms" or "5s".
func (v Value) Duration() (time.Duration, error) {
	value, err := ToDurationE(v.text())
	return value, v.withValue(err)
}

// DurationOr returns the wrapped value as a time.Duration, or fallback when the
// value is empty or cannot be read as one.
func (v Value) DurationOr(fallback time.Duration) time.Duration {
	if v.IsEmpty() {
		return fallback
	}
	value, err := v.Duration()
	if err != nil {
		return fallback
	}
	return value
}

// Time returns the wrapped value as a time.Time, or reports why it is not one.
// It follows ToTimeE: a string is tried against the supported layouts (RFC 3339,
// time.DateTime and time.DateOnly) and then as a Unix timestamp, and a number is
// read as Unix seconds.
func (v Value) Time() (time.Time, error) {
	value, err := ToTimeE(v.text())
	return value, v.withValue(err)
}

// TimeOr returns the wrapped value as a time.Time, or fallback when the value is
// empty or cannot be read as one.
func (v Value) TimeOr(fallback time.Time) time.Time {
	if v.IsEmpty() {
		return fallback
	}
	value, err := v.Time()
	if err != nil {
		return fallback
	}
	return value
}

// text returns a byte slice as its string form, since the package conversions
// read text from strings, and anything else unchanged.
func (v Value) text() any {
	if value, ok := v.value.([]byte); ok {
		return string(value)
	}
	return v.value
}

// withValue adds the wrapped value to a failed conversion: the package
// conversions name the source and target type but not the offending value.
func (v Value) withValue(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("value %q (%T): %w", v.String(), v.value, err)
}

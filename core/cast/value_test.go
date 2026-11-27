package cast

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewValueKeepsUnderlyingValue(t *testing.T) {
	raw := []byte("42")
	assert.Equal(t, raw, NewValue(raw).Value())

	assert.Nil(t, NewValue(nil).Value())
	assert.Equal(t, 42, NewValue(42).Value())
}

func TestValueIsEmpty(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  bool
	}{
		{"nil", nil, true},
		{"empty_string", "", true},
		{"empty_bytes", []byte{}, true},
		{"nil_bytes", []byte(nil), true},
		{"empty_json_number", json.Number(""), true},
		{"string_zero", "0", false},
		{"int_zero", 0, false},
		{"false", false, false},
		{"json_number_zero", json.Number("0"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NewValue(tt.value).IsEmpty())
		})
	}
}

func TestValueString(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"nil", nil, ""},
		{"string", "text", "text"},
		{"bytes", []byte("bytes"), "bytes"},
		{"json_number", json.Number("42"), "42"},
		{"float64", 1.5, "1.5"},
		{"float64_whole", 2.0, "2"},
		{"float32", float32(3.14), "3.14"},
		{"int", 42, "42"},
		{"bool", true, "true"},
		{"error", errors.New("boom"), "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewValue(tt.value)
			assert.Equal(t, tt.want, v.String())
			assert.Equal(t, tt.want, fmt.Sprint(v))
		})
	}
}

func TestValueInt(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  int
	}{
		{"int", 42, 42},
		{"int8_negative", int8(-8), -8},
		{"int32", int32(32), 32},
		{"uint", uint(7), 7},
		{"whole_float", 3.0, 3},
		{"fractional_float", 3.9, 3},
		{"string", "42", 42},
		{"padded_string", " 42 ", 42},
		{"bytes", []byte("7"), 7},
		{"json_number", json.Number("9"), 9},
		{"bool_true", true, 1},
		{"nil", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewValue(tt.value).Int()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	for _, value := range []any{math.NaN(), math.Inf(-1), "abc", json.Number("1.5"), []string{"1"}, struct{}{}} {
		_, err := NewValue(value).Int()
		assert.Error(t, err, "%#v should not convert to int", value)
	}
}

func TestValueInt64(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  int64
	}{
		{"int", 1, 1},
		{"int8", int8(2), 2},
		{"int16", int16(3), 3},
		{"int32", int32(4), 4},
		{"int64", int64(5), 5},
		{"uint", uint(6), 6},
		{"uint8", uint8(7), 7},
		{"uint16", uint16(8), 8},
		{"uint32", uint32(9), 9},
		{"uint64", uint64(10), 10},
		{"float32_whole", float32(11), 11},
		{"float64_whole", 12.0, 12},
		{"json_number", json.Number("13"), 13},
		{"string", "14", 14},
		{"padded_string", " 15 ", 15},
		{"bytes", []byte("16"), 16},
		{"bool_true", true, 1},
		{"nil", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewValue(tt.value).Int64()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	invalid := []any{
		math.NaN(),
		math.Inf(1),
		1e19,
		uint64(math.MaxInt64) + 1,
		json.Number("1.5"),
		"abc",
		"0xff",
		struct{}{},
	}
	for _, value := range invalid {
		_, err := NewValue(value).Int64()
		assert.Error(t, err, "%#v should not convert to int64", value)
	}
}

func TestValueUint64(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  uint64
	}{
		{"uint", uint(1), 1},
		{"uint8", uint8(2), 2},
		{"uint16", uint16(3), 3},
		{"uint32", uint32(4), 4},
		{"uint64", uint64(5), 5},
		{"int", 6, 6},
		{"int8", int8(7), 7},
		{"int16", int16(8), 8},
		{"int32", int32(9), 9},
		{"int64", int64(10), 10},
		{"float32_whole", float32(11), 11},
		{"float64_whole", 12.0, 12},
		{"float64_fractional", 13.9, 13},
		{"json_number", json.Number("14"), 14},
		{"string", "15", 15},
		{"padded_string", " 16 ", 16},
		{"bytes", []byte("17"), 17},
		{"bool_true", true, 1},
		{"bool_false", false, 0},
		{"nil", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewValue(tt.value).Uint64()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	invalid := []any{
		-1,
		int8(-1),
		int64(-1),
		-1.0,
		math.NaN(),
		math.Inf(1),
		1e20,
		json.Number("-1"),
		"abc",
		struct{}{},
	}
	for _, value := range invalid {
		_, err := NewValue(value).Uint64()
		assert.Error(t, err, "%#v should not convert to uint64", value)
	}
}

func TestValueFloat64(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  float64
	}{
		{"int", 1, 1},
		{"int8_negative", int8(-2), -2},
		{"int16", int16(3), 3},
		{"int32", int32(4), 4},
		{"int64", int64(5), 5},
		{"uint", uint(6), 6},
		{"uint8", uint8(7), 7},
		{"uint16", uint16(8), 8},
		{"uint32", uint32(9), 9},
		{"uint64", uint64(10), 10},
		{"float32", float32(1.5), 1.5},
		{"float64", 2.5, 2.5},
		{"json_number", json.Number("1.25"), 1.25},
		{"string", "2.5", 2.5},
		{"padded_string", " 3.5 ", 3.5},
		{"bytes", []byte("4.5"), 4.5},
		{"bool_true", true, 1},
		{"nil", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewValue(tt.value).Float64()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	for _, value := range []any{json.Number("abc"), "abc", []int{1}, struct{}{}} {
		_, err := NewValue(value).Float64()
		assert.Error(t, err, "%#v should not convert to float64", value)
	}
}

func TestValueBool(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  bool
	}{
		{"true", true, true},
		{"false", false, false},
		{"string_one", "1", true},
		{"string_zero", "0", false},
		{"string_true", "TRUE", true},
		{"string_false", "false", false},
		{"padded_string", " T ", true},
		{"bytes", []byte("true"), true},
		{"json_number_one", json.Number("1"), true},
		{"json_number_zero", json.Number("0"), false},
		{"json_number_other", json.Number("2"), true},
		{"int_one", 1, true},
		{"int_zero", int64(0), false},
		{"int_other", 2, true},
		{"negative_int", int8(-1), true},
		{"float_one", 1.0, true},
		{"float_zero", 0.0, false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewValue(tt.value).Bool()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	for _, value := range []any{"yes", json.Number("1.5"), struct{}{}} {
		_, err := NewValue(value).Bool()
		assert.Error(t, err, "%#v should not convert to bool", value)
	}
}

func TestValueOrReturnsFallback(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		assert.Equal(t, "text", NewValue("text").StringOr("fallback"))
		assert.Equal(t, "0", NewValue(0).StringOr("fallback"))
		assert.Equal(t, "fallback", NewValue("").StringOr("fallback"))
		assert.Equal(t, "fallback", NewValue(nil).StringOr("fallback"))
	})

	t.Run("int", func(t *testing.T) {
		assert.Equal(t, 42, NewValue("42").IntOr(7))
		assert.Equal(t, 0, NewValue(0).IntOr(7))
		assert.Equal(t, 7, NewValue("").IntOr(7))
		assert.Equal(t, 7, NewValue("abc").IntOr(7))
		assert.Equal(t, 7, NewValue(nil).IntOr(7))
	})

	t.Run("int64", func(t *testing.T) {
		assert.Equal(t, int64(42), NewValue("42").Int64Or(7))
		assert.Equal(t, int64(7), NewValue("").Int64Or(7))
		assert.Equal(t, int64(7), NewValue("1.5").Int64Or(7))
		assert.Equal(t, int64(7), NewValue(nil).Int64Or(7))
	})

	t.Run("uint64", func(t *testing.T) {
		assert.Equal(t, uint64(42), NewValue("42").Uint64Or(7))
		assert.Equal(t, uint64(7), NewValue("").Uint64Or(7))
		assert.Equal(t, uint64(7), NewValue(-1).Uint64Or(7))
		assert.Equal(t, uint64(7), NewValue(nil).Uint64Or(7))
	})

	t.Run("float64", func(t *testing.T) {
		assert.Equal(t, 1.5, NewValue("1.5").Float64Or(0.5))
		assert.Equal(t, 0.5, NewValue("").Float64Or(0.5))
		assert.Equal(t, 0.5, NewValue("abc").Float64Or(0.5))
		assert.Equal(t, 0.5, NewValue(nil).Float64Or(0.5))
	})

	t.Run("bool", func(t *testing.T) {
		assert.Equal(t, true, NewValue("1").BoolOr(false))
		assert.Equal(t, false, NewValue("").BoolOr(false))
		assert.Equal(t, true, NewValue("").BoolOr(true))
		assert.Equal(t, true, NewValue("yes").BoolOr(true))
		assert.Equal(t, false, NewValue(nil).BoolOr(false))
	})
}

func TestValueConversionErrorCarriesValue(t *testing.T) {
	_, err := NewValue("abc").Uint64()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `value "abc" (string)`)
	assert.Contains(t, err.Error(), "failed to cast string to uint64")

	var castErr *ErrCastFailed
	require.ErrorAs(t, err, &castErr)
	assert.Equal(t, "string", castErr.From)
	assert.Equal(t, "uint64", castErr.To)

	_, err = NewValue([]byte("abc")).Int64()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `value "abc" ([]uint8)`)
	assert.Contains(t, err.Error(), "failed to cast string to int64")
}

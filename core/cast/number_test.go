package cast

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ToInt tests ---

func TestToInt(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  int
	}{
		{"int", 42, 42},
		{"int8", int8(8), 8},
		{"int16", int16(16), 16},
		{"int32", int32(32), 32},
		{"int64", int64(64), 64},
		{"uint", uint(42), 42},
		{"uint8", uint8(8), 8},
		{"uint16", uint16(16), 16},
		{"uint32", uint32(32), 32},
		{"uint64", uint64(64), 64},
		{"float32", float32(3.14), 3},
		{"float64", float64(3.99), 3},
		{"bool_true", true, 1},
		{"bool_false", false, 0},
		{"string", "123", 123},
		{"string_zero_padded", "08", 8},
		{"string_hex_not_supported", "0xff", 0}, // hexadecimal is no longer accepted accidentally; parsing is always base 10
		{"json.Number", json.Number("42"), 42},
		{"nil", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ToInt(tt.input))
		})
	}
}

func TestToIntE_Error(t *testing.T) {
	_, err := ToIntE("abc")
	assert.Error(t, err)

	_, err = ToIntE([]int{1, 2})
	assert.Error(t, err)
}

func TestToIntE_NonFiniteFloat(t *testing.T) {
	// NaN and Inf cannot be converted to integers safely, so an error must be returned
	// instead of an undefined value
	_, err := ToIntE(math.NaN())
	assert.Error(t, err)
	_, err = ToIntE(math.Inf(1))
	assert.Error(t, err)

	_, err = ToInt64E(math.NaN())
	assert.Error(t, err)
	_, err = ToInt64E(math.Inf(-1))
	assert.Error(t, err)

	_, err = ToUint64E(math.NaN())
	assert.Error(t, err)
	_, err = ToUint64E(math.Inf(1))
	assert.Error(t, err)
}

func TestToIntE_Overflow(t *testing.T) {
	// uint64 values above math.MaxInt must error instead of overflowing silently
	// into negative values
	_, err := ToIntE(uint64(math.MaxUint64))
	assert.Error(t, err)

	_, err = ToInt64E(uint64(math.MaxUint64))
	assert.Error(t, err)

	// values within bounds convert normally
	n, err := ToInt64E(uint64(math.MaxInt64))
	require.NoError(t, err)
	assert.Equal(t, int64(math.MaxInt64), n)
}

// --- ToInt64 tests ---

func TestToInt64(t *testing.T) {
	assert.Equal(t, int64(42), ToInt64(42))
	assert.Equal(t, int64(42), ToInt64("42"))
	assert.Equal(t, int64(42), ToInt64(json.Number("42")))
	assert.Equal(t, int64(0), ToInt64(nil))
}

func TestToInt64E_LargeNumber(t *testing.T) {
	n, err := ToInt64E("9223372036854775807")
	require.NoError(t, err)
	assert.Equal(t, int64(9223372036854775807), n)
}

// --- ToUint tests ---

func TestToUint(t *testing.T) {
	assert.Equal(t, uint(42), ToUint(uint(42)))
	assert.Equal(t, uint(42), ToUint("42"))
	assert.Equal(t, uint(42), ToUint(int64(42)))
}

func TestToUint64E_Negative(t *testing.T) {
	_, err := ToUint64E(-1)
	assert.Error(t, err)
}

// --- Float tests ---

func TestToFloat64(t *testing.T) {
	assert.Equal(t, float64(3.14), ToFloat64(3.14))
	assert.Equal(t, float64(3.14), ToFloat64("3.14"))
	assert.Equal(t, float64(3), ToFloat64(3))
	assert.Equal(t, float64(3), ToFloat64(json.Number("3")))
}

func TestToFloat32(t *testing.T) {
	assert.Equal(t, float32(3.14), ToFloat32("3.14"))
}

func TestToFloat64E_Error(t *testing.T) {
	_, err := ToFloat64E("not a number")
	assert.Error(t, err)
}

// --- Additional coverage: full type family and failure branches of the E-series ---

func TestToInt64E_TypeFamily(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  int64
	}{
		{"int", int(1), 1},
		{"int8", int8(8), 8},
		{"int16", int16(16), 16},
		{"int32", int32(32), 32},
		{"int64", int64(64), 64},
		{"uint", uint(5), 5},
		{"uint8", uint8(1), 1},
		{"uint16", uint16(2), 2},
		{"uint32", uint32(3), 3},
		{"uint64", uint64(4), 4},
		{"float32", float32(3), 3},
		{"float64", 3.14, 3},
		{"bool_true", true, 1},
		{"bool_false", false, 0},
		{"json.Number", json.Number("9"), 9},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ToInt64E(c.input)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestToInt64E_Errors(t *testing.T) {
	_, err := ToInt64E("abc")
	assert.Error(t, err)
	_, err = ToInt64E(json.Number("1e2")) // Int64() cannot parse it
	assert.Error(t, err)
	_, err = ToInt64E([]int{1})
	assert.Error(t, err)
	_, err = ToInt64E(uint(math.MaxUint64)) // overflow
	assert.Error(t, err)
}

func TestToUint64E_TypeFamily(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  uint64
	}{
		{"int", int(42), 42},
		{"int8", int8(8), 8},
		{"int16", int16(16), 16},
		{"int32", int32(32), 32},
		{"int64", int64(64), 64},
		{"uint", uint(7), 7},
		{"uint8", uint8(1), 1},
		{"uint16", uint16(2), 2},
		{"uint32", uint32(3), 3},
		{"uint64", uint64(4), 4},
		{"float32", float32(3.5), 3},
		{"float64", 3.5, 3},
		{"bool_true", true, 1},
		{"json.Number", json.Number("7"), 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ToUint64E(c.input)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestToUint64E_Errors(t *testing.T) {
	_, err := ToUint64E(nil)
	assert.NoError(t, err) // nil → 0, no error

	_, err = ToUint64E(int8(-1))
	assert.Error(t, err)
	_, err = ToUint64E(int64(-5))
	assert.Error(t, err)
	_, err = ToUint64E(float64(-1))
	assert.Error(t, err)
	_, err = ToUint64E("abc")
	assert.Error(t, err)
	_, err = ToUint64E(json.Number("1e2"))
	assert.Error(t, err)
	_, err = ToUint64E([]int{1})
	assert.Error(t, err)
}

func TestToUint64(t *testing.T) {
	assert.Equal(t, uint64(42), ToUint64(uint64(42)))
	assert.Equal(t, uint64(0), ToUint64("-1")) // failure returns the zero value
	assert.Equal(t, uint64(42), ToUint64("42"))
}

func TestToFloat64E_TypeFamily(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  float64
	}{
		{"int", int(3), 3},
		{"int8", int8(3), 3},
		{"int64", int64(3), 3},
		{"uint", uint(3), 3},
		{"uint64", uint64(3), 3},
		{"float32", float32(1.5), 1.5},
		{"float64", 2.5, 2.5},
		{"bool_true", true, 1},
		{"bool_false", false, 0},
		{"json.Number", json.Number("3"), 3},
		{"string", "3.14", 3.14},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ToFloat64E(c.input)
			require.NoError(t, err)
			assert.InDelta(t, c.want, got, 0.001)
		})
	}
}

func TestToFloat64E_Errors(t *testing.T) {
	_, err := ToFloat64E(nil)
	assert.NoError(t, err) // nil → 0

	_, err = ToFloat64E(json.Number("abc"))
	assert.Error(t, err)
	_, err = ToFloat64E([]int{1})
	assert.Error(t, err)
}

func TestToIntE_ExtraEdges(t *testing.T) {
	// uint overflow (on 32-bit platforms the boundary is decided by math.MaxInt)
	_, err := ToIntE(uint(math.MaxUint64))
	assert.Error(t, err)

	// non-finite float32
	_, err = ToIntE(float32(math.Inf(1)))
	assert.Error(t, err)

	// json.Number Int64 failure
	_, err = ToIntE(json.Number("1.5"))
	assert.Error(t, err)

	// a negative json.Number works
	n, err := ToIntE(json.Number("-7"))
	require.NoError(t, err)
	assert.Equal(t, -7, n)
}

// --- Overflow protection (previously returned garbage values silently with a nil error) ---

// TestToInt64E_FloatOverflow is a regression test: floats outside the int64 range must error.
// Previous behaviour: the Go spec calls an out-of-range float-to-integer conversion
// "implementation-specific"; on amd64 int64(1e30) produced -9223372036854775808 with a nil error.
func TestToInt64E_FloatOverflow(t *testing.T) {
	bad := []float64{1e30, -1e30, math.MaxFloat64, -math.MaxFloat64}
	for _, f := range bad {
		v, err := ToInt64E(f)
		assert.Error(t, err, "ToInt64E(%v) should fail", f)
		assert.Equal(t, int64(0), v)
	}

	// values within bounds still convert normally
	v, err := ToInt64E(float64(42))
	require.NoError(t, err)
	assert.Equal(t, int64(42), v)

	// -2^63 is exactly representable, so it is a valid input
	v, err = ToInt64E(-9223372036854775808.0)
	require.NoError(t, err)
	assert.Equal(t, int64(math.MinInt64), v)

	// 2^63 is above the int64 upper bound (float64 cannot represent MaxInt64 exactly)
	_, err = ToInt64E(9223372036854775808.0)
	assert.Error(t, err)

	// NaN / Inf still error
	_, err = ToInt64E(math.NaN())
	assert.Error(t, err)
}

// TestToIntE_FloatOverflow is a regression test: ToIntE needs the same overflow check.
func TestToIntE_FloatOverflow(t *testing.T) {
	for _, f := range []float64{1e30, -1e30} {
		v, err := ToIntE(f)
		assert.Error(t, err, "ToIntE(%v) should fail", f)
		assert.Equal(t, 0, v)
	}

	// the normal range keeps the original truncate-towards-zero semantics
	v, err := ToIntE(7.9)
	require.NoError(t, err)
	assert.Equal(t, 7, v)
}

// TestToUint64E_FloatOverflow is a regression test: floats outside the uint64 range must error.
func TestToUint64E_FloatOverflow(t *testing.T) {
	for _, f := range []float64{1e30, math.MaxFloat64} {
		v, err := ToUint64E(f)
		assert.Error(t, err, "ToUint64E(%v) should fail", f)
		assert.Equal(t, uint64(0), v)
	}

	v, err := ToUint64E(float64(42))
	require.NoError(t, err)
	assert.Equal(t, uint64(42), v)

	// negative values still error
	_, err = ToUint64E(-1.5)
	assert.Error(t, err)
}

// TestToUintE_Overflow verifies that a uint64 beyond the uint range reports an error.
// It actually triggers on 32-bit platforms; on 64-bit platforms it ensures that path
// does not report false positives.
func TestToUintE_Overflow(t *testing.T) {
	v, err := ToUintE(uint64(42))
	require.NoError(t, err)
	assert.Equal(t, uint(42), v)

	big := uint64(math.MaxUint32) + 1
	v, err = ToUintE(big)
	if uint64(uint(big)) == big {
		// the platform uint is wide enough, so it must succeed
		require.NoError(t, err)
		assert.Equal(t, uint(big), v)
	} else {
		assert.Error(t, err, "narrowing overflow must be reported")
	}
}

// TestToFloat32E_Overflow is a regression test: a narrowing float64→float32 overflow must error.
// Previous behaviour: ToFloat32E(1e300) returned +Inf with a nil error.
func TestToFloat32E_Overflow(t *testing.T) {
	_, err := ToFloat32E(1e300)
	assert.Error(t, err)
	_, err = ToFloat32E(-1e300)
	assert.Error(t, err)
	_, err = ToFloat32E(math.MaxFloat64)
	assert.Error(t, err)

	// normal values are unaffected
	v, err := ToFloat32E(1.5)
	require.NoError(t, err)
	assert.Equal(t, float32(1.5), v)

	// an input that is already ±Inf is passed through; not a narrowing overflow
	v, err = ToFloat32E(math.Inf(1))
	require.NoError(t, err)
	assert.True(t, math.IsInf(float64(v), 1))
}

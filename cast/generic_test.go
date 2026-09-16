package cast

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Generic To tests ---

func TestTo_Generic(t *testing.T) {
	assert.Equal(t, 123, To[int]("123"))
	assert.Equal(t, int64(123), To[int64]("123"))
	assert.Equal(t, uint(42), To[uint]("42"))
	assert.Equal(t, "456", To[string](456))
	assert.Equal(t, true, To[bool]("true"))
	assert.Equal(t, 3.14, To[float64]("3.14"))
	assert.Equal(t, 5*time.Second, To[time.Duration]("5s"))
}

func TestTo_GenericStruct(t *testing.T) {
	type User struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	u := To[User](map[string]any{"name": "Alice", "age": 30})
	assert.Equal(t, "Alice", u.Name)
	assert.Equal(t, 30, u.Age)
}

// --- Generic ToE tests (returns an error, so failures are detectable) ---

func TestToE_Success(t *testing.T) {
	n, err := ToE[int]("123")
	assert.NoError(t, err)
	assert.Equal(t, 123, n)

	s, err := ToE[string](456)
	assert.NoError(t, err)
	assert.Equal(t, "456", s)

	d, err := ToE[time.Duration]("5s")
	assert.NoError(t, err)
	assert.Equal(t, 5*time.Second, d)
}

func TestToE_Error(t *testing.T) {
	_, err := ToE[int]("abc")
	assert.Error(t, err)

	var ce *ErrCastFailed
	assert.True(t, errors.As(err, &ce))
	assert.Equal(t, "string", ce.From)
	assert.Equal(t, "int", ce.To)

	_, err = ToE[bool]("not a bool")
	assert.Error(t, err)

	// a failed conversion returns the zero value of the type
	n, err := ToE[int]("abc")
	assert.Error(t, err)
	assert.Equal(t, 0, n)
}

// --- Additional coverage: ToE narrow type family and per-target failure branches ---

func TestToE_NarrowTypes(t *testing.T) {
	assert.Equal(t, int8(127), To[int8]("127"))
	assert.Equal(t, int16(-1), To[int16]("-1"))
	assert.Equal(t, int32(32), To[int32]("32"))
	assert.Equal(t, uint8(255), To[uint8]("255"))
	assert.Equal(t, uint16(16), To[uint16]("16"))
	assert.Equal(t, uint32(32), To[uint32]("32"))
	assert.Equal(t, uint64(64), To[uint64]("64"))
	assert.Equal(t, float32(1.5), To[float32]("1.5"))
}

func TestToE_NarrowTypeErrors(t *testing.T) {
	// every target type must return the zero value instead of panicking when conversion fails
	assert.Equal(t, int8(0), To[int8]("abc"))
	assert.Equal(t, int64(0), To[int64]("abc"))
	assert.Equal(t, uint(0), To[uint]("-1"))
	assert.Equal(t, float64(0), To[float64]("x"))
	assert.Equal(t, time.Duration(0), To[time.Duration]("bad"))
	assert.Equal(t, time.Time{}, To[time.Time]("bad"))
	assert.Equal(t, "", To[string](func() {})) // Marshal fails
}

func TestToE_TimeType(t *testing.T) {
	tm := To[time.Time]("2024-01-15T10:30:00Z")
	assert.Equal(t, 2024, tm.Year())

	tm = To[time.Time](int64(1700000000))
	assert.Equal(t, 2023, tm.Year())
}

func TestToE_DefaultJSONPath(t *testing.T) {
	type User struct {
		Name string `json:"name"`
	}

	// default branch: non-basic types go through JSON marshal/unmarshal
	u := To[User](map[string]any{"name": "Alice"})
	assert.Equal(t, "Alice", u.Name)

	// nil input → zero value
	assert.Equal(t, User{}, To[User](nil))

	// JSON unmarshal type mismatch yields an error → zero value (nil map)
	assert.Nil(t, To[map[string]int](map[string]any{"a": "x"}))

	// json.Marshal fails → zero value
	type Bad struct {
		F func()
	}
	assert.Equal(t, Bad{}, To[Bad](Bad{F: func() {}}))
}

func TestToE_JsonNumberInput(t *testing.T) {
	n, err := ToE[int](json.Number("42"))
	require.NoError(t, err)
	assert.Equal(t, 42, n)

	// json.Number conversion fails
	_, err = ToE[int](json.Number("1.5"))
	assert.Error(t, err)
}

// --- Narrow type overflow protection (previously wrapped around silently) ---

// TestToE_NarrowIntOverflow is a regression test: a target type that is too narrow must error.
// Previous behaviour: ToE[int8]("200") wrapped around to -56 with a nil error.
func TestToE_NarrowIntOverflow(t *testing.T) {
	// int8: [-128, 127]
	v, err := ToE[int8]("127")
	require.NoError(t, err)
	assert.Equal(t, int8(127), v)

	_, err = ToE[int8]("128")
	assert.Error(t, err)
	_, err = ToE[int8]("200")
	assert.Error(t, err)
	_, err = ToE[int8]("-129")
	assert.Error(t, err)

	n, err := ToE[int8]("-128")
	require.NoError(t, err)
	assert.Equal(t, int8(-128), n)

	// int16: [-32768, 32767]
	_, err = ToE[int16]("32768")
	assert.Error(t, err)
	_, err = ToE[int16]("32767")
	require.NoError(t, err)
	_, err = ToE[int16]("-32769")
	assert.Error(t, err)

	// int32: [-2147483648, 2147483647]
	_, err = ToE[int32]("2147483648")
	assert.Error(t, err)
	_, err = ToE[int32]("2147483647")
	require.NoError(t, err)

	// out-of-range native integers error as well (previously the narrowing conversion
	// after ToIntE wrapped around silently)
	_, err = ToE[int8](200)
	assert.Error(t, err)
	_, err = ToE[int16](40000)
	assert.Error(t, err)

	// out-of-range floats never turn into garbage input first
	_, err = ToE[int8](1e30)
	assert.Error(t, err)
}

// TestToE_NarrowUintOverflow is a regression test: unsigned narrow type overflow must error.
// Previous behaviour: ToE[uint8]("300") wrapped around to 44 with a nil error.
func TestToE_NarrowUintOverflow(t *testing.T) {
	v, err := ToE[uint8]("255")
	require.NoError(t, err)
	assert.Equal(t, uint8(255), v)

	_, err = ToE[uint8]("256")
	assert.Error(t, err)
	_, err = ToE[uint8]("300")
	assert.Error(t, err)

	// uint16: upper bound 65535
	_, err = ToE[uint16]("65536")
	assert.Error(t, err)
	_, err = ToE[uint16]("65535")
	require.NoError(t, err)

	// uint32: upper bound 4294967295
	_, err = ToE[uint32]("4294967296")
	assert.Error(t, err)
	_, err = ToE[uint32]("4294967295")
	require.NoError(t, err)

	// negative values and out-of-range native integers
	_, err = ToE[uint8]("-1")
	assert.Error(t, err)
	_, err = ToE[uint8](300)
	assert.Error(t, err)
}

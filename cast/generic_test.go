package cast

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- 泛型 To 测试 ---

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

// --- 泛型 ToE 测试（带 error，可判断转换失败） ---

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

	// 失败返回类型零值
	n, err := ToE[int]("abc")
	assert.Error(t, err)
	assert.Equal(t, 0, n)
}

// --- 补充：ToE 窄类型族与各目标类型失败分支 ---

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
	// 各目标类型转换失败均应返回零值而非 panic
	assert.Equal(t, int8(0), To[int8]("abc"))
	assert.Equal(t, int64(0), To[int64]("abc"))
	assert.Equal(t, uint(0), To[uint]("-1"))
	assert.Equal(t, float64(0), To[float64]("x"))
	assert.Equal(t, time.Duration(0), To[time.Duration]("bad"))
	assert.Equal(t, time.Time{}, To[time.Time]("bad"))
	assert.Equal(t, "", To[string](func() {})) // Marshal 失败
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

	// default 路径：非基本类型走 JSON marshal/unmarshal
	u := To[User](map[string]any{"name": "Alice"})
	assert.Equal(t, "Alice", u.Name)

	// nil 输入 → 零值
	assert.Equal(t, User{}, To[User](nil))

	// JSON unmarshal 类型不匹配报错 → 返回零值（nil map）
	assert.Nil(t, To[map[string]int](map[string]any{"a": "x"}))

	// json.Marshal 失败 → 返回零值
	type Bad struct {
		F func()
	}
	assert.Equal(t, Bad{}, To[Bad](Bad{F: func() {}}))
}

func TestToE_JsonNumberInput(t *testing.T) {
	n, err := ToE[int](json.Number("42"))
	require.NoError(t, err)
	assert.Equal(t, 42, n)

	// json.Number 转换失败
	_, err = ToE[int](json.Number("1.5"))
	assert.Error(t, err)
}

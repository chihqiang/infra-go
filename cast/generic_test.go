package cast

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

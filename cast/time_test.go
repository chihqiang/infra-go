package cast

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ToDuration 测试 ---

func TestToDuration(t *testing.T) {
	assert.Equal(t, 5*time.Second, ToDuration("5s"))
	assert.Equal(t, 100*time.Millisecond, ToDuration("100ms"))
	assert.Equal(t, time.Duration(42), ToDuration(42))
	assert.Equal(t, time.Duration(0), ToDuration(nil))
}

func TestToDurationE_Error(t *testing.T) {
	_, err := ToDurationE("not a duration")
	assert.Error(t, err)
}

// --- ToTime 测试 ---

func TestToTime(t *testing.T) {
	// RFC3339
	tm := ToTime("2024-01-15T10:30:00Z")
	assert.Equal(t, 2024, tm.Year())
	assert.Equal(t, time.January, tm.Month())
	assert.Equal(t, 15, tm.Day())

	// Unix 时间戳
	tm2 := ToTime("1700000000")
	assert.Equal(t, 2023, tm2.Year())

	// 数值
	tm3 := ToTime(int64(1700000000))
	assert.Equal(t, 2023, tm3.Year())

	// nil
	assert.True(t, ToTime(nil).IsZero())
}

func TestToTimeE_Error(t *testing.T) {
	_, err := ToTimeE("not a time")
	assert.Error(t, err)
}

// --- 补充：ToDurationE / ToTimeE 类型族与失败分支 ---

func TestToDurationE_TypeFamily(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  time.Duration
	}{
		{"duration", 5 * time.Second, 5 * time.Second},
		{"int", 42, 42},
		{"int8", int8(8), 8},
		{"int16", int16(16), 16},
		{"int32", int32(32), 32},
		{"int64", int64(64), 64},
		{"uint", uint(7), 7},
		{"uint8", uint8(1), 1},
		{"uint16", uint16(2), 2},
		{"uint32", uint32(3), 3},
		{"uint64", uint64(4), 4},
		{"float32", float32(1.5), 1},
		{"float64", 1.5, 1},
		{"string", "5s", 5 * time.Second},
		{"json.Number", json.Number("42"), 42},
		{"nil", nil, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ToDurationE(c.input)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestToDurationE_Errors(t *testing.T) {
	_, err := ToDurationE(json.Number("1.5")) // Int64 失败
	assert.Error(t, err)
	_, err = ToDurationE([]int{})
	assert.Error(t, err)
}

func TestToTimeE_TypeFamily(t *testing.T) {
	base := int64(1700000000)
	cases := []struct {
		name  string
		input any
	}{
		{"time.Time", time.Unix(base, 0)},
		{"int", int(base)},
		{"int8", int8(10)},
		{"int16", int16(10)},
		{"int32", int32(base)},
		{"int64", base},
		{"uint", uint(base)},
		{"uint8", uint8(10)},
		{"uint16", uint16(10)},
		{"uint32", uint32(base)},
		{"uint64", uint64(base)},
		{"float32", float32(base)},
		{"float64", float64(base)},
		{"json.Number", json.Number("1700000000")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ToTimeE(c.input)
			require.NoError(t, err)
			assert.False(t, got.IsZero())
		})
	}
}

func TestToTimeE_Errors(t *testing.T) {
	_, err := ToTimeE(json.Number("1e2"))
	assert.Error(t, err)
	_, err = ToTimeE([]int{})
	assert.Error(t, err)
}

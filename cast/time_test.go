package cast

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

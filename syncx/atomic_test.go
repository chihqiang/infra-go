package syncx

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- hashKey 测试 ---

func TestHashKey_Deterministic(t *testing.T) {
	// 覆盖所有高效路径分支 + 默认回退分支，并断言结果确定（同值同 hash）。
	tests := []any{
		"hello",
		"",
		int(42),
		int(-42),
		int8(8),
		int16(16),
		int32(32),
		int64(64),
		uint(7),
		uint8(1),
		uint16(2),
		uint32(3),
		uint64(4),
		float32(1.5),
		float64(2.5),
		true,
		false,
		// 默认分支：不可直接命中高效路径的复合类型
		struct{ A int }{A: 1},
	}

	for _, v := range tests {
		h1 := hashKey(v)
		h2 := hashKey(v)
		assert.Equal(t, h1, h2, "hashKey should be deterministic for %#v", v)
	}
}

func TestHashKey_DistinguishesValues(t *testing.T) {
	// 不同值应（极大概率）得到不同 hash；
	// 至少保证同类型内给定样本互不冲突，验证 hash 有效分散。
	vals := []int{1, 2, 3, 100, -1, -999}
	seen := make(map[uint64]bool)
	for _, v := range vals {
		h := hashKey(v)
		assert.False(t, seen[h], "hash collision for %d", v)
		seen[h] = true
	}
}

func TestHashKey_StructFallback(t *testing.T) {
	// 默认分支走 fmt.Sprint：结构相同但值不同应产生不同 hash。
	type pair struct {
		A int
		B string
	}
	assert.NotEqual(t, hashKey(pair{A: 1, B: "x"}), hashKey(pair{A: 2, B: "x"}))
}

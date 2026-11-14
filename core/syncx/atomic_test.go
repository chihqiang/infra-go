package syncx

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- hashKey tests ---

func TestHashKey_Deterministic(t *testing.T) {
	// Covers every fast-path branch plus the default fallback, and asserts the
	// result is deterministic (same value, same hash).
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
		// Default branch: a composite type that cannot hit the fast path directly.
		struct{ A int }{A: 1},
	}

	for _, v := range tests {
		h1 := hashKey(v)
		h2 := hashKey(v)
		assert.Equal(t, h1, h2, "hashKey should be deterministic for %#v", v)
	}
}

func TestHashKey_DistinguishesValues(t *testing.T) {
	// Different values should (with overwhelming probability) produce different
	// hashes; at minimum the samples of the same type must not collide, which
	// shows the hash spreads well.
	vals := []int{1, 2, 3, 100, -1, -999}
	seen := make(map[uint64]bool)
	for _, v := range vals {
		h := hashKey(v)
		assert.False(t, seen[h], "hash collision for %d", v)
		seen[h] = true
	}
}

func TestHashKey_StructFallback(t *testing.T) {
	// The default branch uses fmt.Sprint: same shape, different values must
	// produce different hashes.
	type pair struct {
		A int
		B string
	}
	assert.NotEqual(t, hashKey(pair{A: 1, B: "x"}), hashKey(pair{A: 2, B: "x"}))
}

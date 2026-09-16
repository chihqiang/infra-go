package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file verifies that GetAs returns consistent values on both backends (removing
// the type differences of Cache.Get).

type getAsUser struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// bothBackends returns instances of both backends so that the same set of assertions
// can run twice.
func bothBackends(t *testing.T) map[string]Cache {
	t.Helper()
	ctx := context.Background()

	mem := NewMemCache(ctx, time.Minute)
	t.Cleanup(mem.Close)

	rds, _ := newMiniRedis(t)

	return map[string]Cache{
		"mem":   mem,
		"redis": NewRedisCache(rds),
	}
}

// TestGetAs_ScalarConsistentAcrossBackends verifies that scalars read back with a
// consistent type on both backends. When Get is used directly, mem returns an int
// while redis returns a json.Number, so asserting an int fails on redis.
func TestGetAs_ScalarConsistentAcrossBackends(t *testing.T) {
	ctx := context.Background()

	for name, c := range bothBackends(t) {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, c.Set(ctx, "count", 42))

			n, err := GetAs[int](ctx, c, "count")
			require.NoError(t, err)
			assert.Equal(t, 42, n)

			n64, err := GetAs[int64](ctx, c, "count")
			require.NoError(t, err)
			assert.Equal(t, int64(42), n64)

			f, err := GetAs[float64](ctx, c, "count")
			require.NoError(t, err)
			assert.Equal(t, float64(42), f)

			// A string value should read back unchanged on both backends.
			require.NoError(t, c.Set(ctx, "name", "chihqiang"))
			s, err := GetAs[string](ctx, c, "name")
			require.NoError(t, err)
			assert.Equal(t, "chihqiang", s)
		})
	}
}

// TestGetAs_Int64WithinFloat64Range verifies that both backends read back an int64
// unchanged as long as it is exactly representable as a float64.
func TestGetAs_Int64WithinFloat64Range(t *testing.T) {
	ctx := context.Background()
	const exact = int64(9007199254740992) // 2^53, exactly representable as a float64

	for name, c := range bothBackends(t) {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, c.Set(ctx, "big", exact))

			got, err := GetAs[int64](ctx, c, "big")
			require.NoError(t, err)
			assert.Equal(t, exact, got)
		})
	}
}

// TestGetAs_LargeInt64Precision verifies that both backends preserve an int64 above
// 2^53 exactly.
//
// The Redis backend relies on the UseNumber decoding of RedisCache.Get: falling back
// to the default float64 loses precision already at the Get stage, which GetAs cannot
// repair.
func TestGetAs_LargeInt64Precision(t *testing.T) {
	ctx := context.Background()
	const big = int64(9007199254740993) // 2^53 + 1

	for name, c := range bothBackends(t) {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, c.Set(ctx, "big", big))

			got, err := GetAs[int64](ctx, c, "big")
			require.NoError(t, err)
			assert.Equal(t, big, got, "must preserve large int64 exactly")
		})
	}
}

// TestGetAs_StructConsistentAcrossBackends verifies that a struct is restored to its
// concrete type on both backends.
// When Get is used directly, mem returns the original struct while redis returns a
// map[string]any.
// ID is above 2^53 so that large-integer precision inside struct fields is covered as
// well.
func TestGetAs_StructConsistentAcrossBackends(t *testing.T) {
	ctx := context.Background()
	want := getAsUser{ID: 9007199254740993, Name: "chihqiang"}

	for name, c := range bothBackends(t) {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, c.Set(ctx, "user:1", want))

			got, err := GetAs[getAsUser](ctx, c, "user:1")
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

// TestGetAs_SliceConsistentAcrossBackends verifies a slice type.
func TestGetAs_SliceConsistentAcrossBackends(t *testing.T) {
	ctx := context.Background()

	for name, c := range bothBackends(t) {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, c.Set(ctx, "tags", []string{"a", "b"}))

			got, err := GetAs[[]string](ctx, c, "tags")
			require.NoError(t, err)
			assert.Equal(t, []string{"a", "b"}, got)
		})
	}
}

// TestGetAs_Miss verifies that a miss returns the zero value and ErrNotFound.
func TestGetAs_Miss(t *testing.T) {
	ctx := context.Background()

	for name, c := range bothBackends(t) {
		t.Run(name, func(t *testing.T) {
			n, err := GetAs[int](ctx, c, "missing")
			assert.ErrorIs(t, err, ErrNotFound)
			assert.Equal(t, 0, n)

			s, err := GetAs[string](ctx, c, "missing")
			assert.ErrorIs(t, err, ErrNotFound)
			assert.Equal(t, "", s)
		})
	}
}

// TestGetAs_TypeMismatch verifies that a value which cannot be decoded into T returns
// an error instead of a silent zero value.
func TestGetAs_TypeMismatch(t *testing.T) {
	ctx := context.Background()

	for name, c := range bothBackends(t) {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, c.Set(ctx, "user", map[string]any{"id": 1}))

			// object -> scalar is incompatible; it must error instead of returning 0
			_, err := GetAs[int](ctx, c, "user")
			assert.Error(t, err)
		})
	}
}

// TestGetAs_TakeWrittenValues verifies that values written by Take can be read back
// with GetAs.
func TestGetAs_TakeWrittenValues(t *testing.T) {
	ctx := context.Background()

	for name, c := range bothBackends(t) {
		t.Run(name, func(t *testing.T) {
			fetch := func() (any, error) { return getAsUser{ID: 9, Name: "from-fetch"}, nil }
			_, err := c.Take(ctx, "u", fetch)
			require.NoError(t, err)

			got, err := GetAs[getAsUser](ctx, c, "u")
			require.NoError(t, err)
			assert.Equal(t, getAsUser{ID: 9, Name: "from-fetch"}, got)
		})
	}
}

package cache

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetAs reads key and restores the value as the concrete type T, hiding the
// representation differences between backends.
//
// Why it exists: Cache.Get returns any, and the two backends hand back different Go
// types for the same value:
//   - MemCache returns the original type stored (Set("k", 5) -> int(5),
//     Set("k", u) -> user)
//   - RedisCache has to serialize values, so it returns the JSON decoding result
//     (Set("k", 5) -> json.Number("5"), Set("k", u) -> map[string]any)
//
// So code that type-asserts the result of Get panics as soon as the backend is
// switched. GetAs always round-trips through JSON and decodes into T, giving the
// same result on both backends, and large integers are not distorted by an
// intermediate float64 conversion (RedisCache stores numbers as json.Number, see
// decodeUseNumber in rediscache.go).
//
// Usage:
//
//	n, err := cache.GetAs[int64](ctx, c, "counter")
//	var u User
//	u, err := cache.GetAs[User](ctx, c, "user:1")
//
// On a miss it returns (zero value, ErrNotFound); when the value cannot be decoded
// into T it returns an error instead of a silent zero value.
//
// Note: when Get is used directly, numbers on the Redis backend are json.Number
// (not float64). Use GetAs for numeric access, or call json.Number's Int64/Float64
// yourself. See the Cache interface comment.
func GetAs[T any](ctx context.Context, c Cache, key string) (T, error) {
	var zero T

	raw, err := c.Get(ctx, key)
	if err != nil {
		return zero, err
	}

	// The backend may already have returned T itself (e.g. MemCache stored a T).
	if v, ok := raw.(T); ok {
		return v, nil
	}

	// Always decode through a JSON round-trip: the Redis backend returns a
	// JSON-compatible representation and the raw value returned by MemCache is
	// serializable as well. Decoding straight into the concrete type T keeps large
	// integers from losing precision via an intermediate float64.
	data, err := json.Marshal(raw)
	if err != nil {
		return zero, fmt.Errorf("cache: marshal cached value for key %q: %w", key, err)
	}

	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return zero, fmt.Errorf("cache: unmarshal cached value for key %q into %T: %w", key, zero, err)
	}
	return out, nil
}

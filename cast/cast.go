package cast

import (
	"fmt"
	"math"
)

// --- Error definitions ---

// ErrCastFailed reports a failed type conversion.
type ErrCastFailed struct {
	From string // type of the source value
	To   string // target type
}

// Error returns the error message.
func (e *ErrCastFailed) Error() string {
	return fmt.Sprintf("cast: failed to cast %s to %s", e.From, e.To)
}

func castErr(from, to string) error {
	return &ErrCastFailed{From: from, To: to}
}

// isNonFinite reports whether f is NaN or an infinity.
func isNonFinite(f float64) bool {
	return math.IsNaN(f) || math.IsInf(f, 0)
}

// Bounds for float-to-integer conversion.
// float64 cannot represent MaxInt64 (2^63-1) exactly, and converting that integer constant
// to float64 rounds up to 2^63, so the bounds are spelled out explicitly to avoid ambiguity.
const (
	maxInt64AsFloat64  = 9223372036854775808.0  // 2^63
	minInt64AsFloat64  = -9223372036854775808.0 // -2^63
	maxUint64AsFloat64 = 18446744073709551616.0 // 2^64
)

// floatToInt64E converts a float to int64, returning an error for non-finite values or
// values outside the int64 range.
//
// The Go spec calls an out-of-range float-to-integer conversion "implementation-specific":
// on amd64, for example, int64(1e30) yields -9223372036854775808. This must be checked
// explicitly, otherwise callers get a plausible-looking garbage value with a nil error.
func floatToInt64E(f float64, from string) (int64, error) {
	if isNonFinite(f) {
		return 0, castErr(from, "int64")
	}
	if f >= maxInt64AsFloat64 || f < minInt64AsFloat64 {
		return 0, castErr(from, "int64")
	}
	return int64(f), nil
}

// floatToIntE converts a float to int, returning an error for non-finite values or
// values outside the int range.
func floatToIntE(f float64, from string) (int, error) {
	n, err := floatToInt64E(f, from)
	if err != nil {
		return 0, err
	}
	// On 32-bit platforms int is narrower than int64, so an extra check is needed
	// (on 64-bit platforms this comparison is always false).
	if n > math.MaxInt || n < math.MinInt {
		return 0, castErr(from, "int")
	}
	return int(n), nil
}

// floatToUint64E converts a float to uint64, returning an error for non-finite values,
// negative values or values outside the uint64 range.
func floatToUint64E(f float64, from string) (uint64, error) {
	if isNonFinite(f) {
		return 0, castErr(from, "uint64")
	}
	if f < 0 {
		return 0, castErr(from+"(negative)", "uint64")
	}
	if f >= maxUint64AsFloat64 {
		return 0, castErr(from, "uint64")
	}
	return uint64(f), nil
}

// checkIntBitSize verifies that n fits losslessly into a signed integer of bitSize bits
// (bitSize is 8/16/32/64).
func checkIntBitSize(n int64, bitSize int, to string) error {
	if bitSize >= 64 {
		return nil
	}
	min := -(int64(1) << (bitSize - 1))
	max := int64(1)<<(bitSize-1) - 1
	if n < min || n > max {
		return castErr("int64", to)
	}
	return nil
}

// checkUintBitSize verifies that n fits losslessly into an unsigned integer of bitSize bits
// (bitSize is 8/16/32/64).
func checkUintBitSize(n uint64, bitSize int, to string) error {
	if bitSize >= 64 {
		return nil
	}
	if n > uint64(1)<<bitSize-1 {
		return castErr("uint64", to)
	}
	return nil
}

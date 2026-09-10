package cast

import (
	"fmt"
	"math"
)

// --- 错误定义 ---

// ErrCastFailed 类型转换失败。
type ErrCastFailed struct {
	From string // 原始值的类型
	To   string // 目标类型
}

// Error 返回错误信息。
func (e *ErrCastFailed) Error() string {
	return fmt.Sprintf("cast: failed to cast %s to %s", e.From, e.To)
}

func castErr(from, to string) error {
	return &ErrCastFailed{From: from, To: to}
}

// isNonFinite 判断浮点数是否为 NaN 或正负无穷。
func isNonFinite(f float64) bool {
	return math.IsNaN(f) || math.IsInf(f, 0)
}

// 浮点→整数转换的边界。
// float64 无法精确表示 MaxInt64（2^63-1），把整数常量转成 float64 会向上取整到 2^63，
// 因此这里显式写出边界值，避免比较时产生歧义。
const (
	maxInt64AsFloat64  = 9223372036854775808.0  // 2^63
	minInt64AsFloat64  = -9223372036854775808.0 // -2^63
	maxUint64AsFloat64 = 18446744073709551616.0 // 2^64
)

// floatToInt64E 将浮点数转换为 int64，非有限值或超出 int64 范围时返回错误。
//
// Go 规范规定超出范围的浮点→整数转换为「实现相关」，例如 amd64 上
// int64(1e30) 会得到 -9223372036854775808。必须显式判断，
// 否则调用方会拿到一个看似合法的垃圾值且 err 为 nil。
func floatToInt64E(f float64, from string) (int64, error) {
	if isNonFinite(f) {
		return 0, castErr(from, "int64")
	}
	if f >= maxInt64AsFloat64 || f < minInt64AsFloat64 {
		return 0, castErr(from, "int64")
	}
	return int64(f), nil
}

// floatToIntE 将浮点数转换为 int，非有限值或超出 int 范围时返回错误。
func floatToIntE(f float64, from string) (int, error) {
	n, err := floatToInt64E(f, from)
	if err != nil {
		return 0, err
	}
	// 32 位平台上 int 窄于 int64，需要额外判断（64 位平台该比较恒为假）。
	if n > math.MaxInt || n < math.MinInt {
		return 0, castErr(from, "int")
	}
	return int(n), nil
}

// floatToUint64E 将浮点数转换为 uint64，非有限值、负数或超出 uint64 范围时返回错误。
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

// checkIntBitSize 校验 n 能否无损放入 bitSize 位的有符号整数（bitSize 为 8/16/32/64）。
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

// checkUintBitSize 校验 n 能否无损放入 bitSize 位的无符号整数（bitSize 为 8/16/32/64）。
func checkUintBitSize(n uint64, bitSize int, to string) error {
	if bitSize >= 64 {
		return nil
	}
	if n > uint64(1)<<bitSize-1 {
		return castErr("uint64", to)
	}
	return nil
}

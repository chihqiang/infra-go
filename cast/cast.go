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

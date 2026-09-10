package stringx

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// IsEmpty 判断字符串是否为空。
func IsEmpty(s string) bool {
	return len(s) == 0
}

// IsNotBlank 判断字符串是否非空且去除空白后不为空。
func IsNotBlank(s string) bool {
	return len(strings.TrimSpace(s)) > 0
}

// DefaultIfBlank 若字符串为空或纯空白，返回默认值。
func DefaultIfBlank(s, def string) string {
	if IsNotBlank(s) {
		return s
	}
	return def
}

// Reverse 反转字符串。
func Reverse(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

// Capitalize 将字符串首字母转为大写。
// 使用 utf8.DecodeRuneInString 定位首字符边界，
// 正确支持多字节 UTF-8 字符（如中文、Emoji），不会产生字节截断。
func Capitalize(s string) string {
	if s == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(s)
	// 无效 UTF-8 编码时原样返回，避免破坏原字符串。
	if r == utf8.RuneError && size == 1 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

// ToSnakeCase 将驼峰命名转为蛇形命名。
// 例：UserName → user_name, HTTPServer → http_server
func ToSnakeCase(s string) string {
	runes := []rune(s)
	var buf []rune
	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 {
				prevIsLower := unicode.IsLower(runes[i-1])
				nextIsLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
				if prevIsLower || nextIsLower {
					buf = append(buf, '_')
				}
			}
			buf = append(buf, unicode.ToLower(r))
		} else {
			buf = append(buf, r)
		}
	}
	return string(buf)
}

// Chunk 将字符串按固定长度拆分为切片，最后一段可能不足长度。
func Chunk(s string, size int) []string {
	if size <= 0 || len(s) == 0 {
		return nil
	}
	runes := []rune(s)
	// 预分配容量，避免多次扩容
	n := (len(runes) + size - 1) / size
	chunks := make([]string, 0, n)
	for i := 0; i < len(runes); i += size {
		end := i + size
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
	}
	return chunks
}

// maxRepeatBytes Repeat 允许生成的结果长度上限（1 GiB）。
// 超过该上限时返回空串而不是尝试分配：分配失败会以
// "runtime error: makeslice: len out of range" 终止，调用方无法转为普通错误。
const maxRepeatBytes = 1 << 30

// Repeat 将字符串重复 n 次。
// n <= 0、s 为空，或结果长度超过 maxRepeatBytes 时返回空字符串。
//
// 该函数不会 panic：旧实现下 len(s)*n 溢出为负会使
// strings.Builder.Grow 抛出 "negative count"，而巨大的 n 会触发
// makeslice 运行时 panic；两者在参数来自外部输入时都可被用来终止进程。
func Repeat(s string, n int) string {
	if n <= 0 || len(s) == 0 {
		return ""
	}
	// 单一比较同时完成溢出与上限保护：
	// len(s) > maxRepeatBytes/n 等价于 len(s)*n > maxRepeatBytes，
	// 且此处 n > 0，除法本身不会溢出。
	if len(s) > maxRepeatBytes/n {
		return ""
	}
	var buf strings.Builder
	buf.Grow(len(s) * n)
	for i := 0; i < n; i++ {
		buf.WriteString(s)
	}
	return buf.String()
}

// Substr 安全截取子串，支持负数索引（从末尾倒数）。
// 例：Substr("hello", 1, 3) → "el"，Substr("hello", -3, 5) → "llo"
func Substr(s string, start, end int) string {
	runes := []rune(s)
	length := len(runes)

	if start < 0 {
		start = length + start
	}
	if end < 0 {
		end = length + end
	}
	if start < 0 {
		start = 0
	}
	if end > length {
		end = length
	}
	if start >= end {
		return ""
	}
	return string(runes[start:end])
}

// IndexOf 返回子串首次出现的位置，未找到返回 -1。
func IndexOf(s, substr string) int {
	return strings.Index(s, substr)
}

// Count 计算子串在字符串中出现的次数。
func Count(s, substr string) int {
	return strings.Count(s, substr)
}

// Join 使用分隔符连接多个字符串，跳过空字符串。
func Join(sep byte, elem ...string) string {
	var size int
	for _, e := range elem {
		size += len(e)
	}
	if size == 0 {
		return ""
	}

	buf := make([]byte, 0, size+len(elem)-1)
	for _, e := range elem {
		if len(e) == 0 {
			continue
		}

		if len(buf) > 0 {
			buf = append(buf, sep)
		}
		buf = append(buf, e...)
	}

	return string(buf)
}

// Split 按分隔符拆分字符串，自动去除空字符串。
func Split(s string, sep byte) []string {
	if len(s) == 0 {
		return nil
	}
	parts := strings.Split(s, string(sep))
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) > 0 {
			result = append(result, p)
		}
	}
	return result
}

// ToCamelCase 将字符串首字符转为小写（lowerCamelCase），其余字符保持不变。
// 例：UserName → userName、École → école、中文A → 中文A。
//
// 使用 utf8.DecodeRuneInString 定位首字符边界，正确支持多字节 UTF-8 字符。
// 旧实现用 s[i+1:] 拼接（i 恒为首字符的字节偏移 0），
// 首字符为多字节时会丢掉续字节、产生非法 UTF-8（如 "Äbc" → "ä\x84bc"）。
func ToCamelCase(s string) string {
	if s == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(s)
	// 无效 UTF-8 编码时原样返回，避免破坏原字符串。
	if r == utf8.RuneError && size == 1 {
		return s
	}
	return string(unicode.ToLower(r)) + s[size:]
}

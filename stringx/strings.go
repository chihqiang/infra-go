package stringx

import (
	"strings"
	"unicode"
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
func Capitalize(s string) string {
	for i, v := range s {
		return string(unicode.ToUpper(v)) + s[i+1:]
	}
	return ""
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
	var chunks []string
	runes := []rune(s)
	for i := 0; i < len(runes); i += size {
		end := i + size
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
	}
	return chunks
}

// Repeat 将字符串重复 n 次。
func Repeat(s string, n int) string {
	if n <= 0 || len(s) == 0 {
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

// ToCamelCase 将字符串首字母转为小写，其余字符保持不变。
func ToCamelCase(s string) string {
	for i, v := range s {
		return string(unicode.ToLower(v)) + s[i+1:]
	}

	return ""
}

package stringx

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// IsEmpty reports whether s is empty.
func IsEmpty(s string) bool {
	return len(s) == 0
}

// IsNotBlank reports whether s is non-empty after trimming whitespace.
func IsNotBlank(s string) bool {
	return len(strings.TrimSpace(s)) > 0
}

// DefaultIfBlank returns def when s is empty or contains only whitespace.
func DefaultIfBlank(s, def string) string {
	if IsNotBlank(s) {
		return s
	}
	return def
}

// Reverse reverses s.
func Reverse(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

// Capitalize upper-cases the first character of s.
// utf8.DecodeRuneInString is used to locate the first character boundary, so
// multi-byte UTF-8 input (CJK, emoji, ...) is handled correctly and never gets
// truncated mid-byte.
func Capitalize(s string) string {
	if s == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(s)
	// Return s unchanged for invalid UTF-8 so the original bytes are preserved.
	if r == utf8.RuneError && size == 1 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

// ToSnakeCase converts a camelCase or PascalCase name to snake_case.
// Example: UserName → user_name, HTTPServer → http_server
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

// Chunk splits s into pieces of at most size runes; the last piece may be
// shorter.
func Chunk(s string, size int) []string {
	if size <= 0 || len(s) == 0 {
		return nil
	}
	runes := []rune(s)
	// Pre-allocate the capacity to avoid growing the slice repeatedly
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

// maxRepeatBytes is the maximum result length Repeat is allowed to produce
// (1 GiB). Above that limit it returns an empty string instead of attempting the
// allocation: a failed allocation aborts the process with
// "runtime error: makeslice: len out of range", which callers cannot turn into
// an ordinary error.
const maxRepeatBytes = 1 << 30

// Repeat repeats s n times.
// It returns an empty string when n <= 0, when s is empty, or when the result
// would be longer than maxRepeatBytes.
//
// This function never panics: with the old implementation an overflowing
// len(s)*n made strings.Builder.Grow throw "negative count", while a huge n
// triggered a makeslice runtime panic; either one could be used to terminate the
// process when the arguments come from external input.
func Repeat(s string, n int) string {
	if n <= 0 || len(s) == 0 {
		return ""
	}
	// A single comparison handles both overflow and the cap:
	// len(s) > maxRepeatBytes/n is equivalent to len(s)*n > maxRepeatBytes,
	// and n > 0 here, so the division itself cannot overflow.
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

// Substr safely slices a substring; negative indexes count back from the end.
// Example: Substr("hello", 1, 3) → "el", Substr("hello", -3, 5) → "llo"
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

// IndexOf returns the position of the first occurrence of substr, or -1 when it
// is not found.
func IndexOf(s, substr string) int {
	return strings.Index(s, substr)
}

// Count returns how many times substr occurs in s.
func Count(s, substr string) int {
	return strings.Count(s, substr)
}

// Join concatenates the elements with sep between them, skipping empty strings.
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

// Split splits s on sep and drops empty parts.
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

// ToCamelCase lower-cases the first character of s (lowerCamelCase) and leaves
// the remaining characters untouched.
// Example: UserName → userName, École → école, 😀A → 😀A.
//
// utf8.DecodeRuneInString locates the first character boundary, which handles
// multi-byte UTF-8 input correctly. The old implementation concatenated s[i+1:]
// (i was always byte offset 0), so a multi-byte first character lost its
// continuation bytes and produced invalid UTF-8 (e.g. "Äbc" → "ä\x84bc").
func ToCamelCase(s string) string {
	if s == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(s)
	// Return s unchanged for invalid UTF-8 so the original bytes are preserved.
	if r == utf8.RuneError && size == 1 {
		return s
	}
	return string(unicode.ToLower(r)) + s[size:]
}

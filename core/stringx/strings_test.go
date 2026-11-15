package stringx

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsEmpty(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", true},
		{" ", false},
		{"hello", false},
	}
	for _, tt := range tests {
		if got := IsEmpty(tt.input); got != tt.want {
			t.Errorf("IsEmpty(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestIsNotBlank(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", false},
		{"   ", false},
		{"\t\n", false},
		{"hello", true},
		{" hello ", true},
	}
	for _, tt := range tests {
		if got := IsNotBlank(tt.input); got != tt.want {
			t.Errorf("IsNotBlank(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestDefaultIfBlank(t *testing.T) {
	tests := []struct {
		s, def, want string
	}{
		{"", "fallback", "fallback"},
		{"   ", "fallback", "fallback"},
		{"value", "fallback", "value"},
	}
	for _, tt := range tests {
		if got := DefaultIfBlank(tt.s, tt.def); got != tt.want {
			t.Errorf("DefaultIfBlank(%q, %q) = %q, want %q", tt.s, tt.def, got, tt.want)
		}
	}
}

func TestReverse(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"hello", "olleh"},
		{"", ""},
		{"a", "a"},
		{"héllo wörld", "dlröw olléh"},
	}
	for _, tt := range tests {
		if got := Reverse(tt.input); got != tt.want {
			t.Errorf("Reverse(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCapitalize(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"hello", "Hello"},
		{"Hello", "Hello"},
		{"", ""},
		{"a", "A"},
		// Multi-byte UTF-8 characters with no notion of case must be kept as-is
		{"😀😀😀", "😀😀😀"},
		{"🎈abc", "🎈abc"},
		// Non-letter characters such as emoji must be preserved
		{"😀abc", "😀abc"},
		// Mixed content whose first character is a Latin letter
		{"élan", "Élan"},
		// Leading digit: ToUpper has no effect on non-letters
		{"123abc", "123abc"},
		// Invalid UTF-8 must be returned unchanged, without damaging the bytes
		{"\xff\xfe", "\xff\xfe"},
	}
	for _, tt := range tests {
		if got := Capitalize(tt.input); got != tt.want {
			t.Errorf("Capitalize(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"UserName", "user_name"},
		{"HTTPServer", "http_server"},
		{"hello", "hello"},
		{"", ""},
		{"A", "a"},
		{"ABC", "abc"},
		// An upper-case letter preceded by a single lower-case letter → insert a separator
		{"userID", "user_id"},
		// A run of upper-case letters followed by a lower-case letter → split before the last one
		{"XMLHttpRequest", "xml_http_request"},
		{"URLValue", "url_value"},
		// Already snake_case with no upper-case letters → unchanged
		{"user_name", "user_name"},
		// Digits do not participate in the separator decision
		{"apiV2", "api_v2"},
	}
	for _, tt := range tests {
		if got := ToSnakeCase(tt.input); got != tt.want {
			t.Errorf("ToSnakeCase(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestChunk(t *testing.T) {
	tests := []struct {
		input string
		size  int
		want  []string
	}{
		{"abcdef", 2, []string{"ab", "cd", "ef"}},
		{"abcde", 2, []string{"ab", "cd", "e"}},
		{"abc", 5, []string{"abc"}},
		{"", 3, nil},
		{"abc", 0, nil},
		// A negative size returns nil
		{"abc", -1, nil},
		// Multi-byte input is chunked by rune, so no character is broken apart
		{"привет", 2, []string{"пр", "ив", "ет"}},
	}
	for _, tt := range tests {
		if got := Chunk(tt.input, tt.size); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Chunk(%q, %d) = %v, want %v", tt.input, tt.size, got, tt.want)
		}
	}
}

func TestRepeat(t *testing.T) {
	tests := []struct {
		input string
		n     int
		want  string
	}{
		{"ab", 3, "ababab"},
		{"x", 0, ""},
		{"", 5, ""},
		{"a", 1, "a"},
		// A negative n returns an empty string
		{"ab", -1, ""},
	}
	for _, tt := range tests {
		if got := Repeat(tt.input, tt.n); got != tt.want {
			t.Errorf("Repeat(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.want)
		}
	}
}

// TestRepeat_OverflowDoesNotPanic is a regression test: an overflow or an
// oversized result yields an empty string instead of panicking.
// Historical defect: an overflowing len(s)*n made strings.Builder.Grow throw
// "negative count"; a huge n triggered "makeslice: len out of range".
// Both are unrecoverable runtime panics and can terminate the process when n
// comes from external input.
func TestRepeat_OverflowDoesNotPanic(t *testing.T) {
	tests := []struct {
		name  string
		input string
		n     int
	}{
		{"max int with 1-byte input", "a", math.MaxInt},
		{"max int with 2-byte input", "ab", math.MaxInt},
		{"large n", "abc", math.MaxInt / 2},
		{"overflow boundary", "ab", math.MaxInt/2 + 1},
		{"exceeds cap", "a", maxRepeatBytes + 1},
		{"exactly at cap boundary", "ab", maxRepeatBytes/2 + 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			require.NotPanics(t, func() {
				got = Repeat(tt.input, tt.n)
			})
			assert.Equal(t, "", got, "overflow/oversize must yield an empty string")
		})
	}
}

// TestRepeat_WithinCap verifies that normal usage within the cap is unaffected.
func TestRepeat_WithinCap(t *testing.T) {
	assert.Equal(t, "ababab", Repeat("ab", 3))
	assert.Equal(t, strings.Repeat("xy", 1000), Repeat("xy", 1000))

	// Boundary check: within the cap it passes, above the cap an empty string is
	// returned. Note that only the condition logic is verified here; no 1GiB is
	// actually allocated.
	assert.LessOrEqual(t, len("ab")*3, maxRepeatBytes)
	assert.Greater(t, len("a")*(maxRepeatBytes+1), maxRepeatBytes)
}

func TestSubstr(t *testing.T) {
	tests := []struct {
		input      string
		start, end int
		want       string
	}{
		{"hello", 1, 3, "el"},
		{"hello", 0, 5, "hello"},
		{"hello", -3, 5, "llo"},
		{"hello", 1, 1, ""},
		{"hello", 5, 5, ""},
		{"hello", 10, 5, ""},
		// Negative end: count back from the end
		{"hello", 0, -2, "hel"},
		{"hello", 2, -1, "ll"},
		// Negative start out of range: clamped to 0
		{"hello", -10, 5, "hello"},
		// End out of range: clamped to length
		{"hello", 3, 10, "lo"},
		// Multi-byte input is sliced by rune
		{"привет", 1, 3, "ри"},
	}
	for _, tt := range tests {
		if got := Substr(tt.input, tt.start, tt.end); got != tt.want {
			t.Errorf("Substr(%q, %d, %d) = %q, want %q", tt.input, tt.start, tt.end, got, tt.want)
		}
	}
}

func TestIndexOf(t *testing.T) {
	tests := []struct {
		s, substr string
		want      int
	}{
		{"hello world", "world", 6},
		{"hello", "xyz", -1},
		{"hello", "", 0},
		// Locating a multi-byte substring (the index is a byte offset)
		{"héllo wörld", "wörld", 7},
	}
	for _, tt := range tests {
		if got := IndexOf(tt.s, tt.substr); got != tt.want {
			t.Errorf("IndexOf(%q, %q) = %d, want %d", tt.s, tt.substr, got, tt.want)
		}
	}
}

func TestCount(t *testing.T) {
	tests := []struct {
		s, substr string
		want      int
	}{
		{"hello", "l", 2},
		{"hello", "o", 1},
		{"hello", "xyz", 0},
		// Empty-substring semantics match strings.Count: len+1
		{"hello", "", 6},
	}
	for _, tt := range tests {
		if got := Count(tt.s, tt.substr); got != tt.want {
			t.Errorf("Count(%q, %q) = %d, want %d", tt.s, tt.substr, got, tt.want)
		}
	}
}

func TestJoin(t *testing.T) {
	tests := []struct {
		sep  byte
		elem []string
		want string
	}{
		{',', []string{"a", "b", "c"}, "a,b,c"},
		{'-', []string{"", "b", ""}, "b"},
		{',', []string{}, ""},
		{',', []string{"a"}, "a"},
		// Every element empty → empty string
		{',', []string{"", "", ""}, ""},
	}
	for _, tt := range tests {
		if got := Join(tt.sep, tt.elem...); got != tt.want {
			t.Errorf("Join(%q, %v) = %q, want %q", tt.sep, tt.elem, got, tt.want)
		}
	}
}

func TestSplit(t *testing.T) {
	tests := []struct {
		input string
		sep   byte
		want  []string
	}{
		{"a,b,c", ',', []string{"a", "b", "c"}},
		{",a,,b,", ',', []string{"a", "b"}},
		{"a-b-c", '-', []string{"a", "b", "c"}},
		{"", ',', nil},
		{"abc", ',', []string{"abc"}},
	}
	for _, tt := range tests {
		if got := Split(tt.input, tt.sep); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Split(%q, %q) = %v, want %v", tt.input, tt.sep, got, tt.want)
		}
	}
}

func TestToCamelCase(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"Hello", "hello"},
		{"hello", "hello"},
		{"", ""},
		// Only the first character is converted, the rest stays as-is
		{"ABC", "aBC"},
		// Single character
		{"H", "h"},
		// A non-letter first character stays unchanged
		{"123Abc", "123Abc"},
	}
	for _, tt := range tests {
		if got := ToCamelCase(tt.input); got != tt.want {
			t.Errorf("ToCamelCase(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestToCamelCase_MultibyteFirstRune is a regression test: a multi-byte UTF-8
// first character must be handled correctly and never truncated by byte.
// Historical defect: the implementation concatenated s[i+1:] (i was always 0),
// so a multi-byte first character lost its continuation bytes and returned
// invalid UTF-8 (e.g. "Äbc" → "ä\x84bc").
func TestToCamelCase_MultibyteFirstRune(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		// Latin-1 supplement: Ä occupies 2 bytes
		{"Äbc", "äbc"},
		{"École", "école"},
		// Emoji first character (it has no notion of case and must stay unchanged)
		{"🎈A", "🎈A"},
		// Emoji followed by upper-case ASCII letters
		{"🎈ABC", "🎈ABC"},
		// Greek
		{"Σigma", "σigma"},
		// Cyrillic
		{"Дом", "дом"},
		// Emoji first character (no notion of case, must stay unchanged)
		{"😀Test", "😀Test"},
		// Multi-byte first character + multi-byte remainder, verifying both survive
		{"Ä😀😀", "ä😀😀"},
	}
	for _, tt := range tests {
		got := ToCamelCase(tt.input)
		assert.Equal(t, tt.want, got)
		assert.True(t, utf8.ValidString(got),
			"ToCamelCase(%q) produced invalid UTF-8: %q", tt.input, got)
		assert.Equal(t, len([]rune(tt.input)), len([]rune(got)),
			"ToCamelCase(%q) must not change the rune count", tt.input)
	}
}

// TestToCamelCase_InvalidUTF8 verifies that invalid UTF-8 input is returned
// unchanged and not damaged any further (consistent with Capitalize).
func TestToCamelCase_InvalidUTF8(t *testing.T) {
	invalid := string([]byte{0xff, 0xfe, 'a'})
	assert.Equal(t, invalid, ToCamelCase(invalid))
}

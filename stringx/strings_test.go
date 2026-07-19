package stringx

import (
	"reflect"
	"testing"
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
		{"你好世界", "界世好你"},
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
	}
	for _, tt := range tests {
		if got := Repeat(tt.input, tt.n); got != tt.want {
			t.Errorf("Repeat(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.want)
		}
	}
}

func TestSubstr(t *testing.T) {
	tests := []struct {
		input       string
		start, end  int
		want        string
	}{
		{"hello", 1, 3, "el"},
		{"hello", 0, 5, "hello"},
		{"hello", -3, 5, "llo"},
		{"hello", 1, 1, ""},
		{"hello", 5, 5, ""},
		{"hello", 10, 5, ""},
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
	}
	for _, tt := range tests {
		if got := ToCamelCase(tt.input); got != tt.want {
			t.Errorf("ToCamelCase(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

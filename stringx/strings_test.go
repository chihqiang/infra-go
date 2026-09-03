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
		// 多字节 UTF-8 字符：中文无大小写概念，应原样保留
		{"你好世界", "你好世界"},
		{"中文abc", "中文abc"},
		// Emoji 等非字母字符应保留
		{"😀abc", "😀abc"},
		// 首字符是拉丁字母的混合内容
		{"élan", "Élan"},
		// 数字开头：ToUpper 对非字母无影响
		{"123abc", "123abc"},
		// 无效 UTF-8 编码应原样返回，不破坏字节
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
		// 末位大写前是单个小写字母 → 加分隔
		{"userID", "user_id"},
		// 连续大写后接小写 → 在最后一个大写前分隔
		{"XMLHttpRequest", "xml_http_request"},
		{"URLValue", "url_value"},
		// 已含下划线且无大写 → 保持不变
		{"user_name", "user_name"},
		// 数字不参与分隔判断
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
		// 负数 size 返回 nil
		{"abc", -1, nil},
		// 多字节按 rune 分割，不产生乱码
		{"你好世界", 2, []string{"你好", "世界"}},
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
		// 负数 n 返回空串
		{"ab", -1, ""},
	}
	for _, tt := range tests {
		if got := Repeat(tt.input, tt.n); got != tt.want {
			t.Errorf("Repeat(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.want)
		}
	}
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
		// end 负数：从末尾倒数
		{"hello", 0, -2, "hel"},
		{"hello", 2, -1, "ll"},
		// start 负数越界：截断到 0
		{"hello", -10, 5, "hello"},
		// end 越界：截断到 length
		{"hello", 3, 10, "lo"},
		// 中文按 rune 截取
		{"你好世界", 1, 3, "好世"},
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
		// 中文子串定位
		{"你好世界你好", "世界", 6},
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
		// 空子串语义与 strings.Count 一致：len+1
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
		// 全部为空元素 → 空串
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
		// 仅转首字母，其余保持不变
		{"ABC", "aBC"},
		// 单个字符
		{"H", "h"},
		// 非字母首字符保持不变
		{"123Abc", "123Abc"},
	}
	for _, tt := range tests {
		if got := ToCamelCase(tt.input); got != tt.want {
			t.Errorf("ToCamelCase(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

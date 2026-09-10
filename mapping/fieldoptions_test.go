package mapping

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- parseKeyAndOptions 测试 ---

func TestParseKeyAndOptions_NoTag(t *testing.T) {
	type S struct {
		Name string
	}
	field := reflect.TypeOf(S{}).Field(0)
	key, opts, err := parseKeyAndOptions("json", field)
	assert.NoError(t, err)
	assert.Equal(t, "Name", key)
	assert.Nil(t, opts)
}

func TestParseKeyAndOptions_SimpleKey(t *testing.T) {
	type S struct {
		Name string `json:"name"`
	}
	field := reflect.TypeOf(S{}).Field(0)
	key, opts, err := parseKeyAndOptions("json", field)
	assert.NoError(t, err)
	assert.Equal(t, "name", key)
	assert.Nil(t, opts)
}

func TestParseKeyAndOptions_Default(t *testing.T) {
	type S struct {
		Name string `json:",default=hello"`
	}
	field := reflect.TypeOf(S{}).Field(0)
	key, opts, err := parseKeyAndOptions("json", field)
	assert.NoError(t, err)
	assert.Equal(t, "Name", key)
	assert.NotNil(t, opts)
	assert.Equal(t, "hello", opts.Default)
}

func TestParseKeyAndOptions_Optional(t *testing.T) {
	type S struct {
		Port int `json:",optional"`
	}
	field := reflect.TypeOf(S{}).Field(0)
	_, opts, err := parseKeyAndOptions("json", field)
	assert.NoError(t, err)
	assert.True(t, opts.Optional)
}

func TestParseKeyAndOptions_Env(t *testing.T) {
	type S struct {
		Name string `json:",env=APP_NAME"`
	}
	field := reflect.TypeOf(S{}).Field(0)
	_, opts, err := parseKeyAndOptions("json", field)
	assert.NoError(t, err)
	assert.Equal(t, "APP_NAME", opts.EnvVar)
}

func TestParseKeyAndOptions_Options(t *testing.T) {
	type S struct {
		Mode string `json:",options=[file,console]"`
	}
	field := reflect.TypeOf(S{}).Field(0)
	_, opts, err := parseKeyAndOptions("json", field)
	assert.NoError(t, err)
	assert.Equal(t, []string{"file", "console"}, opts.Options)
}

func TestParseKeyAndOptions_Range(t *testing.T) {
	type S struct {
		Port int `json:",range=[0:65535]"`
	}
	field := reflect.TypeOf(S{}).Field(0)
	_, opts, err := parseKeyAndOptions("json", field)
	assert.NoError(t, err)
	assert.NotNil(t, opts.Range)
	assert.True(t, opts.Range.leftInclude)
	assert.True(t, opts.Range.rightInclude)
	assert.Equal(t, 0.0, opts.Range.left)
	assert.Equal(t, 65535.0, opts.Range.right)
}

func TestParseKeyAndOptions_RangeOpen(t *testing.T) {
	type S struct {
		Port int `json:",range=[0:1000)"`
	}
	field := reflect.TypeOf(S{}).Field(0)
	_, opts, err := parseKeyAndOptions("json", field)
	assert.NoError(t, err)
	assert.True(t, opts.Range.leftInclude)
	assert.False(t, opts.Range.rightInclude)
}

func TestParseKeyAndOptions_Combined(t *testing.T) {
	type S struct {
		Port int `json:"port,default=8080,range=[1:65535]"`
	}
	field := reflect.TypeOf(S{}).Field(0)
	key, opts, err := parseKeyAndOptions("json", field)
	assert.NoError(t, err)
	assert.Equal(t, "port", key)
	assert.Equal(t, "8080", opts.Default)
	assert.NotNil(t, opts.Range)
}

func TestParseKeyAndOptions_StringOption(t *testing.T) {
	type S struct {
		Name string `json:"name,string"`
	}
	field := reflect.TypeOf(S{}).Field(0)
	_, opts, err := parseKeyAndOptions("json", field)
	assert.NoError(t, err)
	assert.True(t, opts.FromString)
}

// --- parseNumberRange 测试 ---

func TestParseNumberRange(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"[0:100]", false},
		{"(0:100]", false},
		{"[0:100)", false},
		{"(0:100)", false},
		{"[:100]", false},
		{"[0:]", false},
		{"[100:0]", true},
		{"[2:2)", true},
		{"[2:2]", false},
		{"", true},
		{"abc", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := parseNumberRange(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// --- isInRange 测试 ---

func TestIsInRange(t *testing.T) {
	opts := &fieldOptions{Range: &numberRange{left: 0, leftInclude: true, right: 100, rightInclude: false}}
	assert.True(t, opts.isInRange(0))
	assert.True(t, opts.isInRange(50))
	assert.True(t, opts.isInRange(99))
	assert.False(t, opts.isInRange(100))
	assert.False(t, opts.isInRange(-1))

	opts2 := &fieldOptions{}
	assert.True(t, opts2.isInRange(999999))

	opts3 := &fieldOptions{Range: &numberRange{left: 0, leftInclude: false, right: 100, rightInclude: true}}
	assert.False(t, opts3.isInRange(0))
	assert.True(t, opts3.isInRange(100))
}

// --- 补充：边界与错误分支 ---

func TestIsInRange_RightExceeded(t *testing.T) {
	// 右闭区间越界（覆盖 isInRange 的 right 越界分支）
	opts := &fieldOptions{Range: &numberRange{left: 0, leftInclude: true, right: 100, rightInclude: true}}
	assert.False(t, opts.isInRange(101))
}

func TestParseKeyAndOptions_InvalidOption(t *testing.T) {
	// 任一 option 解析失败应返回错误
	type S struct {
		X string `json:"x,range=bad"`
	}
	_, _, err := parseKeyAndOptions("json", reflect.TypeOf(S{}).Field(0))
	assert.Error(t, err)
}

// TestParseKeyAndOptions_Inherit 验证 inherit 被明确拒绝。
//
// 历史行为：inherit 被解析并写入 fieldOptions.Inherit，但 unmarshaler 从未读取它 ——
// 文档承诺"从父级继承值"，实际什么都没做。本设计中没有"父级"概念，
// 因此改为报错，而不是继续静默忽略。
func TestParseKeyAndOptions_Inherit(t *testing.T) {
	type S struct {
		X string `json:"x,inherit"`
	}
	_, _, err := parseKeyAndOptions("json", reflect.TypeOf(S{}).Field(0))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
	assert.Contains(t, err.Error(), "inherit")
}

func TestParseKeyAndOptions_OptionalDep(t *testing.T) {
	type S struct {
		X string `json:"x,optional=other"`
	}
	_, opts, err := parseKeyAndOptions("json", reflect.TypeOf(S{}).Field(0))
	assert.NoError(t, err)
	require.NotNil(t, opts)
	assert.True(t, opts.Optional)
	assert.Equal(t, "other", opts.OptionalDep)
	assert.False(t, opts.OptionalDepNegate, "without ! the dependency is not negated")
}

func TestParseKeyAndOptions_OptionalDepNegated(t *testing.T) {
	type S struct {
		X string `json:"x,optional=!other"`
	}
	_, opts, err := parseKeyAndOptions("json", reflect.TypeOf(S{}).Field(0))
	require.NoError(t, err)
	require.NotNil(t, opts)
	assert.True(t, opts.Optional)
	assert.Equal(t, "other", opts.OptionalDep)
	assert.True(t, opts.OptionalDepNegate)
}

// TestParseKeyAndOptions_OptionalNegatedEmpty 验证 `optional=!` 被拒绝。
func TestParseKeyAndOptions_OptionalNegatedEmpty(t *testing.T) {
	type S struct {
		X string `json:"x,optional=!"`
	}
	_, _, err := parseKeyAndOptions("json", reflect.TypeOf(S{}).Field(0))
	assert.Error(t, err)
}

// TestParseKeyAndOptions_UnknownOption 回归测试：未知选项必须报错。
//
// 历史缺陷：parseOption 用 strings.HasPrefix 逐个匹配且无 default 分支，
// 拼写错误（如 optinal）被静默忽略 —— 字段按必填处理，
// 直到运行期才以 "field not set" 暴露，且错误信息不指向真实原因。
func TestParseKeyAndOptions_UnknownOption(t *testing.T) {
	cases := []struct {
		name string
		tag  string
	}{
		{"typo in optional", `json:"x,optinal"`},
		{"typo in default", `json:"x,defualt=1"`},
		{"typo in range", `json:"x,rang=[1:2]"`},
		{"unknown word", `json:"x,bogus"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var field reflect.StructField
			switch tc.tag {
			case `json:"x,optinal"`:
				field = reflect.TypeOf(struct {
					X string `json:"x,optinal"`
				}{}).Field(0)
			case `json:"x,defualt=1"`:
				field = reflect.TypeOf(struct {
					X string `json:"x,defualt=1"`
				}{}).Field(0)
			case `json:"x,rang=[1:2]"`:
				field = reflect.TypeOf(struct {
					X string `json:"x,rang=[1:2]"`
				}{}).Field(0)
			default:
				field = reflect.TypeOf(struct {
					X string `json:"x,bogus"`
				}{}).Field(0)
			}
			_, _, err := parseKeyAndOptions("json", field)
			require.Error(t, err, "unknown option must be rejected")
			assert.Contains(t, err.Error(), "unknown option")
		})
	}
}

// TestParseKeyAndOptions_PrefixMustNotMatch 回归测试：前缀不得误匹配。
//
// 历史缺陷：`defaultFoo=bar` 会因 HasPrefix("default") 而生效为 Default="bar"。
func TestParseKeyAndOptions_PrefixMustNotMatch(t *testing.T) {
	type S struct {
		X string `json:"x,defaultFoo=bar"`
	}
	_, _, err := parseKeyAndOptions("json", reflect.TypeOf(S{}).Field(0))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown option")
}

func TestParseKeyAndOptions_OptionalInvalid(t *testing.T) {
	// optional=a=b 多等号报错
	type S struct {
		X string `json:"x,optional=a=b"`
	}
	_, _, err := parseKeyAndOptions("json", reflect.TypeOf(S{}).Field(0))
	assert.Error(t, err)
}

func TestParseKeyAndOptions_OptionDoubleEqual(t *testing.T) {
	// default=foo=bar 值内含多个等号报错
	type S struct {
		X string `json:"x,default=foo=bar"`
	}
	_, _, err := parseKeyAndOptions("json", reflect.TypeOf(S{}).Field(0))
	assert.Error(t, err)
}

func TestParseOptionsValue_Pipe(t *testing.T) {
	// 管道分隔
	type S struct {
		X string `json:"x,options=a|b|c"`
	}
	_, opts, err := parseKeyAndOptions("json", reflect.TypeOf(S{}).Field(0))
	assert.NoError(t, err)
	require.NotNil(t, opts)
	assert.Equal(t, []string{"a", "b", "c"}, opts.Options)
}

func TestParseNumberRange_MoreErrors(t *testing.T) {
	for _, input := range []string{"[", "1]", "[:]", "abc:5]", "[1:xyz]"} {
		_, err := parseNumberRange(input)
		assert.Error(t, err, "parseNumberRange(%q) should error", input)
	}
}

func TestIsRightInclude_Invalid(t *testing.T) {
	_, err := isRightInclude('x')
	assert.Error(t, err)
}

func TestParseSegments_EscapedComma(t *testing.T) {
	// 转义逗号不分割
	segs := parseSegments(`default=a\,b,c`)
	assert.Equal(t, []string{"default=a,b", "c"}, segs)
}

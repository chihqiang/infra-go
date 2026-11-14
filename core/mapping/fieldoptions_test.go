package mapping

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- parseKeyAndOptions tests ---

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

// --- parseNumberRange tests ---

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

// --- isInRange tests ---

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

// --- Additional coverage: boundary and error branches ---

func TestIsInRange_RightExceeded(t *testing.T) {
	// Right-closed bound exceeded (covers the right out-of-range branch of isInRange)
	opts := &fieldOptions{Range: &numberRange{left: 0, leftInclude: true, right: 100, rightInclude: true}}
	assert.False(t, opts.isInRange(101))
}

func TestParseKeyAndOptions_InvalidOption(t *testing.T) {
	// A failure to parse any option must return an error
	type S struct {
		X string `json:"x,range=bad"`
	}
	_, _, err := parseKeyAndOptions("json", reflect.TypeOf(S{}).Field(0))
	assert.Error(t, err)
}

// TestParseKeyAndOptions_Inherit verifies that inherit is rejected explicitly.
//
// Historic behaviour: inherit was parsed and stored into fieldOptions.Inherit,
// but the unmarshaler never read it — the documentation promised "inherit the
// value from the parent" while nothing actually happened. This design has no
// notion of a "parent", so it now reports an error instead of continuing to
// ignore it silently.
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

// TestParseKeyAndOptions_OptionalNegatedEmpty verifies that `optional=!` is rejected.
func TestParseKeyAndOptions_OptionalNegatedEmpty(t *testing.T) {
	type S struct {
		X string `json:"x,optional=!"`
	}
	_, _, err := parseKeyAndOptions("json", reflect.TypeOf(S{}).Field(0))
	assert.Error(t, err)
}

// TestParseKeyAndOptions_UnknownOption is a regression test: unknown options must
// report an error.
//
// Historic defect: parseOption matched each prefix with strings.HasPrefix and had
// no default branch, so typos (such as optinal) were silently ignored — the field
// was treated as required and only surfaced at runtime as "field not set", with
// an error message that did not point at the real cause.
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

// TestParseKeyAndOptions_PrefixMustNotMatch is a regression test: prefixes must
// not match incorrectly.
//
// Historic defect: `defaultFoo=bar` took effect as Default="bar" because of
// HasPrefix("default").
func TestParseKeyAndOptions_PrefixMustNotMatch(t *testing.T) {
	type S struct {
		X string `json:"x,defaultFoo=bar"`
	}
	_, _, err := parseKeyAndOptions("json", reflect.TypeOf(S{}).Field(0))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown option")
}

func TestParseKeyAndOptions_OptionalInvalid(t *testing.T) {
	// optional=a=b with a second equals sign must error
	type S struct {
		X string `json:"x,optional=a=b"`
	}
	_, _, err := parseKeyAndOptions("json", reflect.TypeOf(S{}).Field(0))
	assert.Error(t, err)
}

func TestParseKeyAndOptions_OptionDoubleEqual(t *testing.T) {
	// default=foo=bar with several equals signs inside the value must error
	type S struct {
		X string `json:"x,default=foo=bar"`
	}
	_, _, err := parseKeyAndOptions("json", reflect.TypeOf(S{}).Field(0))
	assert.Error(t, err)
}

func TestParseOptionsValue_Pipe(t *testing.T) {
	// Pipe separated
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
	// An escaped comma does not split
	segs := parseSegments(`default=a\,b,c`)
	assert.Equal(t, []string{"default=a,b", "c"}, segs)
}

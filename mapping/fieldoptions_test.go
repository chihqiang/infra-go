package mapping

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
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

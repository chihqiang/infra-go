package cast

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- ToBool 测试 ---

func TestToBool(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  bool
	}{
		{"bool_true", true, true},
		{"bool_false", false, false},
		{"int_1", 1, true},
		{"int_0", 0, false},
		{"string_true", "true", true},
		{"string_1", "1", true},
		{"string_false", "false", false},
		{"string_0", "0", false},
		{"string_T", "T", true},
		{"string_F", "F", false},
		{"float_1", 1.0, true},
		{"float_0", 0.0, false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ToBool(tt.input))
		})
	}
}

func TestToBoolE_Error(t *testing.T) {
	_, err := ToBoolE("not a bool")
	assert.Error(t, err)
}

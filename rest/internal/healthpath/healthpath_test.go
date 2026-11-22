package healthpath

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", Default},
		{"   ", Default},
		{"healthz", "/healthz"},
		{" /ready ", "/ready"},
		{"/health", "/health"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, Normalize(tt.in), "Normalize(%q)", tt.in)
	}
}

func TestValid(t *testing.T) {
	valid := []string{"/health", "/healthz", "/internal/health", "/v1/ready"}
	for _, path := range valid {
		assert.True(t, Valid(path), "Valid(%q)", path)
	}

	invalid := []string{"", "/", "/health/", "/he alth", "/health?x=1", "/health#frag", "/health{id}"}
	for _, path := range invalid {
		assert.False(t, Valid(path), "Valid(%q)", path)
	}

	// Normalize output must satisfy Valid for ordinary values.
	require.True(t, Valid(Normalize("healthz")))
}

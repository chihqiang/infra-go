package mapping

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file covers the behaviour of optional conditional dependencies
// (optional=Dep / optional=!Dep).
//
// Historic defect: OptionalDep was parsed into fieldOptions, but the unmarshaler
// never read it, nor did it read Inherit. The result was that the documentation
// promised "this field is optional when other is not set" while the field was in
// fact **unconditionally optional**, with no error at all.

// TestUnmarshal_OptionalDep_PresentMakesOptional verifies `optional=other`:
// the field is optional (may be omitted) when the dependency is present.
func TestUnmarshal_OptionalDep_PresentMakesOptional(t *testing.T) {
	type Config struct {
		Other string `json:"other,optional"`
		Value string `json:"value,optional=other"`
	}

	t.Run("dependency present, field omitted is allowed", func(t *testing.T) {
		var cfg Config
		err := UnmarshalJsonMap(map[string]any{"other": "x"}, &cfg)
		require.NoError(t, err)
		assert.Equal(t, "x", cfg.Other)
		assert.Equal(t, "", cfg.Value)
	})

	t.Run("dependency absent, field omitted is rejected", func(t *testing.T) {
		var cfg Config
		err := UnmarshalJsonMap(map[string]any{}, &cfg)
		require.Error(t, err, "without the dependency the field must be required")
		assert.Contains(t, err.Error(), "required because")
		assert.Contains(t, err.Error(), "other")
	})

	t.Run("dependency present and field set", func(t *testing.T) {
		var cfg Config
		err := UnmarshalJsonMap(map[string]any{"other": "x", "value": "v"}, &cfg)
		require.NoError(t, err)
		assert.Equal(t, "v", cfg.Value)
	})
}

// TestUnmarshal_OptionalDepNegated verifies `optional=!other`:
// the field is optional when the dependency is not present.
func TestUnmarshal_OptionalDepNegated(t *testing.T) {
	type Config struct {
		Other string `json:"other,optional"`
		Value string `json:"value,optional=!other"`
	}

	t.Run("dependency absent, field omitted is allowed", func(t *testing.T) {
		var cfg Config
		err := UnmarshalJsonMap(map[string]any{}, &cfg)
		require.NoError(t, err)
		assert.Equal(t, "", cfg.Value)
	})

	t.Run("dependency present, field omitted is rejected", func(t *testing.T) {
		var cfg Config
		err := UnmarshalJsonMap(map[string]any{"other": "x"}, &cfg)
		require.Error(t, err, "with the dependency set the field must be required")
		assert.Contains(t, err.Error(), "required because")
	})

	t.Run("dependency present and field set", func(t *testing.T) {
		var cfg Config
		err := UnmarshalJsonMap(map[string]any{"other": "x", "value": "v"}, &cfg)
		require.NoError(t, err)
		assert.Equal(t, "v", cfg.Value)
	})
}

// TestUnmarshal_OptionalDepWithDefault verifies that when a default exists the
// default logic still runs even if the dependency is unmet (the default takes
// precedence over the "required" decision).
func TestUnmarshal_OptionalDepWithDefault(t *testing.T) {
	type Config struct {
		Other string `json:"other,optional"`
		Value string `json:"value,default=d,optional=other"`
	}

	var cfg Config
	err := UnmarshalJsonMap(map[string]any{}, &cfg)
	require.NoError(t, err, "a default satisfies the field, so the dependency is irrelevant")
	assert.Equal(t, "d", cfg.Value)
}

// TestUnmarshal_OptionalDepUnknownDependency is a regression test: a misspelled
// dependency name must report an error.
//
// Without the check, a typo in the dependency name makes the field
// **permanently required** (the dependency is never found), a silent behavioural
// deviation that is hard to track down.
func TestUnmarshal_OptionalDepUnknownDependency(t *testing.T) {
	type Config struct {
		Other string `json:"other,optional"`
		Value string `json:"value,optional=missing"`
	}

	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"other": "x"}, &cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match any field key")
}

// TestUnmarshal_OptionalDepCaseInsensitiveViaCanonicalKey verifies that the
// dependency lookup and the field lookup take the same path (equally
// case-insensitive when canonicalKey is set).
func TestUnmarshal_OptionalDepCaseInsensitiveViaCanonicalKey(t *testing.T) {
	type Config struct {
		Other string `json:"other,optional"`
		Value string `json:"value,optional=other"`
	}

	// The config key is upper case while the field tag is lower case, so the
	// dependency check must be equally insensitive
	var cfg Config
	err := UnmarshalJsonMap(map[string]any{"OTHER": "x"}, &cfg,
		WithCanonicalKeyFunc(lowerFunc))
	require.NoError(t, err)
	assert.Equal(t, "x", cfg.Other)
}

// TestUnmarshal_OptionalDepNested verifies that conditional optionality applies
// independently inside nested structs.
func TestUnmarshal_OptionalDepNested(t *testing.T) {
	type Inner struct {
		Flag  string `json:"flag,optional"`
		Extra string `json:"extra,optional=flag"`
	}
	type Config struct {
		Inner Inner `json:"inner,optional"`
	}

	t.Run("nested dependency present", func(t *testing.T) {
		var cfg Config
		err := UnmarshalJsonMap(map[string]any{
			"inner": map[string]any{"flag": "on"},
		}, &cfg)
		require.NoError(t, err)
		assert.Equal(t, "on", cfg.Inner.Flag)
	})

	t.Run("nested dependency absent", func(t *testing.T) {
		var cfg Config
		err := UnmarshalJsonMap(map[string]any{
			"inner": map[string]any{},
		}, &cfg)
		require.Error(t, err, "nested optional dependency must be enforced too")
	})
}

// lowerFunc is used by WithCanonicalKeyFunc to avoid an extra strings import.
func lowerFunc(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

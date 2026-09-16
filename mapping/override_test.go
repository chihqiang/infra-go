package mapping

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- FillAndOverride tests ---

func TestFillAndOverride_Basic(t *testing.T) {
	type Config struct {
		Host    string        `json:",default=0.0.0.0"`
		Port    int           `json:",default=8080"`
		Timeout time.Duration `json:",default=5s"`
	}

	var c Config
	err := FillAndOverride(&c, Config{
		Host:    "127.0.0.1",
		Timeout: 10 * time.Second,
	})
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1", c.Host) // non-zero overrides
	assert.Equal(t, 8080, c.Port)        // a zero value keeps the default
	assert.Equal(t, 10*time.Second, c.Timeout)
}

func TestFillAndOverride_ZeroValueKeepsDefault(t *testing.T) {
	type Config struct {
		Host string `json:",default=localhost"`
		Port int    `json:",default=8080"`
	}

	// overrides is all zero values: defaults must be kept
	var c Config
	err := FillAndOverride(&c, Config{})
	require.NoError(t, err)
	assert.Equal(t, "localhost", c.Host)
	assert.Equal(t, 8080, c.Port)
}

func TestFillAndOverride_StringEmptyKeepsDefault(t *testing.T) {
	type Config struct {
		Host string `json:",default=localhost"`
	}

	// An empty string counts as unset and does not override the default
	var c Config
	err := FillAndOverride(&c, Config{Host: ""})
	require.NoError(t, err)
	assert.Equal(t, "localhost", c.Host)
}

func TestFillAndOverride_OptionalStringAlwaysOverrides(t *testing.T) {
	type Config struct {
		Secret string `json:",optional"`
	}

	// A string that is optional without default: an empty string also counts as a
	// valid value (always overrides)
	var c Config
	err := FillAndOverride(&c, Config{Secret: "s3cret"})
	require.NoError(t, err)
	assert.Equal(t, "s3cret", c.Secret)
}

func TestFillAndOverride_PointerExplicitZero(t *testing.T) {
	type Config struct {
		Port *int `json:",optional"`
	}

	zero := 0
	var c Config
	err := FillAndOverride(&c, Config{Port: &zero})
	require.NoError(t, err)
	require.NotNil(t, c.Port)
	assert.Equal(t, 0, *c.Port)
}

func TestFillAndOverride_NestedStruct(t *testing.T) {
	type DB struct {
		Host string `json:",default=localhost"`
		Port int    `json:",default=3306"`
	}
	type Config struct {
		DB DB `json:"db"`
	}

	// A nested struct only overrides its non-zero sub-fields
	var c Config
	err := FillAndOverride(&c, Config{DB: DB{Port: 5432}})
	require.NoError(t, err)
	assert.Equal(t, "localhost", c.DB.Host)
	assert.Equal(t, 5432, c.DB.Port)
}

func TestFillAndOverride_AnonymousField(t *testing.T) {
	type Base struct {
		Host string `json:",default=0.0.0.0"`
	}
	type Server struct {
		Base
		Name string `json:",default=svc"`
	}

	var c Server
	err := FillAndOverride(&c, Server{Name: "api"})
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", c.Host)
	assert.Equal(t, "api", c.Name)
}

func TestFillAndOverride_NotPointer(t *testing.T) {
	type Config struct {
		Name string `json:",default=x"`
	}

	var c Config
	err := FillAndOverride(c, Config{})
	assert.Error(t, err)
}

// --- Regression: nil / typed nil / anonymous embedded pointers (all used to panic) ---

// TestFillAndOverride_NilOverrides verifies that a nil overrides only fills
// defaults and does not panic.
// Historic defect: reflect.ValueOf(nil) yields an invalid value, and Type()
// panicked with "reflect: call of reflect.Value.Type on zero Value".
func TestFillAndOverride_NilOverrides(t *testing.T) {
	type Config struct {
		Host string `json:",default=localhost"`
		Port int    `json:",default=8080"`
	}

	var c Config
	require.NotPanics(t, func() {
		require.NoError(t, FillAndOverride(&c, nil))
	})
	assert.Equal(t, "localhost", c.Host)
	assert.Equal(t, 8080, c.Port)
}

// TestFillAndOverride_TypedNilOverrides verifies that a typed nil pointer also
// counts as "no override".
func TestFillAndOverride_TypedNilOverrides(t *testing.T) {
	type Config struct {
		Host string `json:",default=localhost"`
		Port int    `json:",default=8080"`
	}

	var nilCfg *Config
	var c Config
	require.NotPanics(t, func() {
		require.NoError(t, FillAndOverride(&c, nilCfg))
	})
	assert.Equal(t, "localhost", c.Host)
	assert.Equal(t, 8080, c.Port)
}

// TestFillAndOverride_AnonymousPtrField verifies that an anonymous embedded
// pointer struct can be overridden recursively with its defaults preserved.
// Historic defect: the recursion did not dereference, and calling Field(i) on a
// Ptr value panicked with "reflect: call of reflect.Value.Field on ptr Value".
func TestFillAndOverride_AnonymousPtrField(t *testing.T) {
	type Base struct {
		Host string `json:",default=0.0.0.0"`
		Port int    `json:",default=80"`
	}
	type Server struct {
		*Base
		Name string `json:",default=svc"`
	}

	t.Run("target pointer nil, override provided", func(t *testing.T) {
		var c Server
		require.NotPanics(t, func() {
			require.NoError(t, FillAndOverride(&c, Server{
				Base: &Base{Port: 8080},
				Name: "api",
			}))
		})
		require.NotNil(t, c.Base, "embedded pointer should be allocated to receive overrides")
		assert.Equal(t, "0.0.0.0", c.Host, "default must be preserved")
		assert.Equal(t, 8080, c.Port)
		assert.Equal(t, "api", c.Name)
	})

	t.Run("override pointer nil keeps defaults", func(t *testing.T) {
		var c Server
		require.NotPanics(t, func() {
			require.NoError(t, FillAndOverride(&c, Server{Name: "api"}))
		})
		// The default comes from FillDefault (mapping allocates the embedded
		// pointer); a nil override does not override
		assert.Equal(t, "api", c.Name)
		if c.Base != nil {
			assert.Equal(t, "0.0.0.0", c.Host)
		}
	})
}

// TestFillAndOverride_AnonymousPtrFieldNestedNonZero verifies that overriding a
// non-zero sub-field of an anonymous embedded pointer and preserving defaults
// both hold at the same time.
func TestFillAndOverride_AnonymousPtrFieldNestedNonZero(t *testing.T) {
	type Base struct {
		Host string `json:",default=localhost"`
		Port int    `json:",default=3306"`
	}
	type Config struct {
		*Base
		Name string `json:",default=app"`
	}

	var c Config
	require.NoError(t, FillAndOverride(&c, Config{Base: &Base{Host: "db.example.com"}}))
	require.NotNil(t, c.Base)
	assert.Equal(t, "db.example.com", c.Host)
	assert.Equal(t, 3306, c.Port, "unset sub-field must keep its default")
	assert.Equal(t, "app", c.Name)
}

func TestFillAndOverride_TypeMismatch(t *testing.T) {
	type A struct {
		Name string
	}
	type B struct {
		Name string
	}

	var a A
	err := FillAndOverride(&a, B{})
	assert.Error(t, err)
}

func TestMustFillAndOverride(t *testing.T) {
	type Config struct {
		Host string `json:",default=0.0.0.0"`
	}

	var c Config
	MustFillAndOverride(&c, Config{Host: "1.2.3.4"})
	assert.Equal(t, "1.2.3.4", c.Host)
}

func TestMustFillAndOverride_Panic(t *testing.T) {
	type Config struct {
		Name string `json:",default=x"`
	}

	var c Config
	assert.Panics(t, func() {
		MustFillAndOverride(c, Config{})
	})
}

// --- Additional coverage: override semantics branches ---

func TestFillAndOverride_OverridesPointer(t *testing.T) {
	type Config struct {
		Host string `json:",default=0.0.0.0"`
	}
	var c Config
	// A pointer passed as overrides must be dereferenced properly
	err := FillAndOverride(&c, &Config{Host: "1.1.1.1"})
	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.1", c.Host)
}

func TestFillAndOverride_BoolField(t *testing.T) {
	type Config struct {
		Verbose bool `json:",optional"`
	}
	var c Config
	// true overrides
	err := FillAndOverride(&c, Config{Verbose: true})
	assert.NoError(t, err)
	assert.True(t, c.Verbose)

	// false counts as unset -> the zero value is kept
	var c2 Config
	err = FillAndOverride(&c2, Config{Verbose: false})
	assert.NoError(t, err)
	assert.False(t, c2.Verbose)
}

func TestFillAndOverride_BoolPointerExplicitFalse(t *testing.T) {
	type Config struct {
		Verbose *bool `json:",optional"`
	}
	f := false
	var c Config
	err := FillAndOverride(&c, Config{Verbose: &f})
	assert.NoError(t, err)
	require.NotNil(t, c.Verbose)
	assert.False(t, *c.Verbose)
}

func TestFillAndOverride_SliceField(t *testing.T) {
	type Config struct {
		Tags []string `json:",optional"`
	}
	var c Config
	// A non-empty slice overrides
	err := FillAndOverride(&c, Config{Tags: []string{"a"}})
	assert.NoError(t, err)
	assert.Equal(t, []string{"a"}, c.Tags)

	// An empty slice counts as unset -> no override (stays nil)
	var c2 Config
	err = FillAndOverride(&c2, Config{Tags: []string{}})
	assert.NoError(t, err)
	assert.Nil(t, c2.Tags)
}

func TestFillAndOverride_MapField(t *testing.T) {
	type Config struct {
		Labels map[string]string `json:",optional"`
	}
	var c Config
	// A non-empty map overrides
	err := FillAndOverride(&c, Config{Labels: map[string]string{"a": "1"}})
	assert.NoError(t, err)
	assert.Equal(t, "1", c.Labels["a"])
}

func TestFillAndOverride_UnexportedField(t *testing.T) {
	type Config struct {
		Host   string `json:",default=0.0.0.0"`
		hidden string // unexported fields must be skipped by the override logic
	}
	var c Config
	c.hidden = "keep-me" // initial value: src would overwrite it if unexported fields were mishandled
	err := FillAndOverride(&c, Config{hidden: "drop-me"})
	assert.NoError(t, err)
	assert.Equal(t, "0.0.0.0", c.Host)   // exported fields are still filled from the default
	assert.Equal(t, "keep-me", c.hidden) // unexported fields keep their value, untouched
}

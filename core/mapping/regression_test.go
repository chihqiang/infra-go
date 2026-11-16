package mapping

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnmarshal_AnonymousScalarField used to recurse between processAnonymousField
// and processField until the goroutine stack overflowed (a fatal error that cannot
// be recovered).
func TestUnmarshal_AnonymousScalarField(t *testing.T) {
	type Level int
	type Config struct {
		Level
		Name string `json:"name"`
	}

	var c Config
	err := UnmarshalJsonMap(map[string]any{
		"Level": json.Number("3"),
		"name":  "svc",
	}, &c)
	require.NoError(t, err)
	assert.Equal(t, Level(3), c.Level)
	assert.Equal(t, "svc", c.Name)
}

func TestFillDefault_AnonymousScalarField(t *testing.T) {
	type Level int
	type Config struct {
		Level
		Name string `json:",default=svc"`
	}

	var c Config
	require.NoError(t, FillDefault(&c))
	assert.Equal(t, "svc", c.Name)
}

// TestUnmarshal_MalformedInputReturnsError covers value shapes that used to panic
// inside reflect instead of returning an error.
func TestUnmarshal_MalformedInputReturnsError(t *testing.T) {
	var withMap struct {
		Labels map[string]string `json:"labels"`
	}
	require.Error(t, UnmarshalJsonMap(map[string]any{"labels": []any{"a"}}, &withMap))

	var withPtrMap struct {
		Labels *map[string]string `json:"labels"`
	}
	require.Error(t, UnmarshalJsonMap(map[string]any{"labels": "not-a-map"}, &withPtrMap))

	// A nested struct element that is not a map must not panic either.
	var withSlice []struct {
		Name string `json:"name"`
	}
	require.Error(t, UnmarshalJsonMap(map[string]any{"field": []any{"x"}}, &struct {
		Field []struct {
			Name string `json:"name"`
		} `json:"field"`
	}{}))
	_ = withSlice
}

func TestUnmarshal_PointerCollectionElements(t *testing.T) {
	var c struct {
		Flags  []*bool            `json:"flags"`
		Lookup map[string]*bool   `json:"lookup"`
		Names  map[string]*string `json:"names"`
	}
	err := UnmarshalJsonMap(map[string]any{
		"flags":  []any{true, false},
		"lookup": map[string]any{"on": true, "off": false},
		"names":  map[string]any{"a": "x"},
	}, &c)
	require.NoError(t, err)
	require.Len(t, c.Flags, 2)
	assert.True(t, *c.Flags[0])
	assert.False(t, *c.Flags[1])
	assert.True(t, *c.Lookup["on"])
	assert.False(t, *c.Lookup["off"])
	assert.Equal(t, "x", *c.Names["a"])
}

func TestUnmarshal_DurationInContainers(t *testing.T) {
	var c struct {
		Durations []time.Duration          `json:"durations"`
		ByKey     map[string]time.Duration `json:"by_key"`
		AnyList   []any                    `json:"any_list"`
		AnyMap    map[string]any           `json:"any_map"`
	}
	err := UnmarshalJsonMap(map[string]any{
		"durations": []any{"1s", json.Number("2000000000")},
		"by_key":    map[string]any{"a": "1s"},
		"any_list":  []any{"a", json.Number("1")},
		"any_map":   map[string]any{"k": "v"},
	}, &c)
	require.NoError(t, err)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, c.Durations)
	assert.Equal(t, time.Second, c.ByKey["a"])
	assert.Equal(t, []any{"a", json.Number("1")}, c.AnyList)
	assert.Equal(t, "v", c.AnyMap["k"])
}

func TestUnmarshal_Float32OverflowAndNonFinite(t *testing.T) {
	var c struct {
		Rate float32 `json:"rate"`
	}
	require.Error(t, UnmarshalJsonMap(map[string]any{"rate": json.Number("1e300")}, &c))

	var n struct {
		Rate float64 `json:"rate"`
	}
	require.Error(t, UnmarshalJsonMap(map[string]any{"rate": json.Number(".nan")}, &n))
	require.Error(t, UnmarshalJsonMap(map[string]any{"rate": json.Number("+inf")}, &n))
}

func TestUnmarshal_NaNBypassesRange(t *testing.T) {
	var c struct {
		Rate float64 `json:"rate,range=[0:1]"`
	}
	require.Error(t, UnmarshalJsonMap(map[string]any{"rate": json.Number(".nan")}, &c))
}

func TestFillAndOverride_TimeTime(t *testing.T) {
	type Config struct {
		Name string
		When time.Time
	}

	when := time.Now().Truncate(time.Second)
	var c Config
	require.NoError(t, FillAndOverride(&c, Config{Name: "svc", When: when}))
	assert.Equal(t, "svc", c.Name)
	assert.True(t, c.When.Equal(when), "a non-zero time.Time override must be applied")
}

// SelfRefNode is exported on purpose: an embedded unexported type is skipped by
// the field walker, so only an exported self-referential type exercises the cycle.
type SelfRefNode struct {
	*SelfRefNode
	Name string `json:"name"`
}

// TestUnmarshal_SelfReferentialEmbeddedType is a regression test: an embedded
// pointer to the same struct recursed between processAnonymousField and
// processField until the goroutine stack overflowed (fatal, not recoverable).
func TestUnmarshal_SelfReferentialEmbeddedType(t *testing.T) {
	var n SelfRefNode
	err := UnmarshalJsonMap(map[string]any{"name": "x"}, &n)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nesting depth")
}

// TestUnmarshal_DeeplyNestedMapIsBounded protects the container recursion as well:
// a recursive config type fed with a very deep document must fail cleanly.
func TestUnmarshal_DeeplyNestedMapIsBounded(t *testing.T) {
	type tree struct {
		Children []tree `json:"children"`
	}

	node := map[string]any{"children": []any{}}
	for i := 0; i < 500; i++ {
		node = map[string]any{"children": []any{node}}
	}

	var out tree
	err := UnmarshalJsonMap(node, &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nesting depth")
}

// OverrideNode is the exported self-referential type used by the override test;
// an embedded unexported type is skipped by the field walker.
type OverrideNode struct {
	*OverrideNode
	Name string `json:"name"`
}

// TestOverrideStructFields_SelfReferentialGraph is a regression test: a pointer
// cycle in the overrides recursed in overrideStructFields until the stack
// overflowed.
func TestOverrideStructFields_SelfReferentialGraph(t *testing.T) {
	overrides := &OverrideNode{Name: "x"}
	overrides.OverrideNode = overrides

	var target OverrideNode
	err := overrideStructFields(
		reflect.ValueOf(&target).Elem(),
		reflect.ValueOf(overrides).Elem(),
		reflect.TypeOf(target),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nesting depth")
}

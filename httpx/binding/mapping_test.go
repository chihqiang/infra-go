package binding

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers mapping.go: mapURI/mapForm/mapHeader/mapFormByTag, the setter and the mapping engine.

// --- Direct fill into a map target ---

func TestMapForm_ToMapStringString(t *testing.T) {
	m := map[string]string{}
	err := mapForm(&m, map[string][]string{
		"a": {"1", "2"},
		"b": {"x"},
	})
	require.NoError(t, err)
	assert.Equal(t, "2", m["a"]) // the last value wins
	assert.Equal(t, "x", m["b"])
}

func TestMapForm_ToMapStringSlices(t *testing.T) {
	m := map[string][]string{}
	err := mapForm(&m, map[string][]string{
		"a": {"1", "2"},
		"b": {"x"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"1", "2"}, m["a"])
	assert.Equal(t, []string{"x"}, m["b"])
}

func TestMapForm_ToBadMapTarget(t *testing.T) {
	m := map[string]int{}
	require.Error(t, mapForm(&m, map[string][]string{"a": {"1"}}))
}

// --- Struct mapping (anonymous embedding / automatic pointer allocation / ignoring) ---

type mappingInner struct {
	X string `form:"x"`
}

type mappingOuter struct {
	mappingInner             // anonymous embedding, expands the sub-fields
	Y            string      `form:"y"`
	P            *mappingPtr // no form tag: allocated automatically when a sub-field is hit
	Skip         string      `form:"-"`
}

type mappingPtr struct {
	Z string `form:"z"`
}

func TestMapForm_NestedAnonymousAndPtr(t *testing.T) {
	var o mappingOuter
	err := mapForm(&o, map[string][]string{
		"x": {"vx"},
		"y": {"vy"},
		"z": {"vz"},
	})
	require.NoError(t, err)
	assert.Equal(t, "vx", o.X)
	assert.Equal(t, "vy", o.Y)
	require.NotNil(t, o.P) // the pointer field is allocated when a sub-field is hit
	assert.Equal(t, "vz", o.P.Z)
	assert.Empty(t, o.Skip) // form:"-" is ignored
}

// --- header mapping goes through headerSource ---

func TestMapHeader_CanonicalKey(t *testing.T) {
	type h struct {
		Token string `header:"x-token"` // lowercase tag → matched after CanonicalMIMEHeaderKey upper-casing
	}
	var v h
	// The data source uses the canonical key (the actual shape of http.Header: X-Token)
	require.NoError(t, mapHeader(&v, map[string][]string{"X-Token": {"abc"}}))
	assert.Equal(t, "abc", v.Token)
}

// --- Slice/default-value branches of setByForm ---

func TestSetByForm_SliceSplitAndDefault(t *testing.T) {
	// a single value containing commas is split automatically
	s := &struct {
		Tags []string
	}{}
	rv := reflectValueFieldOf(s, 0)
	ok, err := setByForm(rv, nil, map[string][]string{"tags": {"a,b,c"}}, "tags", setOptions{})
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, []string{"a", "b", "c"}, s.Tags)

	// falls back to the default value when absent (split by comma into a slice)
	ok, err = setByForm(rv, nil, map[string][]string{}, "tags", setOptions{isDefaultExists: true, defaultValue: "x,y"})
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, []string{"x", "y"}, s.Tags)

	// no data and no default value → not set
	ok, err = setByForm(rv, nil, map[string][]string{}, "tags", setOptions{})
	require.NoError(t, err)
	assert.False(t, ok)
}

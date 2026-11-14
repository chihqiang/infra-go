package conf

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- loaders extension registration ---

func TestLoaders_RegisteredTypes(t *testing.T) {
	// all three supported extensions should have a loader registered
	for _, ext := range []string{".json", ".yaml", ".yml"} {
		loader, ok := loaders[ext]
		assert.True(t, ok, "loader for %s should be registered", ext)
		assert.NotNil(t, loader)
	}
}

// --- loadFromJSONBytes ---

func TestLoadFromJSONBytesInternal_Invalid(t *testing.T) {
	_, err := loadFromJSONBytes([]byte("not json"))
	assert.Error(t, err)
}

func TestLoadFromJSONBytes_NumberPrecision(t *testing.T) {
	// numbers stay as json.Number so that no precision is lost
	m, err := loadFromJSONBytes([]byte(`{"id": 1234567890123456789, "pi": 3.14}`))
	assert.NoError(t, err)
	assert.Equal(t, json.Number("1234567890123456789"), m["id"])
	assert.Equal(t, json.Number("3.14"), m["pi"])
}

// --- loadFromYAMLBytes ---

func TestLoadFromYAMLBytesInternal_Invalid(t *testing.T) {
	_, err := loadFromYAMLBytes([]byte(": bad"))
	assert.Error(t, err)
}

func TestLoadFromYAMLBytes_NormalizesNumbers(t *testing.T) {
	// int/float parsed from YAML are converted to json.Number, matching the JSON path
	m, err := loadFromYAMLBytes([]byte("port: 3306\nrate: 1.5\n"))
	assert.NoError(t, err)
	assert.Equal(t, json.Number("3306"), m["port"])
	assert.Equal(t, json.Number("1.5"), m["rate"])
}

func TestLoadFromYAMLBytes_NormalizesNested(t *testing.T) {
	// numbers inside nested maps and slices are normalised too
	m, err := loadFromYAMLBytes([]byte("db:\n  port: 5432\nnums:\n  - 1\n  - 2\n"))
	assert.NoError(t, err)

	db := m["db"].(map[string]any)
	assert.Equal(t, json.Number("5432"), db["port"])

	nums := m["nums"].([]any)
	assert.Equal(t, json.Number("1"), nums[0])
	assert.Equal(t, json.Number("2"), nums[1])
}
